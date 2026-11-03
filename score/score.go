// Package score reads the JSON note list written by the LilyPond plugin.
package score

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"sort"
	"strings"
)

// Rat is an exact fraction of a whole note, written in JSON as [num, den].
type Rat struct{ Num, Den int64 }

func (r *Rat) UnmarshalJSON(b []byte) error {
	var pair [2]int64
	if err := json.Unmarshal(b, &pair); err != nil {
		return err
	}
	if pair[1] == 0 {
		return fmt.Errorf("zero denominator in %s", b)
	}
	r.Num, r.Den = pair[0], pair[1]
	return nil
}

func (r Rat) Big() *big.Rat    { return big.NewRat(r.Num, r.Den) }
func (r Rat) Float64() float64 { return float64(r.Num) / float64(r.Den) }
func (r Rat) Cmp(s Rat) int    { return r.Big().Cmp(s.Big()) }
func (r Rat) Add(s Rat) Rat {
	sum := new(big.Rat).Add(r.Big(), s.Big())
	return Rat{sum.Num().Int64(), sum.Denom().Int64()}
}

// Moment is a point in the score: the main time, plus a negative grace part
// for grace notes, which come just before their main time.
// Written in JSON as [[num, den], [num, den]].
type Moment struct{ Main, Grace Rat }

func (m *Moment) UnmarshalJSON(b []byte) error {
	var pair [2]Rat
	if err := json.Unmarshal(b, &pair); err != nil {
		return err
	}
	m.Main, m.Grace = pair[0], pair[1]
	return nil
}

func (m Moment) Cmp(n Moment) int {
	if c := m.Main.Cmp(n.Main); c != 0 {
		return c
	}
	return m.Grace.Cmp(n.Grace)
}

type Note struct {
	ID       string `json:"id"`
	At       Moment `json:"at"`
	Duration Rat    `json:"duration"`
	Pitch    int    `json:"pitch"` // MIDI note number
	Staff    int    `json:"staff"`
	Voice    int    `json:"voice"`
	Bar      int    `json:"bar"`
	// Hidden notes sound but can't be seen: transparent, or without a
	// note head, as scores use them to carry a tie or a voice.
	Hidden bool `json:"hidden"`
	// Tremolo is how often a one-note tremolo strikes the note, as in
	// \repeat tremolo 4 c8, which is written c2 with a stroke.
	Tremolo *Rat `json:"tremolo"`
}

// TremoloSpan is a two-note tremolo, \repeat tremolo 4 { c16 e }: its
// notes are written one after the other, each for half the span, and
// alternate every Interval.
type TremoloSpan struct {
	From, To Moment
	Interval Rat
}

// Head is the id of the note head a note is drawn with. A tremolo strikes
// its note again and again: the strikes after the first have ids of their
// own, "n5~2", "n5~3"..., and share the first's head.
func Head(id string) string {
	if i := strings.IndexByte(id, '~'); i >= 0 {
		return id[:i]
	}
	return id
}

type Named struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	// Group is the staff group a staff is in, numbered: "PianoStaff0".
	Group string `json:"group"`
}

type Bar struct {
	At       Moment `json:"at"`
	Number   int    `json:"number"`
	Position Rat    `json:"position"` // measure position at At; negative in a pickup
}

type Tempo struct {
	At              Moment `json:"at"`
	WholesPerMinute Rat    `json:"wholesPerMinute"`
}

// Pedal is a pedal marking: "sustain", "sostenuto" or "una-corda" going
// down or coming up. A pedal change is an up and a down at the same moment.
type Pedal struct {
	At    Moment `json:"at"`
	Pedal string `json:"pedal"`
	Down  bool   `json:"down"`
}

type TimeSignature struct {
	At            Moment `json:"at"`
	Numerator     *int   `json:"numerator"` // nil for compound meters
	Denominator   *int   `json:"denominator"`
	MeasureLength Rat    `json:"measureLength"`
}

type Score struct {
	Version        int             `json:"version"`
	Staves         []Named         `json:"staves"`
	Voices         []Named         `json:"voices"`
	Notes          []Note          `json:"notes"`
	Ties           [][2]string     `json:"ties"` // [from id, to id]
	Bars           []Bar           `json:"bars"`
	Tempos         []Tempo         `json:"tempos"`
	Pedals         []Pedal         `json:"pedals"`
	Repeats        []Repeat        `json:"repeats"`
	Tremolos       []TremoloSpan   `json:"tremolos"`
	TimeSignatures []TimeSignature `json:"timeSignatures"`
	// LooseTies counts the ties Load dropped because a repeat lies between
	// their notes, as for a tie into a second ending: that note is played
	// rather than held over.
	LooseTies int `json:"-"`
}

// Load reads a score, with its tremolos played out as the strikes they
// stand for.
func Load(path string) (*Score, error) {
	s, err := LoadAsWritten(path)
	if err != nil {
		return nil, err
	}
	s.playTremolos()
	s.dropLooseTies()
	return s, nil
}

