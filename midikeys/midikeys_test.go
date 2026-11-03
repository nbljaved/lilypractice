package midikeys

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func anyCommand(string) bool { return true }

func defaultMap(t *testing.T) *Map {
	t.Helper()
	var c Config
	if err := json.Unmarshal([]byte(Default), &c); err != nil {
		t.Fatal(err)
	}
	m, err := New(c, anyCommand)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

const ch16 = 15 // MIDI channel 16, counted from 0

func TestParseNote(t *testing.T) {
	for s, want := range map[string]int{
		"A0": 21, "a0": 21, "A#0": 22, "A♯0": 22, "Bb0": 22, "B♭0": 22,
		"C4": 60, "C-1": 0, "G9": 127, "21": 21, " E1 ": 28,
	} {
		if got, err := ParseNote(s); err != nil || got != want {
			t.Errorf("ParseNote(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"", "H2", "C", "C#", "Cx1", "128", "G#9", "A0+"} {
		if got, err := ParseNote(s); err == nil {
			t.Errorf("ParseNote(%q) = %d, want an error", s, got)
		}
	}
}

func TestSingleKeysActWhenTheyGoDown(t *testing.T) {
	m := defaultMap(t)
	if c, ok := m.Press(ch16, 23); c != "play-stop" || !ok { // B0
		t.Errorf("B0 down: %q %v, want play-stop", c, ok)
	}
	if c, ok := m.Release(ch16, 23); c != "" || !ok {
		t.Errorf("B0 up: %q %v, want nothing", c, ok)
	}
}

func TestKeysOfACombinationActWhenTheyGoUp(t *testing.T) {
	m := defaultMap(t)
	// A0 alone: back a bar, when it goes up.
	if c, _ := m.Press(ch16, 21); c != "" {
		t.Errorf("A0 down ran %q, want it to wait", c)
	}
	if c, _ := m.Release(ch16, 21); c != "bar-back" {
		t.Errorf("A0 up ran %q, want bar-back", c)
	}
	// A0 and C1 together: to the start, and neither key's own command.
	m.Press(ch16, 21)
	if c, _ := m.Press(ch16, 24); c != "start" {
		t.Errorf("A0+C1 ran %q, want start", c)
	}
	for _, p := range []int{24, 21} {
		if c, _ := m.Release(ch16, p); c != "" {
			t.Errorf("letting go of %d after A0+C1 ran %q", p, c)
		}
	}
}

func TestAKeyPressedWhileAnotherIsHeld(t *testing.T) {
	m := defaultMap(t)
	m.Press(ch16, 26) // D1, waits for a combination
	if c, _ := m.Press(ch16, 23); c != "play-stop" {
		t.Errorf("B0 with D1 held ran %q, want play-stop", c)
	}
	if c, _ := m.Release(ch16, 26); c != "" {
		t.Errorf("D1 up ran %q after being held for B0, want nothing", c)
	}
}

func TestChannel(t *testing.T) {
	m := defaultMap(t)
	if _, ok := m.Press(0, 23); ok {
		t.Error("B0 on channel 1 taken as a command key; the zone is channel 16")
	}
	if c, ok := m.Press(ch16, 60); c != "" || !ok {
		t.Errorf("C4 on channel 16: %q %v; want a command key that does nothing", c, ok)
	}

	// Any channel: only the keys named.
	m, err := New(Config{Keys: map[string]string{"A0": "play-stop"}}, anyCommand)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := m.Press(3, 21); c != "play-stop" || !ok {
		t.Errorf("A0 on channel 4: %q %v, want play-stop", c, ok)
	}
	if _, ok := m.Press(3, 60); ok {
		t.Error("C4 taken as a command key, but only A0 is one")
	}
}

func TestProblemsReported(t *testing.T) {
	known := func(c string) bool { return c == "play-stop" }
	m, err := New(Config{Channel: 16, Keys: map[string]string{
		"A0": "play-stop", "B0": "dance", "H1": "play-stop", "C1+X": "play-stop",
	}}, known)
	if err == nil || !strings.Contains(err.Error(), "dance") || !strings.Contains(err.Error(), "H1") || !strings.Contains(err.Error(), "C1+X") {
		t.Errorf("error %v, want the unknown command and both bad keys", err)
	}
	if len(m.Bindings) != 1 || m.Bindings[0].Keys != "A0" {
		t.Errorf("kept %v, want only A0", m.Bindings)
	}
}

func TestLoadWritesTheDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lilypractice", "commands.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Channel != 16 || c.Keys["B0"] != "play-stop" {
		t.Errorf("loaded %+v, want the default", c)
	}
	again, err := Load(path)
	if err != nil || again.Keys["A0+C1"] != "start" {
		t.Errorf("reloaded %+v, %v; want the default, written", again, err)
	}
}
