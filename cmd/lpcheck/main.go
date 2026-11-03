// Command lpcheck compiles .ly files with the lilypractice plugin and checks
// that the SVG note heads, the JSON note list and LilyPond's own MIDI output
// agree with each other.
//
//	lpcheck [-out dir] piece.ly...
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"lilypractice/lilypond"
	"lilypractice/rsvg"
	"lilypractice/score"
)

func main() {
	outDir := flag.String("out", "out", "directory for compiled files")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: lpcheck [-out dir] piece.ly...")
		os.Exit(2)
	}
	ok := true
	for _, ly := range flag.Args() {
		if !check(ly, *outDir) {
			ok = false
		}
	}
	if !ok {
		os.Exit(1)
	}
}

func check(lyPath, outDir string) bool {
	name := strings.TrimSuffix(filepath.Base(lyPath), filepath.Ext(lyPath))
	fmt.Printf("%s\n", lyPath)
	problems := 0
	report := func(format string, args ...any) {
		problems++
		if problems <= 20 {
			fmt.Printf("  PROBLEM: "+format+"\n", args...)
		}
	}

	start := time.Now()
	out, err := lilypond.Compile(context.Background(), lyPath, filepath.Join(outDir, name), true)
	if err != nil {
		fmt.Printf("  PROBLEM: %v\n", err)
		return false
	}
	fmt.Printf("  compiled in %v: %d page(s)\n", time.Since(start).Round(time.Millisecond), len(out.Pages))
	if out.Converted != "" {
		fmt.Printf("  written for LilyPond %s: updated with convert-ly\n", out.Converted)
	}
	for _, e := range out.Errors {
		if strings.Contains(e, "lilypractice-plugin") {
			report("the lilypractice plugin itself has an error: %s", e)
		}
	}
	for i, e := range out.Errors {
		if i == 5 {
			fmt.Printf("  WARNING: ... %d LilyPond errors in all\n", len(out.Errors))
			break
		}
		fmt.Printf("  WARNING: LilyPond went on after an error: %s\n", e)
	}

	// A file may hold several scores, each with its own notes (and MIDI).
	var scores, written []*score.Score // tremolos played out, and as written
	var all []score.Note
	for _, path := range out.Scores {
		s, err := score.Load(path)
		if err != nil {
			fmt.Printf("  PROBLEM: %v\n", err)
			return false
		}
		w, _ := score.LoadAsWritten(path)
		scores, written = append(scores, s), append(written, w)
		all = append(all, w.Notes...)
	}
	midis := midiFiles(out.MIDI)

	// Every note in the JSON has exactly one note head in the SVG, and back.
	byID := make(map[string]score.Note, len(all))
	for _, n := range all {
		if _, dup := byID[n.ID]; dup {
			report("duplicate id %s in the JSON", n.ID)
		}
		byID[n.ID] = n
		if _, ok := out.NotePage[n.ID]; !ok {
			report("note %s is in the JSON but not in the SVG", n.ID)
		}
	}
	for id := range out.NotePage {
		if _, ok := byID[id]; !ok {
			report("note head %s is in the SVG but not in the JSON", id)
		}
	}
	// And is on a line of music with staves, which the cursor spans.
	staffed := map[int]bool{}
	for _, st := range out.Staves {
		staffed[st.System] = true
	}
	var lost []string
	for id, s := range out.NoteSystem {
		if s < 0 || !staffed[s] {
			lost = append(lost, id)
		}
	}
	if len(lost) > 0 {
		slices.Sort(lost)
		report("%d note heads aren't on a line of music with staves, e.g. %s", len(lost), lost[0])
	}

	for i, s := range scores {
		if len(scores) > 1 {
			fmt.Printf("  score %d of %d:\n", i+1, len(scores))
		}
		// Ties join equal pitches (Load has dropped those that don't
		// join a note to the next).
		for _, t := range s.Ties {
			from, ok1 := byID[t[0]]
			to, ok2 := byID[t[1]]
			switch {
			case !ok1 || !ok2:
				report("tie %v refers to unknown notes", t)
			case from.Pitch != to.Pitch:
				report("tie %s->%s joins pitches %d and %d", t[0], t[1], from.Pitch, to.Pitch)
			}
		}

		onsets := s.Onsets()
		struck, hidden := 0, 0
		for _, o := range onsets {
			struck += len(o.Notes)
		}
		for _, n := range s.Notes {
			if n.Hidden {
				hidden++
			}
		}
		fmt.Printf("  %d notes: %d struck at %d onsets, %d tie continuations, %d hidden; %d bars, %d staves, %d tempo marks\n",
			len(s.Notes), struck, len(onsets), len(s.Notes)-struck, hidden, len(s.Bars), len(s.Staves), len(s.Tempos))
		if s.LooseTies > 0 {
			fmt.Printf("  %d ties don't join a note to the next (e.g. into a second ending): their second note is played\n", s.LooseTies)
		}
		switch {
		case len(midis) == 0:
			fmt.Println("  no \\midi block, so no MIDI comparison")
		case len(midis) != len(scores):
			fmt.Printf("  %d MIDI files for %d scores: which is which isn't known, so no MIDI comparison\n", len(midis), len(scores))
		default:
			compareMIDI(s, written[i], midis[i], report)
		}
		checkEngine(s, report)
	}

	checkGeometry(all, out, report)

	if problems > 20 {
		fmt.Printf("  ... %d problems in total\n", problems)
	}
	if problems == 0 {
		fmt.Println("  OK")
	}
	return problems == 0
}

