package sshtermbox

import (
	"strings"
)

const (
	ti_magic         = 0432
	ti_header_length = 12
	ti_mouse_enter   = "\x1b[?1000h\x1b[?1002h\x1b[?1015h\x1b[?1006h"
	ti_mouse_leave   = "\x1b[?1006l\x1b[?1015l\x1b[?1002l\x1b[?1000l"
)

func getTermInfo(term string) ([]string, []string, error) {
	for _, t := range terms {
		if t.name == term {
			return t.keys, t.funcs, nil
		}
	}

	compat_table := []struct {
		partial string
		keys    []string
		funcs   []string
	}{
		{"xterm", xterm_keys, xterm_funcs},
		{"rxvt", rxvt_unicode_keys, rxvt_unicode_funcs},
		{"linux", linux_keys, linux_funcs},
		{"Eterm", eterm_keys, eterm_funcs},
		{"screen", screen_keys, screen_funcs},
		// let's assume that 'cygwin' is xterm compatible
		{"cygwin", xterm_keys, xterm_funcs},
		{"st", xterm_keys, xterm_funcs},
	}

	// try compatibility variants
	for _, t := range compat_table {
		if strings.Contains(term, t.partial) {
			return t.keys, t.funcs, nil
		}
	}

	return nil, nil, errorUnknownTerm
}
