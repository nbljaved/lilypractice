package score

import (
	"fmt"
	"testing"
)

func at(bar int64) Moment { return Moment{Main: Rat{bar, 1}, Grace: Rat{0, 1}} }

func n(i int) *int { return &i }

func order(s *Score, end int64) string {
	var out string
	for _, sp := range s.PlayOrder(Rat{end, 1}) {
		out += fmt.Sprintf("%d-%d/%d ", sp.From.Num/sp.From.Den, sp.To.Num/sp.To.Den, sp.Pass)
	}
	return out
}

func TestPlayOrder(t *testing.T) {
	for _, c := range []struct {
		name    string
		repeats []Repeat
		end     int64
		want    string // from-to/pass of each span, in bars of a whole note
	}{
		{"no repeats", nil, 4, "0-4/0 "},
		{"an end with no start goes back to the beginning",
			[]Repeat{{At: at(2), Kind: "end", Returns: n(1)}},
			4, "0-2/1 0-2/2 2-4/0 "},
		{"an ending for the first two passes, and one for the third",
			[]Repeat{
				{At: at(1), Kind: "start", Count: n(3)},
				{At: at(2), Kind: "alternative", Dir: n(-1), Numbers: []int{1, 2}},
				{At: at(3), Kind: "end", Returns: n(2)},
				{At: at(3), Kind: "alternative", Dir: n(0), Numbers: []int{3}},
				{At: at(4), Kind: "alternative", Dir: n(1)},
			},
			5, "0-1/0 1-3/1 1-3/2 1-2/3 3-4/3 4-5/0 "},
		{"D.C. al Fine",
			[]Repeat{
				{At: at(2), Kind: "fine"},
				{At: at(4), Kind: "dal-segno", Returns: n(1)},
			},
			4, "0-4/0 0-2/0 "},
		{"a repeat ending after the last note, past a closing rest",
			[]Repeat{
				{At: at(2), Kind: "end", Returns: n(1)},
				{At: at(2), Kind: "start", Count: n(2)},
				{At: at(5), Kind: "end", Returns: n(1)},
			},
			4, "0-2/1 0-2/2 2-5/1 2-5/2 "},
		{"a repeat at the very end",
			[]Repeat{
				{At: at(2), Kind: "start", Count: n(2)},
				{At: at(4), Kind: "end", Returns: n(1)},
			},
			4, "0-2/0 2-4/1 2-4/2 "},
	} {
		s := &Score{Repeats: uniqueRepeats(c.repeats)}
		if got := order(s, c.end); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
}

func TestRepeatCopiesFromEveryStaffCountOnce(t *testing.T) {
	end := Repeat{At: at(2), Kind: "end", Returns: n(1)}
	s := &Score{Repeats: uniqueRepeats([]Repeat{end, end})}
	if got, want := order(s, 4), "0-2/1 0-2/2 2-4/0 "; got != want {
		t.Errorf("%s, want %s", got, want)
	}
}

func TestTremolosArePlayedOut(t *testing.T) {
	r := func(n, d int64) Rat { return Rat{n, d} }
	eighth := r(1, 8)
	s := &Score{
		Notes: []Note{
			// \repeat tremolo 4 c8: written c2, struck four times.
			{ID: "n1", At: Moment{r(0, 1), r(0, 1)}, Duration: r(1, 2), Pitch: 60, Tremolo: &eighth},
			// \repeat tremolo 2 { d16 f }: written d8 f8, alternating.
			{ID: "n2", At: Moment{r(1, 2), r(0, 1)}, Duration: r(1, 8), Pitch: 62},
			{ID: "n3", At: Moment{r(5, 8), r(0, 1)}, Duration: r(1, 8), Pitch: 65},
			{ID: "n4", At: Moment{r(3, 4), r(0, 1)}, Duration: r(1, 4), Pitch: 67},
		},
		Tremolos: []TremoloSpan{{From: Moment{r(1, 2), r(0, 1)}, To: Moment{r(3, 4), r(0, 1)}, Interval: r(1, 16)}},
	}
	s.playTremolos()
	var got string
	for _, n := range s.Notes {
		got += fmt.Sprintf("%s@%v/%v ", n.ID, n.At.Main.Big().RatString(), n.Pitch)
	}
	want := "n1@0/60 n1~2@1/8/60 n1~3@1/4/60 n1~4@3/8/60 " +
		"n2@1/2/62 n2~2@5/8/62 n3@9/16/65 n3~2@11/16/65 n4@3/4/67 "
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if Head("n1~3") != "n1" || Head("n4") != "n4" {
		t.Error("Head doesn't find the note head")
	}
}
