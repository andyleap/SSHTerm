package sshtermbox

import (
	"bytes"
	"io"
	"log"
	"sync"

	"github.com/mattn/go-runewidth"
)

type Termbox struct {
	keys  []string
	funcs []string

	back_buffer    cellbuf
	front_buffer   cellbuf
	termw          int
	termh          int
	input_mode     InputMode
	output_mode    OutputMode
	out            io.Writer
	in             io.Reader
	lastfg         Attribute
	lastbg         Attribute
	lastx          int
	lasty          int
	cursor_x       int
	cursor_y       int
	foreground     Attribute
	background     Attribute
	inbuf          []byte
	outbuf         bytes.Buffer
	quit           chan int
	input_comm     chan input_event
	interrupt_comm chan struct{}
	intbuf         []byte
	resize_comm    chan struct{}

	newW     int
	newH     int
	sizeLock sync.Mutex

	// grayscale indexes
	grayscale []Attribute
}

func (t *Termbox) writeString(str string) {
	io.WriteString(t.out, str)
}

// public API

// Initializes termbox library. This function should be called before any other functions.
// After successful initialization, the library must be finalized using 'Close' function.
//
// Example usage:
//      err := termbox.Init()
//      if err != nil {
//              panic(err)
//      }
//      defer termbox.Close()
func Init(in io.Reader, out io.Writer, term string, w, h int) (*Termbox, error) {
	termbox := &Termbox{
		out:            out,
		in:             in,
		input_mode:     InputEsc,
		output_mode:    OutputNormal,
		lastfg:         attr_invalid,
		lastbg:         attr_invalid,
		lastx:          coord_invalid,
		lasty:          coord_invalid,
		cursor_x:       cursor_hidden,
		cursor_y:       cursor_hidden,
		foreground:     ColorDefault,
		background:     ColorDefault,
		inbuf:          make([]byte, 0, 64),
		quit:           make(chan int),
		input_comm:     make(chan input_event),
		interrupt_comm: make(chan struct{}),
		resize_comm:    make(chan struct{}, 1),
		intbuf:         make([]byte, 0, 16),
		grayscale: []Attribute{
			0, 17, 233, 234, 235, 236, 237, 238, 239, 240, 241, 242, 243, 244,
			245, 246, 247, 248, 249, 250, 251, 252, 253, 254, 255, 256, 232,
		},
	}

	termbox.keys, termbox.funcs, _ = getTermInfo(term)

	termbox.writeString(termbox.funcs[t_enter_ca])
	termbox.writeString(termbox.funcs[t_enter_keypad])
	termbox.writeString(termbox.funcs[t_hide_cursor])
	termbox.writeString(termbox.funcs[t_clear_screen])

	termbox.termh = h
	termbox.termw = w
	termbox.newH = h
	termbox.newW = w

	termbox.back_buffer.init(w, h)
	termbox.front_buffer.init(w, h)
	termbox.back_buffer.clear(termbox.foreground, termbox.background)
	termbox.front_buffer.clear(termbox.foreground, termbox.background)
	buf := make([]byte, 0, 128)
	go func() {
		for {
			n, err := termbox.in.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Fatal(err)
				}
				break
			}
			select {
			case termbox.input_comm <- input_event{buf[:n], err}:
				ie := <-termbox.input_comm
				buf = ie.data[:128]
			case <-termbox.quit:
				return
			}
		}
	}()
	return termbox, nil
}

// Interrupt an in-progress call to PollEvent by causing it to return
// EventInterrupt.  Note that this function will block until the PollEvent
// function has successfully been interrupted.
func (t *Termbox) Interrupt() {
	t.interrupt_comm <- struct{}{}
}

// Finalizes termbox library, should be called after successful initialization
// when termbox's functionality isn't required anymore.
func (t *Termbox) Close() {
	t.quit <- 1
	t.writeString(t.funcs[t_show_cursor])
	t.writeString(t.funcs[t_sgr0])
	t.writeString(t.funcs[t_clear_screen])
	t.writeString(t.funcs[t_exit_ca])
	t.writeString(t.funcs[t_exit_keypad])
	t.writeString(t.funcs[t_exit_mouse])
}

