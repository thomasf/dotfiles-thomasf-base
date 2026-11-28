package main

import (
	"testing"

	"github.com/jezek/xgb/xproto"
)

func TestAnyHeld(t *testing.T) {
	keys := make([]byte, 32)
	keys[37/8] |= 1 << (37 % 8) // keycode 37, Control_L on most layouts
	for _, tc := range []struct {
		codes []xproto.Keycode
		want  bool
	}{
		{[]xproto.Keycode{50, 62, 0, 0}, false},
		{[]xproto.Keycode{50, 0, 37, 105}, true},
		{[]xproto.Keycode{0, 0}, false},
		{nil, false},
	} {
		if got := anyHeld(keys, tc.codes); got != tc.want {
			t.Errorf("anyHeld(%v) = %v, want %v", tc.codes, got, tc.want)
		}
	}
	if anyHeld(nil, []xproto.Keycode{37}) {
		t.Error("held with no keymap")
	}
}
