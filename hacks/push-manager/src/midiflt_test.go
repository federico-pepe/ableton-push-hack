package main

import (
	"reflect"
	"testing"
)

// The pass bitmap is shared with push_hook.c: bit n of byte 5+n/8 is CC n.
func TestMidiFiltPassBitmap(t *testing.T) {
	data := make([]byte, midiFiltSize)
	data[4] = 1
	set := setMidiFiltPass(data, []int{85, 86, 0, 87, 88, -1, 200})
	if !reflect.DeepEqual(set, []int{85, 86, 0, 87}) {
		t.Errorf("accepted %v, want [85 86 0 87] (88+ and negatives do not fit)", set)
	}
	if data[5+85/8] != 1<<(85%8)|1<<(86%8)|1<<(87%8) || data[5] != 1 {
		t.Errorf("bitmap bytes % x", data[5:])
	}
	if data[4] != 1 {
		t.Error("the enabled byte must not be touched")
	}
	if got := midiFiltPassList(data); !reflect.DeepEqual(got, []int{0, 85, 86, 87}) {
		t.Errorf("read back %v", got)
	}
	setMidiFiltPass(data, nil)
	if got := midiFiltPassList(data); len(got) != 0 {
		t.Errorf("cleared list reads %v", got)
	}
}
