// Package midikeys turns keys of a MIDI keyboard into commands, such as
// play, back a bar or faster: single keys, and keys held together.
//
// The keys are usually a zone of their own (on the SL88, A0–E1 sent on
// channel 16), so that they never sound or count as notes. A keyboard
// without zones can give up some keys instead: with channel 0, the keys
// named are commands on any channel and the others play as usual.
//
// A key that is also part of keys held together acts when you let it go,
// so that holding it for a combination doesn't run it too. Other keys act
// when they go down.
package midikeys

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Config is what the commands file holds.
type Config struct {
	// Channel is the MIDI channel (1–16) of the command keys: all its
	// notes are commands, never played. 0 means any channel, and then
	// only the keys named are commands.
	Channel int
	// Keys maps a key ("A0", "C#1", "Bb0" or a MIDI note number like
	// "21") or keys held together ("A0+C1") to a command.
	Keys map[string]string
}

// Default is the file written when there is none: the white and black
// keys from A0 to E1.
const Default = `{
  "Channel": 16,
  "Keys": {
    "A0": "bar-back",
    "A#0": "next-mode",
    "B0": "play-stop",
    "C1": "bar-forward",
    "C#1": "next-hand",
    "D1": "slower",
    "D#1": "metronome",
    "E1": "faster",
    "A0+C1": "start",
    "D1+E1": "loop"
  }
}
`

// Binding is one entry of the file, for showing.
type Binding struct {
	Keys    string // as written
	Command string
	pitches []int
}

// Map follows the command keys going down and up and says which command
// to run. It is not safe for concurrent use.
type Map struct {
	Channel  int // as in Config
	Bindings []Binding

	single  map[int]string
	chords  map[string]string // pitches in order, e.g. "21+24"
	inChord map[int]bool
	held    map[int]bool // command keys down -> they have done their part
}

// New checks a Config. known says whether a command exists; every problem
// found is reported, and the Map holds the entries that were fine.
func New(c Config, known func(string) bool) (*Map, error) {
	m := &Map{
		Channel: c.Channel,
		single:  make(map[int]string),
		chords:  make(map[string]string),
		inChord: make(map[int]bool),
		held:    make(map[int]bool),
	}
	var errs []error
	if c.Channel < 0 || c.Channel > 16 {
		errs = append(errs, fmt.Errorf("channel %d: want 1 to 16, or 0 for any", c.Channel))
		m.Channel = 0
	}
	for keys, command := range c.Keys {
		if !known(command) {
			errs = append(errs, fmt.Errorf("%s: no command %q", keys, command))
			continue
		}
		var pitches []int
		for _, k := range strings.Split(keys, "+") {
			p, err := ParseNote(k)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", keys, err))
				pitches = nil
				break
			}
			pitches = append(pitches, p)
		}
		if pitches == nil {
			continue
		}
		slices.Sort(pitches)
		pitches = slices.Compact(pitches)
		if len(pitches) == 1 {
			m.single[pitches[0]] = command
		} else {
			m.chords[chordName(pitches)] = command
			for _, p := range pitches {
				m.inChord[p] = true
			}
		}
		m.Bindings = append(m.Bindings, Binding{keys, command, pitches})
	}
	slices.SortFunc(m.Bindings, func(a, b Binding) int {
		if len(a.pitches) != len(b.pitches) {
			return len(a.pitches) - len(b.pitches) // single keys first
		}
		return slices.Compare(a.pitches, b.pitches)
	})
	return m, errors.Join(errs...)
}

func chordName(pitches []int) string {
	var s []string
	for _, p := range pitches {
		s = append(s, strconv.Itoa(p))
	}
	return strings.Join(s, "+")
}

// Owns reports whether a note is a command key. channel counts from 0.
func (m *Map) Owns(channel, pitch int) bool {
	if m.Channel > 0 {
		return channel == m.Channel-1
	}
	_, ok := m.single[pitch]
	return ok || m.inChord[pitch]
}

// Press is a key going down. It says whether the key is a command key,
// and the command to run now, if any.
func (m *Map) Press(channel, pitch int) (command string, owned bool) {
	if !m.Owns(channel, pitch) {
		return "", false
	}
	if len(m.held) > 0 {
		// Keys held while another goes down were held for a combination.
		for p := range m.held {
			m.held[p] = true
		}
		m.held[pitch] = true
		if c, ok := m.chords[chordName(slices.Sorted(maps.Keys(m.held)))]; ok {
			return c, true
		}
		if m.inChord[pitch] {
			return "", true
		}
		return m.single[pitch], true
	}
	if m.inChord[pitch] {
		m.held[pitch] = false // its own command waits until it goes up
		return "", true
	}
	m.held[pitch] = true
	return m.single[pitch], true
}

// Release is a key going up: a key that waited runs its command now, if
// it wasn't used in a combination.
func (m *Map) Release(channel, pitch int) (command string, owned bool) {
	if !m.Owns(channel, pitch) {
		return "", false
	}
	used, down := m.held[pitch]
	delete(m.held, pitch)
	if down && !used {
		return m.single[pitch], true
	}
	return "", true
}

var noteBase = map[rune]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}

// ParseNote reads a note name in scientific pitch (C4 is middle C, MIDI
// note 60), with # or ♯ for sharp and b or ♭ for flat, or a MIDI note
// number.
func ParseNote(s string) (int, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > 127 {
			return 0, fmt.Errorf("note %d: want 0 to 127", n)
		}
		return n, nil
	}
	bad := fmt.Errorf("%q: want a note like A0, C#1 or Bb0, or a MIDI note number", s)
	letter, size := utf8.DecodeRuneInString(strings.ToUpper(s))
	base, ok := noteBase[letter]
	if !ok {
		return 0, bad
	}
	rest := s[size:]
	for {
		switch {
		case strings.HasPrefix(rest, "#"), strings.HasPrefix(rest, "b"):
			base += map[byte]int{'#': 1, 'b': -1}[rest[0]]
			rest = rest[1:]
			continue
		case strings.HasPrefix(rest, "♯"):
			base++
			rest = rest[len("♯"):]
			continue
		case strings.HasPrefix(rest, "♭"):
			base--
			rest = rest[len("♭"):]
			continue
		}
		break
	}
	octave, err := strconv.Atoi(rest)
	if err != nil {
		return 0, bad
	}
	n := (octave+1)*12 + base
	if n < 0 || n > 127 {
		return 0, fmt.Errorf("%q: beyond MIDI's notes", s)
	}
	return n, nil
}

// Load reads the commands file, first writing Default there if there is
// no file yet.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		b = []byte(Default)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
			os.WriteFile(path, b, 0o644)
		}
	} else if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}
