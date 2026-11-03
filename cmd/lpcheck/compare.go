package main

import (
	"fmt"
	"math"
	"math/big"
	"slices"
	"sort"
	"strings"
	"time"

	"lilypractice/practice"
	"lilypractice/score"
)

// played is a note as the score plays it: when, in whole notes from the
// start, with repeats followed or not.
type played struct {
	at    *big.Rat
	pitch int
	staff int
	grace bool // a grace note: it comes just before at
}

// inOrder lists the struck notes span by span, each span's notes moved to
// where the span is played.
func inOrder(s *score.Score, spans []score.Span) []played {
	onsets := s.Onsets()
	var out []played
	offset := new(big.Rat)
	for _, sp := range spans {
		from, to := sp.From.Big(), sp.To.Big()
		for _, o := range onsets {
			main := o.At.Main.Big()
			if main.Cmp(from) < 0 || main.Cmp(to) >= 0 {
				continue
			}
			at := new(big.Rat).Add(offset, new(big.Rat).Sub(main, from))
			for _, n := range o.Notes {
				out = append(out, played{at, n.Pitch, n.Staff, n.At.Grace.Num != 0})
			}
		}
		offset.Add(offset, new(big.Rat).Sub(to, from))
	}
	return out
}

// matching is how well a list of notes matches the MIDI file.
type matching struct {
	matched, missing, extra, doubled, graces int
	examples                                 []string
}

// match pairs notes with the MIDI file's, up to tick limit (0: all). A
// note doubled at the same time on one staff (two voices in unison) counts
// once, as LilyPond's MIDI, with a track per staff, plays it once.
// Times are rounded to the nearest tick, as LilyPond's are; a note just
// after grace notes may come up to a quarter late, as they take their
// time from it.
func match(notes []played, m *midiFile, limit, shift int64) matching {
	type key struct {
		tick  int64
		pitch int
	}
	ticksPerWhole := big.NewRat(int64(4*m.ppq), 1)
	tickOf := func(at *big.Rat) int64 {
		f, _ := new(big.Rat).Mul(at, ticksPerWhole).Float64()
		return int64(math.Round(f)) + shift
	}
	var r matching
	afterGrace := map[int64]bool{}
	type staffKey struct {
		key
		staff int
	}
	seen := map[staffKey]bool{}
	var order []key // a note per staff
	for _, n := range notes {
		k := key{tickOf(n.at), n.pitch}
		if n.grace {
			r.graces++
			afterGrace[k.tick] = true
			continue
		}
		if seen[staffKey{k, n.staff}] {
			r.doubled++
			continue
		}
		seen[staffKey{k, n.staff}] = true
		order = append(order, k)
	}
	byPitch := map[int][]int64{}
	midiNotes := 0
	for _, n := range m.notes {
		if limit == 0 || n.tick < limit {
			byPitch[n.pitch] = append(byPitch[n.pitch], n.tick)
			midiNotes++
		}
	}
	for _, ticks := range byPitch {
		sort.Slice(ticks, func(i, j int) bool { return ticks[i] < ticks[j] })
	}
	used := map[key]int{}
	take := func(pitch int, lo, hi int64) bool {
		ticks := byPitch[pitch]
		i := sort.Search(len(ticks), func(i int) bool { return ticks[i] >= lo })
		for ; i < len(ticks) && ticks[i] <= hi; i++ {
			k := key{ticks[i], pitch}
			// Several MIDI notes may share a tick and pitch.
			if used[k] < countAt(ticks, ticks[i]) {
				used[k]++
				return true
			}
		}
		return false
	}
	sort.Slice(order, func(i, j int) bool { return order[i].tick < order[j].tick })
	for _, k := range order {
		slack := int64(1)
		if afterGrace[k.tick] {
			slack = int64(m.ppq)
		}
		if take(k.pitch, k.tick, k.tick) || take(k.pitch, k.tick-slack, k.tick+slack) {
			r.matched++
		} else {
			r.missing++
			if len(r.examples) < 5 {
				r.examples = append(r.examples, fmt.Sprintf("pitch %d at tick %d (bar %.2f of 4/4)", k.pitch, k.tick, float64(k.tick)/float64(4*m.ppq)+1))
			}
		}
	}
	total := 0
	for _, c := range used {
		total += c
	}
	r.extra = midiNotes - total
	return r
}

func countAt(ticks []int64, t int64) int {
	n := 0
	for _, x := range ticks {
		if x == t {
			n++
		}
	}
	return n
}

