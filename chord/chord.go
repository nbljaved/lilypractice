// Package chord names the chord that notes played together make, as a
// lead sheet writes it, and as Pianoteq shows it over its keyboard: C,
// Am7, Fdim, G7/B.
package chord

import "slices"

// The roots are spelled as the piano's keys are.
var names = [12]string{"C", "C♯", "D", "E♭", "E", "F", "F♯", "G", "A♭", "A", "B♭", "B"}

// kind is a kind of chord: its notes, in semitones above its root, and
// what its name adds to the root's.
type kind struct {
	notes  []int
	suffix string
}

// The kinds a chord can be. Of two readings of the same notes, both with
// the lowest note as root or neither, the earlier kind wins: A, C, E and G
// with A lowest are Am7, with C lowest C6.
var kinds = []kind{
	{[]int{0, 4, 7}, ""},
	{[]int{0, 3, 7}, "m"},
	{[]int{0, 3, 6}, "dim"},
	{[]int{0, 4, 8}, "aug"},
	{[]int{0, 5, 7}, "sus4"},
	{[]int{0, 2, 7}, "sus2"},
	{[]int{0, 4, 7, 10}, "7"},
	{[]int{0, 4, 7, 11}, "maj7"},
	{[]int{0, 3, 7, 10}, "m7"},
	{[]int{0, 3, 6, 9}, "dim7"},
	{[]int{0, 3, 6, 10}, "m7♭5"},
	{[]int{0, 3, 7, 11}, "m(maj7)"},
	{[]int{0, 4, 8, 10}, "7♯5"},
	{[]int{0, 5, 7, 10}, "7sus4"},
	{[]int{0, 4, 7, 9}, "6"},
	{[]int{0, 3, 7, 9}, "m6"},
	{[]int{0, 2, 4, 7}, "add9"},
	{[]int{0, 2, 3, 7}, "m(add9)"},
	{[]int{0, 2, 4, 7, 10}, "9"},
	{[]int{0, 2, 4, 7, 11}, "maj9"},
	{[]int{0, 2, 3, 7, 10}, "m9"},
	{[]int{0, 1, 4, 7, 10}, "7♭9"},
	{[]int{0, 3, 4, 7, 10}, "7♯9"},
	{[]int{0, 2, 4, 7, 9}, "6/9"},
	// Sevenths are often played without their fifth.
	{[]int{0, 4, 10}, "7"},
	{[]int{0, 4, 11}, "maj7"},
	{[]int{0, 3, 10}, "m7"},
	// A bare fifth, only with its root lowest.
	{[]int{0, 7}, "5"},
}

// Name is the chord that pitches make, MIDI notes in any order, or "" if
// they make none it knows: a single note doesn't, nor do most two. The
// lowest note is the root if the notes allow it; otherwise it follows a
// slash: C/E is C major with E lowest.
func Name(pitches []int) string {
	if len(pitches) == 0 {
		return ""
	}
	var played [12]bool
	for _, p := range pitches {
		played[p%12] = true
	}
	bass := slices.Min(pitches) % 12
	best, bestRank := "", 0
	for root := range 12 {
		if !played[root] {
			continue
		}
		for i, k := range kinds {
			if !makes(played, root, k.notes) {
				continue
			}
			name, rank := names[root]+k.suffix, i
			if root != bass {
				if len(k.notes) < 3 {
					continue
				}
				name, rank = name+"/"+names[bass], i+len(kinds)
			}
			if best == "" || rank < bestRank {
				best, bestRank = name, rank
			}
		}
	}
	return best
}

// makes says whether the notes played are those of a chord on root.
func makes(played [12]bool, root int, notes []int) bool {
	n := 0
	for _, p := range played {
		if p {
			n++
		}
	}
	if n != len(notes) {
		return false
	}
	for _, s := range notes {
		if !played[(root+s)%12] {
			return false
		}
	}
	return true
}