// midiFiles lists the MIDI files of a compile: LilyPond writes piece.midi
// for the first score with a \midi block, piece-1.midi for the next...
func midiFiles(first string) []string {
	if first == "" {
		return nil
	}
	files := []string{first}
	ext := filepath.Ext(first)
	for i := 1; ; i++ {
		p := strings.TrimSuffix(first, ext) + "-" + strconv.Itoa(i) + ext
		if _, err := os.Stat(p); err != nil {
			return files
		}
		files = append(files, p)
	}
}

// checkGeometry asks librsvg for the box of every note head and times it.
// Hidden notes have none.
func checkGeometry(notes []score.Note, out *lilypond.Output, report func(string, ...any)) {
	docs := make([]*rsvg.Doc, len(out.Pages))
	for i, p := range out.Pages {
		d, err := rsvg.Open(p)
		if err != nil {
			report("%v", err)
			return
		}
		defer d.Close()
		docs[i] = d
	}
	start := time.Now()
	type place struct {
		at           score.Moment
		pitch, staff int
	}
	drawn := map[place]bool{}
	var empty []score.Note
	for _, n := range notes {
		page, ok := out.NotePage[n.ID]
		if !ok {
			continue
		}
		r, err := docs[page].Bounds(n.ID, docs[page].Viewport())
		switch {
		case err != nil:
			report("note %s: %v", n.ID, err)
		case r.W <= 0 || r.H <= 0:
			empty = append(empty, n)
		default:
			drawn[place{n.At, n.Pitch, n.Staff}] = true
		}
	}
	// Two voices in unison share one note head: the other is left empty.
	merged := 0
	for _, n := range empty {
		switch {
		case drawn[place{n.At, n.Pitch, n.Staff}]:
			merged++
		case !n.Hidden:
			report("note %s has an empty box but isn't hidden or in unison with a drawn one", n.ID)
		}
	}
	if merged > 0 {
		fmt.Printf("  %d note heads are merged with another voice's in unison\n", merged)
	}
	elapsed := time.Since(start)
	fmt.Printf("  librsvg found boxes for %d note heads in %v (%.2f ms each)\n",
		len(notes), elapsed.Round(time.Millisecond),
		float64(elapsed.Microseconds())/1000/float64(max(len(notes), 1)))
}