// LoadAsWritten reads a score with each tremolo a single note, as written
// (and as LilyPond's MIDI plays it, unless it unfolds repeats).
func LoadAsWritten(path string) (*Score, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Score
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	s.Repeats = uniqueRepeats(s.Repeats)
	s.dropLooseTies()
	return &s, nil
}

// playTremolos turns each tremolo into its strikes: a one-note tremolo
// into the note again and again, a two-note one into its notes in turn.
func (s *Score) playTremolos() {
	var notes []Note
	strikes := func(n Note, from, to, every *big.Rat, offset int64, step int64) {
		k := int64(0)
		for at := new(big.Rat).Set(from); at.Cmp(to) < 0; at.Add(at, every) {
			if k%step == offset {
				c := n
				if k > offset {
					c.ID = fmt.Sprintf("%s~%d", n.ID, k/step+1)
				}
				c.At.Main = Rat{at.Num().Int64(), at.Denom().Int64()}
				c.Duration = Rat{every.Num().Int64(), every.Denom().Int64()}
				notes = append(notes, c)
			}
			k++
		}
	}
	inTremoloSpan := func(n Note) (TremoloSpan, bool, bool) { // the span, and whether n is its second note
		for _, t := range s.Tremolos {
			from, to := t.From.Main.Big(), t.To.Main.Big()
			half := new(big.Rat).Quo(new(big.Rat).Sub(to, from), big.NewRat(2, 1))
			mid := new(big.Rat).Add(from, half)
			at := n.At.Main.Big()
			if n.Duration.Big().Cmp(half) == 0 && t.Interval.Num > 0 {
				switch {
				case at.Cmp(from) == 0:
					return t, false, true
				case at.Cmp(mid) == 0:
					return t, true, true
				}
			}
		}
		return TremoloSpan{}, false, false
	}
	for _, n := range s.Notes {
		if t, second, ok := inTremoloSpan(n); ok {
			offset := int64(0)
			if second {
				offset = 1
			}
			strikes(n, t.From.Main.Big(), t.To.Main.Big(), t.Interval.Big(), offset, 2)
			// The second note's strikes start with the span, one interval in.
			continue
		}
		if n.Tremolo != nil && n.Tremolo.Num > 0 && n.Tremolo.Cmp(n.Duration) < 0 && n.At.Grace.Num == 0 {
			start := n.At.Main.Big()
			strikes(n, start, new(big.Rat).Add(start, n.Duration.Big()), n.Tremolo.Big(), 0, 1)
			continue
		}
		notes = append(notes, n)
	}
	s.Notes = notes
}

// dropLooseTies drops the ties that cross a repeat's end or the start of
// an ending: the note they lead to is played after a jump, not straight
// on. A tie to a later note with nothing between, as when a broken chord's
// notes are held into the chord, stays.
func (s *Score) dropLooseTies() {
	byID := make(map[string]Note, len(s.Notes))
	for _, n := range s.Notes {
		byID[n.ID] = n
	}
	kept := s.Ties[:0]
	for _, t := range s.Ties {
		from, ok1 := byID[t[0]]
		to, ok2 := byID[t[1]]
		if ok1 && ok2 && from.At.Grace.Num == 0 && s.repeatBetween(from.At.Main.Add(from.Duration), to.At.Main) {
			s.LooseTies++
			continue
		}
		kept = append(kept, t)
	}
	s.Ties = kept
}

// End is where the music ends: the latest end of a note.
func (s *Score) End() Rat {
	end := Rat{0, 1}
	for _, n := range s.Notes {
		if e := n.At.Main.Add(n.Duration); e.Cmp(end) > 0 {
			end = e
		}
	}
	return end
}

// TiedTo reports the notes that continue a tie: they are drawn but not played.
func (s *Score) TiedTo() map[string]bool {
	to := make(map[string]bool, len(s.Ties))
	for _, t := range s.Ties {
		to[t[1]] = true
	}
	return to
}

// repeatBetween says whether a repeat ends, or an ending starts, from a up
// to b, or b is before a. A tie whose notes meet (into a first ending)
// crosses nothing.
func (s *Score) repeatBetween(a, b Rat) bool {
	switch b.Cmp(a) {
	case -1:
		return true
	case 0:
		return false
	}
	for _, r := range s.Repeats {
		if (r.Kind == "end" || r.Kind == "alternative") && r.At.Main.Cmp(a) >= 0 && r.At.Main.Cmp(b) <= 0 {
			return true
		}
	}
	return false
}

// Onset is the set of notes struck at the same moment.
type Onset struct {
	At    Moment
	Notes []Note
}

// Onsets groups the struck notes (not tie continuations) by moment, in order.
func (s *Score) Onsets() []Onset {
	tiedTo := s.TiedTo()
	var notes []Note
	for _, n := range s.Notes {
		if !tiedTo[n.ID] {
			notes = append(notes, n)
		}
	}
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.Cmp(notes[j].At) < 0 })
	var onsets []Onset
	for _, n := range notes {
		if len(onsets) == 0 || onsets[len(onsets)-1].At.Cmp(n.At) != 0 {
			onsets = append(onsets, Onset{At: n.At})
		}
		last := &onsets[len(onsets)-1]
		last.Notes = append(last.Notes, n)
	}
	return onsets
}
