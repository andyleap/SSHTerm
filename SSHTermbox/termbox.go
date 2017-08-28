package sshtermbox

import "unicode/utf8"

import "strings"
import "strconv"

import "io"

// private API

const (
	t_enter_ca = iota
	t_exit_ca
	t_show_cursor
	t_hide_cursor
	t_clear_screen
	t_sgr0
	t_underline
	t_bold
	t_blink
	t_reverse
	t_enter_keypad
	t_exit_keypad
	t_enter_mouse
	t_exit_mouse
	t_max_funcs
)

const (
	coord_invalid = -2
	attr_invalid  = Attribute(0xFFFF)
)

type input_event struct {
	data []byte
	err  error
}

func (t *Termbox) write_cursor(x, y int) {
	t.outbuf.WriteString("\033[")
	t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(y+1), 10))
	t.outbuf.WriteString(";")
	t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(x+1), 10))
	t.outbuf.WriteString("H")
}

func (t *Termbox) write_sgr_fg(a Attribute) {
	switch t.output_mode {
	case Output256, Output216, OutputGrayscale:
		t.outbuf.WriteString("\033[38;5;")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(a-1), 10))
		t.outbuf.WriteString("m")
	default:
		t.outbuf.WriteString("\033[3")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(a-1), 10))
		t.outbuf.WriteString("m")
	}
}

func (t *Termbox) write_sgr_bg(a Attribute) {
	switch t.output_mode {
	case Output256, Output216, OutputGrayscale:
		t.outbuf.WriteString("\033[48;5;")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(a-1), 10))
		t.outbuf.WriteString("m")
	default:
		t.outbuf.WriteString("\033[4")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(a-1), 10))
		t.outbuf.WriteString("m")
	}
}

func (t *Termbox) write_sgr(fg, bg Attribute) {
	switch t.output_mode {
	case Output256, Output216, OutputGrayscale:
		t.outbuf.WriteString("\033[38;5;")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(fg-1), 10))
		t.outbuf.WriteString("m")
		t.outbuf.WriteString("\033[48;5;")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(bg-1), 10))
		t.outbuf.WriteString("m")
	default:
		t.outbuf.WriteString("\033[3")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(fg-1), 10))
		t.outbuf.WriteString(";4")
		t.outbuf.Write(strconv.AppendUint(t.intbuf, uint64(bg-1), 10))
		t.outbuf.WriteString("m")
	}
}

func (t *Termbox) send_attr(fg, bg Attribute) {
	if fg == t.lastfg && bg == t.lastbg {
		return
	}

	t.outbuf.WriteString(t.funcs[t_sgr0])

	var fgcol, bgcol Attribute

	switch t.output_mode {
	case Output256:
		fgcol = fg & 0x1FF
		bgcol = bg & 0x1FF
	case Output216:
		fgcol = fg & 0xFF
		bgcol = bg & 0xFF
		if fgcol > 216 {
			fgcol = ColorDefault
		}
		if bgcol > 216 {
			bgcol = ColorDefault
		}
		if fgcol != ColorDefault {
			fgcol += 0x10
		}
		if bgcol != ColorDefault {
			bgcol += 0x10
		}
	case OutputGrayscale:
		fgcol = fg & 0x1F
		bgcol = bg & 0x1F
		if fgcol > 26 {
			fgcol = ColorDefault
		}
		if bgcol > 26 {
			bgcol = ColorDefault
		}
		if fgcol != ColorDefault {
			fgcol = t.grayscale[fgcol]
		}
		if bgcol != ColorDefault {
			bgcol = t.grayscale[bgcol]
		}
	default:
		fgcol = fg & 0x0F
		bgcol = bg & 0x0F
	}

	if fgcol != ColorDefault {
		if bgcol != ColorDefault {
			t.write_sgr(fgcol, bgcol)
		} else {
			t.write_sgr_fg(fgcol)
		}
	} else if bgcol != ColorDefault {
		t.write_sgr_bg(bgcol)
	}

	if fg&AttrBold != 0 {
		t.outbuf.WriteString(t.funcs[t_bold])
	}
	if bg&AttrBold != 0 {
		t.outbuf.WriteString(t.funcs[t_blink])
	}
	if fg&AttrUnderline != 0 {
		t.outbuf.WriteString(t.funcs[t_underline])
	}
	if fg&AttrReverse|bg&AttrReverse != 0 {
		t.outbuf.WriteString(t.funcs[t_reverse])
	}

	t.lastfg, t.lastbg = fg, bg
}

