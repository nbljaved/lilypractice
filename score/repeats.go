package score

import (
	"fmt"
	"slices"
)

// Repeat is an event of a repeat's structure, as LilyPond reports it:
// a volta repeat's start and end, an alternative ending, and \repeat
// segno's segno, coda, D.S. (or D.C.) and Fine. Repeats written as plain
// bar lines or text ("D.C. al Fine") have none.
type Repeat struct {
	At   Moment `json:"at"`
	Kind string `json:"kind"` // start, end, alternative, segno, coda, dal-segno, fine
	// Count is how many times the music is played; Returns how many
	// times an end or a D.S. goes back.
	Count   *int `json:"count"`
	Returns *int `json:"returns"`
	// Dir places an alternative: -1 the first, 0 a later one, 1 just
	// after the last. Numbers are the passes it is played on.
	Dir     *int  `json:"dir"`
	Numbers []int `json:"numbers"`
}

// uniqueRepeats drops the copies LilyPond sends when every staff repeats,
// and orders the events: by time, and at the same time ends before
// alternatives, D.S., Fine, starts and marks, so that music is left
// before it is entered.
func uniqueRepeats(rs []Repeat) []Repeat {
	seen := map[string]bool{}
	var out []Repeat
	for _, r := range rs {
		key := fmt.Sprint(r.At, r.Kind, deref(r.Count), deref(r.Returns), deref(r.Dir), r.Numbers)
		if !seen[key] {
			seen[key] = true
			out = append(out, r)
		}
	}
	order := map[string]int{"end": 0, "alternative": 1, "dal-segno": 2, "fine": 3, "start": 4, "segno": 5, "coda": 6}
	slices.SortStableFunc(out, func(a, b Repeat) int {
		if c := a.At.Main.Cmp(b.At.Main); c != 0 {
			return c
		}
		return order[a.Kind] - order[b.Kind]
	})
	return out
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// Span is a stretch of the score played straight through, from From up to
// To. Pass is the time through the innermost repeat it is in, from 1; 0
// outside repeats.
type Span struct {
	From, To Rat
	Pass     int
}

// PlayOrder follows the repeats: it lists the stretches of the score in
// the order a player plays them, ending at end (the end of the music) or
// at Fine. Volta repeats go back as often as they say and take their
// alternative endings in turn; a D.S. goes back to the segno before it,
// or to the start (D.C.), and the music then ends at Fine. Coda jumps are
// not followed. A score without repeats is one span.
func (s *Score) PlayOrder(end Rat) []Span {
	evs := s.Repeats
	zero := Rat{0, 1}
	type repeat struct {
		start        Rat
		after        int // the event after its start, where going back resumes
		pass         int
		alternatives bool
	}
	var (
		stack    []*repeat
		spans    []Span
		from     = zero
		lastEnd  = zero // where the last repeat ended: an end without a start goes back here
		segnos   []Rat
		dsTimes  = map[int]int{}
		jumpedDS bool
	)
	pass := func() int {
		if len(stack) == 0 {
			return 0
		}
		return stack[len(stack)-1].pass
	}
	cut := func(at Rat) { // end the current span at at
		if from.Cmp(at) < 0 {
			spans = append(spans, Span{from, at, pass()})
		}
		from = at
	}
	firstAt := func(at Rat) int { // the first event at or after at
		return slices.IndexFunc(evs, func(e Repeat) bool { return e.At.Main.Cmp(at) >= 0 })
	}

	for i, steps := 0, 0; ; steps++ {
		if steps > 100000 || len(spans) > 10000 {
			return []Span{{zero, end, 0}} // something is wrong: play it straight
		}
		if i < 0 || i >= len(evs) {
			cut(end)
			return spans
		}
		// A repeat may end after the last note, past a closing rest.
		ev := evs[i]
		at := ev.At.Main
		switch ev.Kind {
		case "start":
			cut(at)
			stack = append(stack, &repeat{start: at, after: i + 1, pass: 1})
			i++

		case "end":
			if len(stack) == 0 { // the repeat began where the last one ended
				stack = append(stack, &repeat{start: lastEnd, after: firstAt(lastEnd), pass: 1})
			}
			r := stack[len(stack)-1]
			returns := 1
			switch {
			case ev.Returns != nil:
				returns = *ev.Returns
			case ev.Count != nil:
				returns = *ev.Count - 1
			}
			if r.pass <= returns {
				cut(at)
				r.pass++
				from, i = r.start, r.after
				continue
			}
			if !r.alternatives {
				cut(at)
				stack = stack[:len(stack)-1]
				lastEnd = at
			}
			i++

		case "alternative":
			if len(stack) == 0 {
				i++
				continue
			}
			r := stack[len(stack)-1]
			if ev.Dir != nil && *ev.Dir == 1 { // past the last alternative
				cut(at)
				stack = stack[:len(stack)-1]
				lastEnd = at
				i++
				continue
			}
			r.alternatives = true
			if len(ev.Numbers) == 0 || slices.Contains(ev.Numbers, r.pass) {
				i++
				continue
			}
			// Not this pass's ending: on to the one that is, or past them.
			j := i + 1
			for ; j < len(evs); j++ {
				e := evs[j]
				if e.Kind == "alternative" && (e.Dir != nil && *e.Dir == 1 || len(e.Numbers) == 0 || slices.Contains(e.Numbers, r.pass)) {
					break
				}
			}
			if j == len(evs) {
				i++
				continue
			}
			cut(at)
			from, i = evs[j].At.Main, j

		case "segno":
			segnos = append(segnos, at)
			i++

		case "dal-segno":
			returns := 1
			if ev.Returns != nil {
				returns = *ev.Returns
			}
			if dsTimes[i] >= returns {
				i++
				continue
			}
			dsTimes[i]++
			target := zero // D.C.
			for _, sg := range segnos {
				if sg.Cmp(at) < 0 {
					target = sg
				}
			}
			cut(at)
			stack, jumpedDS = nil, true
			from, i = target, firstAt(target)
			// The segno itself is seen again; it is the same one.
			for i >= 0 && i < len(evs) && evs[i].Kind == "segno" && evs[i].At.Main.Cmp(target) == 0 {
				i++
			}

		case "fine":
			if jumpedDS {
				cut(at)
				return spans
			}
			i++

		default: // coda
			i++
		}
	}
}
