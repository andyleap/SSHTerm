package sshtermbox

// private API, common OS agnostic part

type cellbuf struct {
	width  int
	height int
	cells  []Cell
}

func (this *cellbuf) init(width, height int) {
	this.width = width
	this.height = height
	this.cells = make([]Cell, width*height)
}

func (this *cellbuf) resize(width, height int, fg, bg Attribute) {
	if this.width == width && this.height == height {
		return
	}

	oldw := this.width
	oldh := this.height
	oldcells := this.cells

	this.init(width, height)
	this.clear(fg, bg)

	minw, minh := oldw, oldh

	if width < minw {
		minw = width
	}
	if height < minh {
		minh = height
	}

	for i := 0; i < minh; i++ {
		srco, dsto := i*oldw, i*width
		src := oldcells[srco : srco+minw]
		dst := this.cells[dsto : dsto+minw]
		copy(dst, src)
	}
}

func (this *cellbuf) clear(fg, bg Attribute) {
	for i := range this.cells {
		c := &this.cells[i]
		c.Ch = ' '
		c.Fg = fg
		c.Bg = bg
	}
}

const cursor_hidden = -1

func (t *Termbox) is_cursor_hidden(x, y int) bool {
	return x == cursor_hidden || y == cursor_hidden
}