func (t *Termbox) send_char(x, y int, ch rune) {
	var buf [8]byte
	n := utf8.EncodeRune(buf[:], ch)
	if x-1 != t.lastx || y != t.lasty {
		t.write_cursor(x, y)
	}
	t.lastx, t.lasty = x, y
	t.outbuf.Write(buf[:n])
}

func (t *Termbox) flush() error {
	_, err := io.Copy(t.out, &t.outbuf)
	t.outbuf.Reset()
	if err != nil {
		return err
	}
	return nil
}

func (t *Termbox) send_clear() error {
	t.send_attr(t.foreground, t.background)
	t.outbuf.WriteString(t.funcs[t_clear_screen])
	if !t.is_cursor_hidden(t.cursor_x, t.cursor_y) {
		t.write_cursor(t.cursor_x, t.cursor_y)
	}

	// we need to invalidate cursor position too and these two vars are
	// used only for simple cursor positioning optimization, cursor
	// actually may be in the correct place, but we simply discard
	// optimization once and it gives us simple solution for the case when
	// cursor moved
	t.lastx = coord_invalid
	t.lasty = coord_invalid

	return t.flush()
}

func (t *Termbox) Resize(newW, newH int) {
	if t.newW != newW || t.newH != newH {
		t.newW, t.newH = newW, newH
		t.resize_comm <- Event{Type: EventResize, Width: newW, Height: newH}
	}
}

func (t *Termbox) update_size_maybe() error {
	w, h := t.newW, t.newH
	if w != t.termw || h != t.termh {
		t.termw, t.termh = w, h
		t.back_buffer.resize(w, h, t.foreground, t.background)
		t.front_buffer.resize(w, h, t.foreground, t.background)
		t.front_buffer.clear(t.foreground, t.background)
		return t.send_clear()
	}
	return nil
}

func (t *Termbox) parse_mouse_event(event *Event, buf string) (int, bool) {
	if strings.HasPrefix(buf, "\033[M") && len(buf) >= 6 {
		// X10 mouse encoding, the simplest one
		// \033 [ M Cb Cx Cy
		b := buf[3] - 32
		switch b & 3 {
		case 0:
			if b&64 != 0 {
				event.Key = MouseWheelUp
			} else {
				event.Key = MouseLeft
			}
		case 1:
			if b&64 != 0 {
				event.Key = MouseWheelDown
			} else {
				event.Key = MouseMiddle
			}
		case 2:
			event.Key = MouseRight
		case 3:
			event.Key = MouseRelease
		default:
			return 6, false
		}
		event.Type = EventMouse // KeyEvent by default
		if b&32 != 0 {
			event.Mod |= ModMotion
		}

		// the coord is 1,1 for upper left
		event.MouseX = int(buf[4]) - 1 - 32
		event.MouseY = int(buf[5]) - 1 - 32
		return 6, true
	} else if strings.HasPrefix(buf, "\033[<") || strings.HasPrefix(buf, "\033[") {
		// xterm 1006 extended mode or urxvt 1015 extended mode
		// xterm: \033 [ < Cb ; Cx ; Cy (M or m)
		// urxvt: \033 [ Cb ; Cx ; Cy M

		// find the first M or m, that's where we stop
		mi := strings.IndexAny(buf, "Mm")
		if mi == -1 {
			return 0, false
		}

		// whether it's a capital M or not
		isM := buf[mi] == 'M'

		// whether it's urxvt or not
		isU := false

		// buf[2] is safe here, because having M or m found means we have at
		// least 3 bytes in a string
		if buf[2] == '<' {
			buf = buf[3:mi]
		} else {
			isU = true
			buf = buf[2:mi]
		}

		s1 := strings.Index(buf, ";")
		s2 := strings.LastIndex(buf, ";")
		// not found or only one ';'
		if s1 == -1 || s2 == -1 || s1 == s2 {
			return 0, false
		}

		n1, err := strconv.ParseInt(buf[0:s1], 10, 64)
		if err != nil {
			return 0, false
		}
		n2, err := strconv.ParseInt(buf[s1+1:s2], 10, 64)
		if err != nil {
			return 0, false
		}
		n3, err := strconv.ParseInt(buf[s2+1:], 10, 64)
		if err != nil {
			return 0, false
		}

		// on urxvt, first number is encoded exactly as in X10, but we need to
		// make it zero-based, on xterm it is zero-based already
		if isU {
			n1 -= 32
		}
		switch n1 & 3 {
		case 0:
			if n1&64 != 0 {
				event.Key = MouseWheelUp
			} else {
				event.Key = MouseLeft
			}
		case 1:
			if n1&64 != 0 {
				event.Key = MouseWheelDown
			} else {
				event.Key = MouseMiddle
			}
		case 2:
			event.Key = MouseRight
		case 3:
			event.Key = MouseRelease
		default:
			return mi + 1, false
		}
		if !isM {
			// on xterm mouse release is signaled by lowercase m
			event.Key = MouseRelease
		}

		event.Type = EventMouse // KeyEvent by default
		if n1&32 != 0 {
			event.Mod |= ModMotion
		}

		event.MouseX = int(n2) - 1
		event.MouseY = int(n3) - 1
		return mi + 1, true
	}

	return 0, false
}