// compareMIDI checks the struck notes against LilyPond's MIDI file. Scores
// often unfold their repeats, and with them their tremolos, for MIDI only,
// so it tries the score with and without repeats followed (played) and
// tremolos struck out (s) or as written (w), and keeps the closest.
func compareMIDI(s, w *score.Score, path string, report func(string, ...any)) {
	m, err := readMIDI(path)
	if err != nil {
		report("%v", err)
		return
	}
	e := s.End()
	straight := []score.Span{{From: score.Rat{Num: 0, Den: 1}, To: e}}
	spans := s.PlayOrder(e)
	// LilyPond's unfolded MIDI goes on past Fine; the player stops there.
	var limit int64
	if last := spans[len(spans)-1]; last.To.Cmp(e) < 0 {
		length := new(big.Rat)
		for _, sp := range spans {
			length.Add(length, new(big.Rat).Sub(sp.To.Big(), sp.From.Big()))
		}
		f, _ := new(big.Rat).Mul(length, big.NewRat(int64(4*m.ppq), 1)).Float64()
		limit = int64(math.Round(f))
	}
	// A MIDI score may also begin with a lead-in the printed one hasn't:
	// line the first notes up too.
	var shifts []int64
	if len(m.notes) > 0 {
		first := m.notes[0].tick
		for _, n := range m.notes {
			first = min(first, n.tick)
		}
		shifts = append(shifts, 0, first)
	}
	var best matching
	var how string
	tries := 0
	for _, shift := range shifts {
		for _, try := range []struct {
			sc    *score.Score
			spans []score.Span
			limit int64
			how   string
		}{
			{w, straight, 0, "as written"},
			{w, spans, limit, "with the repeats followed"},
			{s, straight, 0, "with the tremolos struck out"},
			{s, spans, limit, "with the repeats followed and the tremolos struck out"},
		} {
			r := match(inOrder(try.sc, try.spans), m, try.limit+shift*min(try.limit, 1), shift)
			if tries == 0 || r.missing+r.extra < best.missing+best.extra {
				best, how = r, try.how
				if try.limit > 0 {
					how += ", up to Fine"
				}
				if shift != 0 {
					how += fmt.Sprintf(", after a lead-in of %d ticks", shift)
				}
			}
			tries++
		}
	}
	r := best
	fmt.Printf("  MIDI (%d ticks per quarter) matches %s: %d of %d MIDI notes; %d notes missing from it; %d MIDI notes unmatched (%d grace notes); %d doubled\n",
		m.ppq, how, r.matched, len(m.notes), r.missing, r.extra, r.graces, r.doubled)
	if len(spans) > 1 {
		fmt.Printf("  repeats: %d stretches in playing order\n", len(spans))
	}
	if r.missing > 0 {
		report("%d notes are not in LilyPond's MIDI, e.g. %s", r.missing, strings.Join(r.examples, "; "))
	}
	if r.extra > r.graces {
		report("%d MIDI notes are not in the score and are not grace notes", r.extra-r.graces)
	}
}

// checkEngine plays the score with the practice engine: in listen mode it
// must play every note, repeats followed, and finish; in wait mode a
// perfect player must get through with nothing wrong or missed. It also
// says which staff each hand reads.
func checkEngine(s *score.Score, report func(string, ...any)) {
	hands := practice.Hands(s.Staves)
	var who []string
	for _, st := range s.Staves {
		h, ok := hands[st.Index]
		what := "played for you"
		if ok {
			what = h.String()
		}
		who = append(who, fmt.Sprintf("%q: %s", st.Name, what))
	}
	fmt.Printf("  staves: %s\n", strings.Join(who, ", "))

	struck := len(inOrder(s, s.PlayOrder(s.End())))
	const step = 10 * time.Millisecond
	const limit = 3 * time.Hour

	e := practice.New(s, practice.Settings{Mode: practice.Listen, Hand: practice.Both})
	e.Start(0)
	ons := 0
	now := time.Duration(0)
	for ; now < limit && !e.View().Finished; now += step {
		for _, o := range e.Advance(now) {
			if o.On && o.Port == practice.ScoreOut {
				ons++
			}
		}
	}
	switch {
	case !e.View().Finished:
		report("listen mode did not finish within %v", limit)
	case ons != struck:
		report("listen mode played %d notes, want %d", ons, struck)
	default:
		fmt.Printf("  listen mode: %d notes in %v\n", ons, now.Round(time.Second))
	}

	e = practice.New(s, practice.Settings{Mode: practice.Wait, Hand: practice.Both})
	e.Start(0)
	for now = 0; now < limit && !e.View().Finished; now += step {
		e.Advance(now)
		if v := e.View(); v.Waiting {
			// Each key once: one key plays both hands' notes in unison.
			for _, p := range slices.Compact(slices.Sorted(slices.Values(v.ExpectedPitches))) {
				e.NoteOn(now, p, practice.MIDIKeyboard)
			}
		}
	}
	st := e.View().Stats
	switch {
	case !e.View().Finished:
		report("wait mode with a perfect player did not finish (stuck at %.3f)", e.View().Pos)
	case st.Wrong > 0 || st.Missed > 0:
		report("wait mode with a perfect player: %d wrong, %d missed", st.Wrong, st.Missed)
	default:
		fmt.Printf("  wait mode, both hands: %d notes played right\n", st.Perfect+st.Good)
	}
}