// Synchronizes the internal back buffer with the terminal.
func (t *Termbox) Flush() error {
	// invalidate cursor position
	t.lastx = coord_invalid
	t.lasty = coord_invalid

	t.update_size_maybe()

	for y := 0; y < t.front_buffer.height; y++ {
		line_offset := y * t.front_buffer.width
		for x := 0; x < t.front_buffer.width; {
			cell_offset := line_offset + x
			back := &t.back_buffer.cells[cell_offset]
			front := &t.front_buffer.cells[cell_offset]
			if back.Ch < ' ' {
				back.Ch = ' '
			}
			w := runewidth.RuneWidth(back.Ch)
			if w == 0 || w == 2 && runewidth.IsAmbiguousWidth(back.Ch) {
				w = 1
			}
			if *back == *front {
				x += w
				continue
			}
			*front = *back
			t.send_attr(back.Fg, back.Bg)

			if w == 2 && x == t.front_buffer.width-1 {
				// there's not enough space for 2-cells rune,
				// let's just put a space in there
				t.send_char(x, y, ' ')
			} else {
				t.send_char(x, y, back.Ch)
				if w == 2 {
					next := cell_offset + 1
					t.front_buffer.cells[next] = Cell{
						Ch: 0,
						Fg: back.Fg,
						Bg: back.Bg,
					}
				}
			}
			x += w
		}
	}
	if !t.is_cursor_hidden(t.cursor_x, t.cursor_y) {
		t.write_cursor(t.cursor_x, t.cursor_y)
	}
	return t.flush()
}

// Sets the position of the cursor. See also HideCursor().
func (t *Termbox) SetCursor(x, y int) {
	if t.is_cursor_hidden(t.cursor_x, t.cursor_y) && !t.is_cursor_hidden(x, y) {
		t.outbuf.WriteString(t.funcs[t_show_cursor])
	}

	if !t.is_cursor_hidden(t.cursor_x, t.cursor_y) && t.is_cursor_hidden(x, y) {
		t.outbuf.WriteString(t.funcs[t_hide_cursor])
	}

	t.cursor_x, t.cursor_y = x, y
	if !t.is_cursor_hidden(t.cursor_x, t.cursor_y) {
		t.write_cursor(t.cursor_x, t.cursor_y)
	}
}

// The shortcut for SetCursor(-1, -1).
func (t *Termbox) HideCursor() {
	t.SetCursor(cursor_hidden, cursor_hidden)
}

// Changes cell's parameters in the internal back buffer at the specified
// position.
func (t *Termbox) SetCell(x, y int, ch rune, fg, bg Attribute) {
	if x < 0 || x >= t.back_buffer.width {
		return
	}
	if y < 0 || y >= t.back_buffer.height {
		return
	}

	t.back_buffer.cells[y*t.back_buffer.width+x] = Cell{ch, fg, bg}
}

// Returns a slice into the termbox's back buffer. You can get its dimensions
// using 'Size' function. The slice remains valid as long as no 'Clear' or
// 'Flush' function calls were made after call to this function.
func (t *Termbox) CellBuffer() []Cell {
	return t.back_buffer.cells
}

// After getting a raw event from PollRawEvent function call, you can parse it
// again into an ordinary one using termbox logic. That is parse an event as
// termbox would do it. Returned event in addition to usual Event struct fields
// sets N field to the amount of bytes used within 'data' slice. If the length
// of 'data' slice is zero or event cannot be parsed for some other reason, the
// function will return a special event type: EventNone.
//
// IMPORTANT: EventNone may contain a non-zero N, which means you should skip
// these bytes, because termbox cannot recognize them.
//
// NOTE: This API is experimental and may change in future.
func (t *Termbox) ParseEvent(data []byte) Event {
	event := Event{Type: EventKey}
	ok := t.extract_event(data, &event)
	if !ok {
		return Event{Type: EventNone, N: event.N}
	}
	return event
}

// Wait for an event and return it. This is a blocking function call. Instead
// of EventKey and EventMouse it returns EventRaw events. Raw event is written
// into `data` slice and Event's N field is set to the amount of bytes written.
// The minimum required length of the 'data' slice is 1. This requirement may
// vary on different platforms.
//
// NOTE: This API is experimental and may change in future.
func (t *Termbox) PollRawEvent(data []byte) Event {
	if len(data) == 0 {
		panic("len(data) >= 1 is a requirement")
	}

	var event Event
	if t.extract_raw_event(data, &event) {
		return event
	}

	for {
		select {
		case ev := <-t.input_comm:
			if ev.err != nil {
				return Event{Type: EventError, Err: ev.err}
			}

			t.inbuf = append(t.inbuf, ev.data...)
			t.input_comm <- ev
			if t.extract_raw_event(data, &event) {
				return event
			}
		case <-t.interrupt_comm:
			event.Type = EventInterrupt
			return event
		case <-t.resize_comm:
			event.Type = EventResize
			t.sizeLock.Lock()
			event.Width, event.Height = t.newW, t.newH
			t.sizeLock.Unlock()
			return event
		}
	}
}

