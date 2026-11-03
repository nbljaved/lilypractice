package main

import "fmt"

// pianoKeys lays a piano over the computer keyboard by key position, so it
// works with any keyboard layout. Values are semitones above the base note
// (C3 by default), as in VMPK and most DAWs:
//
//	 s d   g h j   l ;          2 3   5 6 7   9 0   =
//	z x c v b n m , . /        q w e r t y u i o p [ ]
//
// The keycodes are X/XKB keycodes, which GTK also reports on Wayland.
var pianoKeys = map[uint]int{
	// Bottom row, from the base note.
	52: 0, 39: 1, 53: 2, 40: 3, 54: 4, 55: 5, 42: 6, 56: 7, 43: 8, 57: 9,
	44: 10, 58: 11, 59: 12, 46: 13, 60: 14, 47: 15, 61: 16,
	// Top row, an octave higher.
	24: 12, 11: 13, 25: 14, 12: 15, 26: 16, 27: 17, 14: 18, 28: 19, 15: 20,
	29: 21, 16: 22, 30: 23, 31: 24, 18: 25, 32: 26, 19: 27, 33: 28, 34: 29,
	21: 30, 35: 31,
}

var noteNames = [12]string{"C", "C♯", "D", "E♭", "E", "F", "F♯", "G", "A♭", "A", "B♭", "B"}

// noteName gives the scientific pitch name of a MIDI note: 60 is C4.
func noteName(pitch int) string {
	return fmt.Sprintf("%s%d", noteNames[pitch%12], pitch/12-1)
}