func (t *Termbox) parse_escape_sequence(event *Event, buf []byte) (int, bool) {
	bufstr := string(buf)
	for i, key := range t.keys {
		if strings.HasPrefix(bufstr, key) {
			event.Ch = 0
			event.Key = Key(0xFFFF - i)
			return len(key), true
		}
	}

	// if none of the keys match, let's try mouse seqences
	return t.parse_mouse_event(event, bufstr)
}

func (t *Termbox) extract_raw_event(data []byte, event *Event) bool {
	if len(t.inbuf) == 0 {
		return false
	}

	n := len(data)
	if n == 0 {
		return false
	}

	n = copy(data, t.inbuf)
	copy(t.inbuf, t.inbuf[n:])
	t.inbuf = t.inbuf[:len(t.inbuf)-n]

	event.N = n
	event.Type = EventRaw
	return true
}

func (t *Termbox) extract_event(inbuf []byte, event *Event) bool {
	if len(inbuf) == 0 {
		event.N = 0
		return false
	}

	if inbuf[0] == '\033' {
		// possible escape sequence
		if n, ok := t.parse_escape_sequence(event, inbuf); n != 0 {
			event.N = n
			return ok
		}

		// it's not escape sequence, then it's Alt or Esc, check input_mode
		switch {
		case t.input_mode&InputEsc != 0:
			// if we're in escape mode, fill Esc event, pop buffer, return success
			event.Ch = 0
			event.Key = KeyEsc
			event.Mod = 0
			event.N = 1
			return true
		case t.input_mode&InputAlt != 0:
			// if we're in alt mode, set Alt modifier to event and redo parsing
			event.Mod = ModAlt
			ok := t.extract_event(inbuf[1:], event)
			if ok {
				event.N++
			} else {
				event.N = 0
			}
			return ok
		default:
			panic("unreachable")
		}
	}

	// if we're here, this is not an escape sequence and not an alt sequence
	// so, it's a FUNCTIONAL KEY or a UNICODE character

	// first of all check if it's a functional key
	if Key(inbuf[0]) <= KeySpace || Key(inbuf[0]) == KeyBackspace2 {
		// fill event, pop buffer, return success
		event.Ch = 0
		event.Key = Key(inbuf[0])
		event.N = 1
		return true
	}

	// the only possible option is utf8 rune
	if r, n := utf8.DecodeRune(inbuf); r != utf8.RuneError {
		event.Ch = r
		event.Key = 0
		event.N = n
		return true
	}

	return false
}
