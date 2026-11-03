// Command lpsim simulates a pianist: it plays one hand of a piece into a
// MIDI port at the written tempo, optionally with mistakes. Use it to try
// lilypractice without a MIDI keyboard, or to test it.
//
//	lpsim [-hand right] [-tempo 100] [-wrong 0.1] [-delay 0] [-to lilypractice:0] piece.ly
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"lilypractice/alsa"
	"lilypractice/lilypond"
	"lilypractice/practice"
	"lilypractice/score"
)

func main() {
	hand := flag.String("hand", "right", "the hand to play: right, left or both")
	tempo := flag.Float64("tempo", 100, "tempo, in percent of the written tempo")
	wrong := flag.Float64("wrong", 0, "chance (0-1) of hitting a wrong key before the right one")
	delay := flag.Duration("delay", 0, "wait this long before the first note")
	to := flag.String("to", "lilypractice:0", "MIDI port to play into (lilypractice:0 is its keyboard-in)")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: lpsim [flags] piece.ly")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *hand, *tempo/100, *wrong, *delay, *to); err != nil {
		fmt.Fprintln(os.Stderr, "lpsim:", err)
		os.Exit(1)
	}
}

type strike struct {
	at    time.Duration
	pitch int
	off   time.Duration
}

func run(lyPath, hand string, tempo, wrong float64, delay time.Duration, to string) error {
	want := map[string]practice.Hand{"right": practice.Right, "left": practice.Left, "both": practice.Both}
	h, ok := want[hand]
	if !ok {
		return fmt.Errorf("-hand %q: want right, left or both", hand)
	}
	name := strings.TrimSuffix(filepath.Base(lyPath), filepath.Ext(lyPath))
	out, err := lilypond.Compile(context.Background(), lyPath, filepath.Join("out", name), false)
	if err != nil {
		return err
	}
	s, err := score.Load(out.Notes)
	if err != nil {
		return err
	}

	clock := newClock(s, tempo)
	hands := practice.Hands(s.Staves)
	onsets := s.Onsets()
	var strikes []strike
	// Repeats are followed, as lilypractice does by default.
	var elapsed time.Duration
	for _, sp := range s.PlayOrder(s.End()) {
		from, to := sp.From.Float64(), sp.To.Float64()
		base := clock.at(from)
		for _, o := range onsets {
			if pos := o.At.Main.Float64(); pos < from || pos >= to {
				continue
			}
			for _, n := range o.Notes {
				if nh, ok := hands[n.Staff]; !ok || (h != practice.Both && nh != h) {
					continue
				}
				at := elapsed + clock.at(n.At.Main.Float64()+n.At.Grace.Float64()) - base
				length := clock.at(n.At.Main.Float64()+n.Duration.Float64()) - clock.at(n.At.Main.Float64())
				strikes = append(strikes, strike{at, n.Pitch, at + length*9/10})
			}
		}
		elapsed += clock.at(to) - base
	}

	seq, err := alsa.Open("lpsim")
	if err != nil {
		return err
	}
	defer seq.Close()
	port, err := seq.OutputPort("out")
	if err != nil {
		return err
	}
	if err := seq.ConnectTo(port, to); err != nil {
		return err
	}

	type event struct {
		at    time.Duration
		on    bool
		pitch int
	}
	var events []event
	for _, st := range strikes {
		if rand.Float64() < wrong {
			events = append(events, event{st.at - 80*time.Millisecond, true, st.pitch + 1},
				event{st.at - 40*time.Millisecond, false, st.pitch + 1})
		}
		events = append(events, event{st.at, true, st.pitch}, event{st.off, false, st.pitch})
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at < events[j].at })

	fmt.Printf("playing %d notes (%s hand) into %s\n", len(strikes), hand, to)
	start := time.Now().Add(delay)
	for _, ev := range events {
		time.Sleep(time.Until(start.Add(ev.at)))
		if ev.on {
			seq.NoteOn(port, 0, ev.pitch, 80)
		} else {
			seq.NoteOff(port, 0, ev.pitch)
		}
	}
	return nil
}

// clock turns score positions into time, following the tempo marks.
type clock struct {
	marks []mark
}

type mark struct {
	pos, secondsPerWhole float64
}

func newClock(s *score.Score, tempo float64) clock {
	c := clock{marks: []mark{{0, 60 / 15.0 / tempo}}} // ♩ = 60 until a tempo mark
	for i, t := range s.Tempos {
		m := mark{t.At.Main.Float64(), 60 / t.WholesPerMinute.Float64() / tempo}
		if i == 0 && m.pos == 0 {
			c.marks[0] = m
		} else {
			c.marks = append(c.marks, m)
		}
	}
	return c
}

func (c clock) at(pos float64) time.Duration {
	seconds := 0.0
	if pos < 0 { // grace notes before the first beat
		seconds = pos * c.marks[0].secondsPerWhole
	}
	for i, m := range c.marks {
		if m.pos >= pos {
			break
		}
		end := pos
		if i+1 < len(c.marks) {
			end = math.Min(pos, c.marks[i+1].pos)
		}
		seconds += (end - m.pos) * m.secondsPerWhole
	}
	return time.Duration(seconds * float64(time.Second))
}
