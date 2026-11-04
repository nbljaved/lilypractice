package midikeys

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func anyCommand(string) bool { return true }

func newMap(t *testing.T, c Config) *Map {
	t.Helper()
	m, err := New(c, anyCommand)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func defaultMap(t *testing.T) *Map {
	t.Helper()
	var c Config
	if err := json.Unmarshal([]byte(Default), &c); err != nil {
		t.Fatal(err)
	}
	return newMap(t, c)
}

const (
	a0, as0, a3, b3, c4, d4, e4, c8 = 21, 22, 57, 59, 60, 62, 64, 108
	sustain                         = Pedal + 64
)

// enter takes command mode on with A0+A#0.
func enter(t *testing.T, m *Map) {
	t.Helper()
	m.Down(a0)
	m.Down(as0)
	m.Up(a0)
	m.Up(as0)
	if !m.Active() {
		t.Fatal("A0+A#0 didn't take command mode on")
	}
}

func want(t *testing.T, what string, got, want Result) {
	t.Helper()
	if got.Command != want.Command || got.Owned != want.Owned || !slices.Equal(got.Mistaken, want.Mistaken) {
		t.Errorf("%s: %+v, want %+v", what, got, want)
	}
}

func TestParseKey(t *testing.T) {
	for s, want := range map[string]int{
		"A0": 21, "a0": 21, "A#0": 22, "A♯0": 22, "Bb0": 22, "B♭0": 22,
		"C4": 60, "C-1": 0, "G9": 127, "21": 21, " E1 ": 28,
		"Sustain": Pedal + 64, "sustain": Pedal + 64, "Sostenuto": Pedal + 66, "Soft": Pedal + 67,
	} {
		if got, err := ParseKey(s); err != nil || got != want {
			t.Errorf("ParseKey(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"", "H2", "C", "C#", "Cx1", "128", "G#9", "A0+", "Pedal"} {
		if got, err := ParseKey(s); err == nil {
			t.Errorf("ParseKey(%q) = %d, want an error", s, got)
		}
	}
}

func TestTheFirstStepIsMusicUntilItIsComplete(t *testing.T) {
	m := defaultMap(t)
	want(t, "C4 outside command mode", m.Down(c4), Result{})
	want(t, "C4 up", m.Up(c4), Result{})
	want(t, "A0 down", m.Down(a0), Result{})
	want(t, "A#0 down, with A0", m.Down(as0), Result{Owned: true, Mistaken: []int{a0}})
	if !m.Active() || m.Path() != "A0+A♯0" {
		t.Errorf("active %v, path %q; want command mode after A0+A♯0", m.Active(), m.Path())
	}
	want(t, "A0 up: it went down as music", m.Up(a0), Result{})
	want(t, "A#0 up", m.Up(as0), Result{Owned: true})
}

func TestTheFirstStepCountsWhileOtherNotesAreHeld(t *testing.T) {
	m := defaultMap(t)
	m.Down(e4) // a note held in the other hand
	m.Down(a0)
	want(t, "A#0 with A0 and E4", m.Down(as0), Result{Owned: true, Mistaken: []int{a0}})
	want(t, "E4 up", m.Up(e4), Result{})
}

func TestACommandEndsCommandMode(t *testing.T) {
	m := defaultMap(t)
	enter(t, m)
	want(t, "C4 down", m.Down(c4), Result{Command: "play-stop", Owned: true})
	if m.Active() {
		t.Error("still in command mode after play-stop")
	}
	want(t, "C4 up: it went down as a command", m.Up(c4), Result{Owned: true})
	want(t, "C4 again: music", m.Down(c4), Result{})
}

func TestACommandThatStays(t *testing.T) {
	m := defaultMap(t)
	enter(t, m)
	for i := range 3 {
		if r := m.Down(b3); r.Command != "bar-back" {
			t.Errorf("B3, time %d: %+v, want bar-back", i+1, r)
		}
		m.Up(b3)
	}
	if !m.Active() {
		t.Error("bar-back ended command mode, but it stays")
	}
	want(t, "C4: play-stop doesn't stay", m.Down(c4), Result{Command: "play-stop", Owned: true})
	if m.Active() {
		t.Error("still in command mode after play-stop")
	}
}

func TestLeavingCommandMode(t *testing.T) {
	m := defaultMap(t)
	enter(t, m)
	m.Down(a0)
	want(t, "the first step again", m.Down(as0), Result{Owned: true})
	if m.Active() {
		t.Error("A0+A#0 again didn't end command mode")
	}
	m.Up(a0)
	m.Up(as0)

	enter(t, m)
	want(t, "C8 does nothing", m.Down(c8), Result{Owned: true})
	if m.Active() {
		t.Error("a key that does nothing didn't end command mode")
	}
	want(t, "C8 up", m.Up(c8), Result{Owned: true})
}

func TestNextShowsWhatTheKeysDo(t *testing.T) {
	m := newMap(t, Config{Keys: map[string]string{
		"A0+A#0 C4": "play-stop", "A0+A#0 B3": "bar-back", "A0+A#0 D4 E4": "loop",
	}, Stay: []string{"bar-back"}})
	if m.Next() != nil {
		t.Errorf("Next outside command mode: %v", m.Next())
	}
	enter(t, m)
	var got []string
	for _, b := range m.Next() {
		got = append(got, b.Keys+" "+b.Command+map[bool]string{true: " stays"}[b.Stay])
	}
	if want := "A0+A♯0 leave, B3 bar-back stays, C4 play-stop, D4 "; strings.Join(got, ", ") != want {
		t.Errorf("Next: %q, want %q", strings.Join(got, ", "), want)
	}
	m.Down(d4)
	if m.Path() != "A0+A♯0 D4" {
		t.Errorf("path %q after D4", m.Path())
	}
	if r := m.Down(e4); r.Command != "loop" || m.Active() {
		t.Errorf("E4 after D4: %+v, active %v; want loop, then out of command mode", r, m.Active())
	}
}

func TestPedals(t *testing.T) {
	m := newMap(t, Config{Keys: map[string]string{
		"A0+A#0 Sustain":        "play-stop",
		"Sustain+C8 C4":         "start",
		"Sustain+C8 Sustain+D4": "faster",
	}, Stay: []string{"faster"}})
	enter(t, m)
	want(t, "sustain down", m.Down(sustain), Result{Command: "play-stop", Owned: true})
	want(t, "sustain down still (its position again)", m.Down(sustain), Result{Owned: true})
	want(t, "sustain up", m.Up(sustain), Result{Owned: true})
	want(t, "sustain again: music", m.Down(sustain), Result{})

	// The pedal as part of the first step, kept down.
	want(t, "C8 with the pedal down", m.Down(c8), Result{Owned: true, Mistaken: []int{sustain}})
	m.Up(c8)
	for i := range 2 {
		if r := m.Down(d4); r.Command != "faster" {
			t.Errorf("D4 with the pedal, time %d: %+v, want faster", i+1, r)
		}
		m.Up(d4)
	}
	if r := m.Down(c4); r.Command != "start" || m.Active() {
		t.Errorf("C4 with the pedal: %+v, active %v; want start (Sustain+C4 isn't a step)", r, m.Active())
	}
	m.Up(c4)
	want(t, "sustain up: it went down as music", m.Up(sustain), Result{})
}

func TestKeysOfAStepOfSeveralActWhenTheyGoUp(t *testing.T) {
	m := newMap(t, Config{Keys: map[string]string{
		"A0+A#0 C4": "play-stop", "A0+A#0 C4+E4": "loop", "A0+A#0 A3": "start",
	}})
	enter(t, m)
	want(t, "C4 down: it waits", m.Down(c4), Result{Owned: true})
	want(t, "C4 up", m.Up(c4), Result{Command: "play-stop", Owned: true})

	enter(t, m)
	m.Down(c4)
	want(t, "E4 with C4", m.Down(e4), Result{Command: "loop", Owned: true})
	want(t, "C4 up after C4+E4", m.Up(c4), Result{Owned: true})
	m.Up(e4)

	enter(t, m)
	m.Down(c4)
	want(t, "A3 with C4 held", m.Down(a3), Result{Command: "start", Owned: true})
	want(t, "C4 up, held for A3", m.Up(c4), Result{Owned: true})
}

func TestProblemsReported(t *testing.T) {
	known := func(c string) bool { return c == "play-stop" || c == "start" }
	// Entries are read in order: "A#0+A0 C4" comes before "A0+A#0".
	m, err := New(Config{Keys: map[string]string{
		"A#0+A0 C4": "play-stop", "A0+A#0 B3": "dance", "A0+A#0 H1": "play-stop",
		"A0+A#0": "start", "B0 C1": "start", "B0 C1 D1": "start", "Bb0+A0 C4": "start",
	}, Stay: []string{"jump"}}, known)
	for _, problem := range []string{"dance", "H1", "A0+A#0: longer sequences", "B0 C1 D1: B0 C1 runs a command",
		"Bb0+A0 C4: the same keys", "jump"} {
		if err == nil || !strings.Contains(err.Error(), problem) {
			t.Errorf("error %v, want %q in it", err, problem)
		}
	}
	var kept []string
	for _, b := range m.Bindings {
		kept = append(kept, b.Keys)
	}
	if want := []string{"A#0+A0 C4", "B0 C1"}; !slices.Equal(kept, want) {
		t.Errorf("kept %v, want %v", kept, want)
	}
}

func TestLoadWritesTheDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lilypractice", "commands.json")
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Keys["A0+A#0 C4"] != "play-stop" || !slices.Contains(c.Stay, "bar-back") {
		t.Errorf("loaded %+v, want the default", c)
	}
	again, err := Load(path)
	if err != nil || again.Keys["A0+A#0 Sustain"] != "play-stop" {
		t.Errorf("reloaded %+v, %v; want the default, written", again, err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "commands.json")
	os.WriteFile(path, []byte(`{"Channel": 16, "Keys": {"A0": "bar-back"}}`), 0o644)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "Channel") {
		t.Errorf("the old format loaded: error %v, want one about Channel", err)
	}
}
