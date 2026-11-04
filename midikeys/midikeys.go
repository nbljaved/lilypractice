// Package midikeys turns keys and pedals of a MIDI keyboard into commands,
// such as play, back a bar or faster, without giving up any keys for
// them. Commands are sequences of steps, as in Emacs: "A0+A#0 C4" is A0
// and A#0 held together, then C4. The first step is keys you never play
// together; it takes command mode on, and while it is on the keys are
// commands, never music.
//
// A step is one key or pedal, or several held together. Keys held
// together act when the last of them goes down. A key that is also part
// of such a step acts alone when you let it go, so that holding it for
// the step doesn't also run it; other keys act when they go down.
//
// A command ends command mode, unless it is one that stays (such as
// bar-back: press its key again to go back another bar). The first step
// again ends it too, and so does a key that does nothing.
package midikeys

import (
	"bytes"
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

// Keys are MIDI note numbers (0–127), or a pedal: Pedal plus its
// controller number (see PedalKey).
const Pedal = 128

var pedalNames = map[int]string{64: "Sustain", 66: "Sostenuto", 67: "Soft"}

// PedalKey is the key of a pedal, from its controller number: 64 sustain,
// 66 sostenuto, 67 soft. ok is false for other controllers.
func PedalKey(controller int) (key int, ok bool) {
	_, ok = pedalNames[controller]
	return Pedal + controller, ok
}

// Config is what the commands file holds.
type Config struct {
	// Keys maps a sequence of steps, separated by spaces, to a command.
	// A step is a key ("A0", "C#1", "Bb0" or a MIDI note number like
	// "21"), a pedal ("Sustain", "Sostenuto", "Soft"), or several held
	// together, joined by + ("A0+A#0", "Sustain+C8").
	Keys map[string]string
	// Stay lists the commands that keep command mode on.
	Stay []string
}

// Default is the file written when there is none: A0 and A#0 together,
// then a white key around middle C, or the sustain pedal.
const Default = `{
  "Keys": {
    "A0+A#0 A3": "start",
    "A0+A#0 B3": "bar-back",
    "A0+A#0 C4": "play-stop",
    "A0+A#0 D4": "bar-forward",
    "A0+A#0 E4": "loop",
    "A0+A#0 F4": "slower",
    "A0+A#0 G4": "faster",
    "A0+A#0 A4": "metronome",
    "A0+A#0 B4": "next-hand",
    "A0+A#0 C5": "next-mode",
    "A0+A#0 Sustain": "play-stop"
  },
  "Stay": ["bar-back", "bar-forward", "note-back", "note-forward",
           "slower", "faster", "next-hand", "next-mode"]
}
`

// Leave is the Command, in Next, of the step that ends command mode.
const Leave = "leave"

// Binding is one entry of the file, for showing.
type Binding struct {
	Keys    string // as written
	Command string
	Stay    bool // the command keeps command mode on
	steps   [][]int
}

// Step is the keys of the binding's last step.
func (b Binding) Step() []int { return b.steps[len(b.steps)-1] }

// Result says what a key going down or up does.
type Result struct {
	Command string // the command to run now, or ""
	Owned   bool   // the key is the commands', not music
	// Mistaken holds the keys of a first step that were taken for music:
	// they went down before the step was complete.
	Mistaken []int
}

// Map follows the keys and pedals going down and up and says which
// command to run. It is not safe for concurrent use.
type Map struct {
	Bindings []Binding

	stay map[string]bool
	root *level // the first steps
	at   *level // the steps that can come next: root outside command mode
	path []*step
	held map[int]*heldKey
}

// level is the steps that can come at one point of a sequence.
type level struct {
	steps   map[string]*step // by stepName
	keys    map[int]bool     // the keys of all of them
	inChord map[int]bool     // the keys of steps of several keys
}

type step struct {
	keys    []int
	command string // what it runs; "" if it leads on, or leaves
	next    *level // where it leads, if anywhere
	leave   bool   // it ends command mode
}

type heldKey struct {
	owned   bool // it went down as one of the commands' keys
	waiting bool // it acts alone when it goes up
}

func newLevel() *level {
	return &level{steps: make(map[string]*step), keys: make(map[int]bool), inChord: make(map[int]bool)}
}

func (lv *level) add(st *step) {
	lv.steps[stepName(st.keys)] = st
	for _, k := range st.keys {
		lv.keys[k] = true
		if len(st.keys) > 1 {
			lv.inChord[k] = true
		}
	}
}

// addLeave lets keys end command mode here and in every level after,
// unless they already do something.
func (lv *level) addLeave(keys []int) {
	if _, ok := lv.steps[stepName(keys)]; !ok {
		lv.add(&step{keys: keys, leave: true})
	}
	for _, st := range lv.steps {
		if st.next != nil {
			st.next.addLeave(keys)
		}
	}
}

func stepName(keys []int) string {
	var s []string
	for _, k := range keys {
		s = append(s, strconv.Itoa(k))
	}
	return strings.Join(s, "+")
}

// New checks a Config. known says whether a command exists; every problem
// found is reported, and the Map holds the entries that were fine.
func New(c Config, known func(string) bool) (*Map, error) {
	m := &Map{stay: make(map[string]bool), root: newLevel(), held: make(map[int]*heldKey)}
	m.at = m.root
	var errs []error
	for _, command := range c.Stay {
		if !known(command) {
			errs = append(errs, fmt.Errorf("Stay: no command %q", command))
			continue
		}
		m.stay[command] = true
	}
	// In order, so that a problem is reported the same way every time.
	for _, keys := range slices.Sorted(maps.Keys(c.Keys)) {
		command := c.Keys[keys]
		if !known(command) {
			errs = append(errs, fmt.Errorf("%s: no command %q", keys, command))
			continue
		}
		steps, err := parseSequence(keys)
		if err == nil {
			err = m.add(steps, command)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", keys, err))
			continue
		}
		m.Bindings = append(m.Bindings, Binding{keys, command, m.stay[command], steps})
	}
	for _, first := range m.root.steps {
		if first.next != nil {
			first.next.addLeave(first.keys)
		}
	}
	sortBindings(m.Bindings)
	return m, errors.Join(errs...)
}

func parseSequence(s string) ([][]int, error) {
	var steps [][]int
	for _, field := range strings.Fields(s) {
		var keys []int
		for _, k := range strings.Split(field, "+") {
			key, err := ParseKey(k)
			if err != nil {
				return nil, err
			}
			keys = append(keys, key)
		}
		slices.Sort(keys)
		steps = append(steps, slices.Compact(keys))
	}
	if steps == nil {
		return nil, errors.New("no keys")
	}
	return steps, nil
}

// add puts a sequence in the tree of steps.
func (m *Map) add(steps [][]int, command string) error {
	lv := m.root
	for i, keys := range steps {
		last := i == len(steps)-1
		st := lv.steps[stepName(keys)]
		switch {
		case st == nil:
			st = &step{keys: keys}
			if last {
				st.command = command
			} else {
				st.next = newLevel()
			}
			lv.add(st)
		case last && st.next != nil:
			return errors.New("longer sequences start with these keys")
		case last:
			return errors.New("the same keys as another entry")
		case st.next == nil:
			return fmt.Errorf("%s runs a command already", sequenceName(steps[:i+1]))
		}
		lv = st.next
	}
	return nil
}

func sortBindings(b []Binding) {
	slices.SortFunc(b, func(x, y Binding) int {
		return slices.CompareFunc(x.steps, y.steps, slices.Compare)
	})
}

// Active reports whether command mode is on.
func (m *Map) Active() bool { return m.at != m.root }

// Path is the steps of command mode so far, such as "A0+A♯0"; "" when
// it is off.
func (m *Map) Path() string {
	var steps [][]int
	for _, st := range m.path {
		steps = append(steps, st.keys)
	}
	return sequenceName(steps)
}

// Next lists the steps that can come next in command mode, as Bindings of
// one step each; nil when it is off. Command is "" for a step that leads
// to further steps, and Leave for one that ends command mode.
func (m *Map) Next() []Binding {
	if !m.Active() {
		return nil
	}
	var next []Binding
	for _, st := range m.at.steps {
		b := Binding{KeysName(st.keys), st.command, m.stay[st.command], [][]int{st.keys}}
		if st.leave {
			b.Command, b.Stay = Leave, false
		}
		next = append(next, b)
	}
	sortBindings(next)
	return next
}

// Down is a key or pedal going down.
func (m *Map) Down(key int) Result {
	if h, ok := m.held[key]; ok {
		return Result{Owned: h.owned} // a pedal's position, again
	}
	// Keys held already were held for this one: none acts alone now.
	for _, o := range m.held {
		o.waiting = false
	}
	h := &heldKey{owned: m.Active()}
	m.held[key] = h
	lv := m.at
	if !lv.keys[key] {
		return m.nothing(h)
	}
	// Keys no step here uses, such as notes being played, don't count.
	var together []int
	for k := range m.held {
		if lv.keys[k] {
			together = append(together, k)
		}
	}
	if len(together) > 1 {
		slices.Sort(together)
		if st, ok := lv.steps[stepName(together)]; ok {
			return m.take(st, key, true, h)
		}
	}
	if lv.inChord[key] {
		h.waiting = len(together) == 1
		return Result{Owned: h.owned}
	}
	if st, ok := lv.steps[stepName([]int{key})]; ok {
		return m.take(st, key, true, h)
	}
	return m.nothing(h)
}

// Up is a key or pedal going up: a key that waited acts now.
func (m *Map) Up(key int) Result {
	h, ok := m.held[key]
	if !ok {
		return Result{}
	}
	delete(m.held, key)
	if !h.waiting {
		return Result{Owned: h.owned}
	}
	if st, ok := m.at.steps[stepName([]int{key})]; ok {
		return m.take(st, key, false, h)
	}
	return m.nothing(h)
}

// nothing is a key that does nothing: in command mode, that ends it.
func (m *Map) nothing(h *heldKey) Result {
	m.leaveIf(h.owned)
	return Result{Owned: h.owned}
}

// take does a step, completed by key going down or up.
func (m *Map) take(st *step, key int, down bool, h *heldKey) Result {
	for _, o := range m.held {
		o.waiting = false
	}
	r := Result{Command: st.command, Owned: h.owned}
	if !m.Active() {
		// The keys before were taken for music; the last is ours.
		for _, k := range st.keys {
			if down && k == key {
				h.owned, r.Owned = true, true
			} else {
				r.Mistaken = append(r.Mistaken, k)
			}
		}
	}
	switch {
	case st.next != nil:
		m.at = st.next
		m.path = append(m.path, st)
	default:
		m.leaveIf(st.leave || !m.stay[st.command])
	}
	return r
}

func (m *Map) leaveIf(leave bool) {
	if leave {
		m.at, m.path = m.root, nil
	}
}

var noteBase = map[rune]int{'C': 0, 'D': 2, 'E': 4, 'F': 5, 'G': 7, 'A': 9, 'B': 11}

var noteNames = [12]string{"C", "C♯", "D", "D♯", "E", "F", "F♯", "G", "G♯", "A", "A♯", "B"}

// KeysName writes keys held together, as in "A0+A♯0".
func KeysName(keys []int) string {
	var s []string
	for _, k := range keys {
		if k >= Pedal {
			s = append(s, pedalNames[k-Pedal])
		} else {
			s = append(s, fmt.Sprintf("%s%d", noteNames[k%12], k/12-1))
		}
	}
	return strings.Join(s, "+")
}

func sequenceName(steps [][]int) string {
	var s []string
	for _, keys := range steps {
		s = append(s, KeysName(keys))
	}
	return strings.Join(s, " ")
}

// ParseKey reads a key: a note name in scientific pitch (C4 is middle C,
// MIDI note 60), with # or ♯ for sharp and b or ♭ for flat; a MIDI note
// number; or a pedal: Sustain, Sostenuto or Soft.
func ParseKey(s string) (int, error) {
	s = strings.TrimSpace(s)
	for cc, name := range pedalNames {
		if strings.EqualFold(s, name) {
			return Pedal + cc, nil
		}
	}
	if n, err := strconv.Atoi(s); err == nil {
		if n < 0 || n > 127 {
			return 0, fmt.Errorf("note %d: want 0 to 127", n)
		}
		return n, nil
	}
	bad := fmt.Errorf("%q: want a note like A0, C#1 or Bb0, a MIDI note number, or Sustain, Sostenuto or Soft", s)
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
	// Unknown fields are an error, so that a misspelt one (or the
	// Channel of the old format) is reported rather than ignored.
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var c Config
	if err := d.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}
