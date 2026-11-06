package chord

import "testing"

func TestName(t *testing.T) {
	for _, c := range []struct {
		pitches []int
		want    string
	}{
		{nil, ""},
		{[]int{60}, ""},
		{[]int{60, 72}, ""},
		{[]int{60, 67}, "C5"},
		{[]int{55, 60}, ""}, // a fourth
		{[]int{60, 61, 62}, ""},
		{[]int{60, 64, 67}, "C"},
		{[]int{67, 60, 64}, "C"},
		{[]int{48, 60, 64, 67, 72}, "C"},
		{[]int{64, 67, 72}, "C/E"},
		{[]int{57, 60, 64}, "Am"},
		{[]int{53, 56, 59}, "Fdim"}, // as Pianoteq shows it
		{[]int{52, 56, 59}, "E"},    // Für Elise
		{[]int{60, 64, 68}, "Caug"},
		{[]int{60, 65, 67}, "Csus4"},
		{[]int{55, 59, 62, 65}, "G7"},
		{[]int{59, 62, 65, 67}, "G7/B"},
		{[]int{60, 64, 67, 71}, "Cmaj7"},
		{[]int{60, 64, 67, 69}, "C6"},
		{[]int{57, 60, 64, 67}, "Am7"},
		{[]int{59, 62, 65, 68}, "Bdim7"},
		{[]int{59, 62, 65, 69}, "Bm7♭5"},
		{[]int{48, 64, 70}, "C7"},
		{[]int{61, 65, 68}, "C♯"},
		{[]int{58, 62, 65}, "B♭"},
		{[]int{60, 62, 64, 67}, "Cadd9"},
		{[]int{48, 62, 64, 67, 70}, "C9"},
	} {
		if got := Name(c.pitches); got != c.want {
			t.Errorf("Name(%v) = %q, want %q", c.pitches, got, c.want)
		}
	}
}