// Wait for an event and return it. This is a blocking function call.
func (t *Termbox) PollEvent() Event {
	var event Event

	// try to extract event from input buffer, return on success
	event.Type = EventKey
	ok := t.extract_event(t.inbuf, &event)
	if event.N != 0 {
		copy(t.inbuf, t.inbuf[event.N:])
		t.inbuf = t.inbuf[:len(t.inbuf)-event.N]
	}
	if ok {
		return event
	}

	for {
		select {
		case ev := <-t.input_comm:
			if ev.err != nil {
				return Event{Type: EventError, Err: ev.err}
			}

			t.inbuf = append(t.inbuf, ev.data...)
			t.input_comm <- ev
			ok := t.extract_event(t.inbuf, &event)
			if event.N != 0 {
				copy(t.inbuf, t.inbuf[event.N:])
				t.inbuf = t.inbuf[:len(t.inbuf)-event.N]
			}
			if ok {
				return event
			}
		case <-t.interrupt_comm:
			event.Type = EventInterrupt
			return event

		case <-t.resize_comm:
			event.Type = EventResize
			t.sizeLock.Lock()
			event.Width, event.Height = t.newW, t.newH
			t.sizeLock.Unlock()
			return event
		}
	}
	panic("unreachable")
}

// Returns the size of the internal back buffer (which is mostly the same as
// terminal's window size in characters). But it doesn't always match the size
// of the terminal window, after the terminal size has changed, the internal
// back buffer will get in sync only after Clear or Flush function calls.
func (t *Termbox) Size() (width int, height int) {
	return t.termw, t.termh
}

// Clears the internal back buffer.
func (t *Termbox) Clear(fg, bg Attribute) error {
	t.foreground, t.background = fg, bg
	err := t.update_size_maybe()
	t.back_buffer.clear(t.foreground, t.background)
	return err
}

// Sets termbox input mode. Termbox has two input modes:
//
// 1. Esc input mode. When ESC sequence is in the buffer and it doesn't match
// any known sequence. ESC means KeyEsc. This is the default input mode.
//
// 2. Alt input mode. When ESC sequence is in the buffer and it doesn't match
// any known sequence. ESC enables ModAlt modifier for the next keyboard event.
//
// Both input modes can be OR'ed with Mouse mode. Setting Mouse mode bit up will
// enable mouse button press/release and drag events.
//
// If 'mode' is InputCurrent, returns the current input mode. See also Input*
// constants.
func (t *Termbox) SetInputMode(mode InputMode) InputMode {
	if mode == InputCurrent {
		return t.input_mode
	}
	if mode&(InputEsc|InputAlt) == 0 {
		mode |= InputEsc
	}
	if mode&(InputEsc|InputAlt) == InputEsc|InputAlt {
		mode &^= InputAlt
	}
	if mode&InputMouse != 0 {
		t.writeString(t.funcs[t_enter_mouse])
	} else {
		t.writeString(t.funcs[t_exit_mouse])
	}

	t.input_mode = mode
	return t.input_mode
}

// Sets the termbox output mode. Termbox has four output options:
//
// 1. OutputNormal => [1..8]
//    This mode provides 8 different colors:
//        black, red, green, yellow, blue, magenta, cyan, white
//    Shortcut: ColorBlack, ColorRed, ...
//    Attributes: AttrBold, AttrUnderline, AttrReverse
//
//    Example usage:
//        SetCell(x, y, '@', ColorBlack | AttrBold, ColorRed);
//
// 2. Output256 => [1..256]
//    In this mode you can leverage the 256 terminal mode:
//    0x01 - 0x08: the 8 colors as in OutputNormal
//    0x09 - 0x10: Color* | AttrBold
//    0x11 - 0xe8: 216 different colors
//    0xe9 - 0x1ff: 24 different shades of grey
//
//    Example usage:
//        SetCell(x, y, '@', 184, 240);
//        SetCell(x, y, '@', 0xb8, 0xf0);
//
// 3. Output216 => [1..216]
//    This mode supports the 3rd range of the 256 mode only.
//    But you dont need to provide an offset.
//
// 4. OutputGrayscale => [1..26]
//    This mode supports the 4th range of the 256 mode
//    and black and white colors from 3th range of the 256 mode
//    But you dont need to provide an offset.
//
// In all modes, 0x00 represents the default color.
//
// `go run _demos/output.go` to see its impact on your terminal.
//
// If 'mode' is OutputCurrent, it returns the current output mode.
//
// Note that this may return a different OutputMode than the one requested,
// as the requested mode may not be available on the target platform.
func (t *Termbox) SetOutputMode(mode OutputMode) OutputMode {
	if mode == OutputCurrent {
		return t.output_mode
	}

	t.output_mode = mode
	return t.output_mode
}

// Sync comes handy when something causes desync between termbox's understanding
// of a terminal buffer and the reality. Such as a third party process. Sync
// forces a complete resync between the termbox and a terminal, it may not be
// visually pretty though.
func (t *Termbox) Sync() error {
	t.front_buffer.clear(t.foreground, t.background)
	err := t.send_clear()
	if err != nil {
		return err
	}

	return t.Flush()
}
