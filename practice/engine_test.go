package practice

import (
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"lilypractice/score"
)

// The tests use the JSON that lpcheck writes for testdata/features.ly:
// 3/4 at ♩=90 (22.5 whole notes a minute), a pickup D5, then
// bar 1: G4 A4 B4 C5 (eighths) D5 over a left-hand G3+B3 chord.
func loadFeatures(t *testing.T) *score.Score {
	t.Helper()
	path := filepath.Join("..", "out", "features", "features.notes.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("run `out/bin/lpcheck testdata/features.ly` first to compile the test piece")
	}
	s, err := score.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type sent struct {
	at time.Duration
	Out
}

// run advances the engine in 1 ms steps and records what it sends.
func run(e *Engine, from, to time.Duration) []sent {
	var all []sent
	for now := from; now <= to; now += time.Millisecond {
		for _, o := range e.Advance(now) {
			all = append(all, sent{now, o})
		}
	}
	return all
}

func noteOns(all []sent, port Port) []sent {
	var ons []sent
	for _, s := range all {
		if s.On && s.Port == port {
			ons = append(ons, s)
		}
	}
	return ons
}

const whole = time.Second * 120 / 45 // at ♩ = 90: 22.5 whole notes a minute

func near(t *testing.T, what string, got, want time.Duration) {
	t.Helper()
	if math.Abs(float64(got-want)) > float64(2*time.Millisecond) {
		t.Errorf("%s at %v, want %v", what, got, want)
	}
}

func TestListenPlaysEverythingOnTime(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both})
	e.Start(0)
	all := run(e, 0, 20*time.Second)
	if !e.View().Finished {
		t.Fatal("did not finish")
	}
	ons := noteOns(all, ScoreOut)
	if len(ons) != 29 { // 32 notes, 3 of them tie continuations
		t.Fatalf("played %d notes, want 29", len(ons))
	}
	near(t, "first note", ons[0].at, 0)
	if ons[0].Pitch != 74 {
		t.Errorf("first note is %d, want D5 (74)", ons[0].Pitch)
	}
	near(t, "bar 1 G4", ons[1].at, whole/4)
	// Every note is released.
	down := map[[2]int]int{}
	for _, s := range all {
		k := [2]int{s.Channel, s.Pitch}
		if s.On {
			down[k]++
		} else if down[k] > 0 {
			down[k]--
		}
	}
	for k, n := range down {
		if n != 0 {
			t.Errorf("channel %d pitch %d left sounding", k[0], k[1])
		}
	}
}

func TestListenToOneHand(t *testing.T) {
	for hand, want := range map[Hand]string{
		Left:  "[55 59 43 45 47 48 50 52 54 55]",
		Right: "[74 67 69 71 72 74 71 74 72 71 69 67 69 67 76 72 74 71 72]",
	} {
		e := New(loadFeatures(t), Settings{Mode: Listen, Hand: hand})
		e.Start(0)
		var got []int
		for _, o := range noteOns(run(e, 0, 20*time.Second), ScoreOut) {
			got = append(got, o.Pitch)
		}
		if fmt.Sprint(got) != want {
			t.Errorf("listening to the %v played %v, want %v", hand, got, want)
		}
	}
}

func TestTieHoldsTheNote(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both})
	e.Start(0)
	all := run(e, 0, 20*time.Second)
	// D5 struck at 3/4 is tied to the D5 at 1, so it sounds until 5/4.
	var on, off time.Duration = -1, -1
	for _, s := range all {
		if s.Pitch == 74 && s.Port == ScoreOut {
			if s.On && s.at > whole/2 && on < 0 {
				on = s.at
			} else if !s.On && on >= 0 && off < 0 {
				off = s.at
			}
		}
	}
	near(t, "tied D5 on", on, whole*3/4)
	near(t, "tied D5 off", off, whole*5/4)
}

func TestTiedNoteLightsUpWhenReached(t *testing.T) {
	s := loadFeatures(t)
	// The D5 struck at 3/4 and the D5 at 1 it is tied to.
	byID := map[string]score.Note{}
	for _, n := range s.Notes {
		byID[n.ID] = n
	}
	var struck, held string
	for _, tie := range s.Ties {
		if n := byID[tie[0]]; n.Pitch == 74 && n.At.Main.Float64() == 0.75 {
			struck, held = tie[0], tie[1]
		}
	}
	if held == "" {
		t.Fatal("no tied D5 at 3/4 in the test piece")
	}
	e := New(s, Settings{Mode: Listen, Hand: Both})
	e.Start(0)
	lit := func() map[string]bool {
		on := map[string]bool{}
		for _, id := range e.View().Sounding {
			on[id] = true
		}
		return on
	}
	run(e, 0, whole*3/4+20*time.Millisecond)
	if on := lit(); !on[struck] || on[held] {
		t.Errorf("just after 3/4: struck note lit %v, tied note lit %v; want true, false", on[struck], on[held])
	}
	run(e, whole*3/4+21*time.Millisecond, whole+20*time.Millisecond)
	if on := lit(); !on[struck] || !on[held] {
		t.Errorf("just after 1: struck note lit %v, tied note lit %v; want both", on[struck], on[held])
	}
}

func TestWaitModeWaitsAndPlaysTheOtherHand(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Wait, Hand: Right})
	e.Start(0)
	if out := run(e, 0, 100*time.Millisecond); len(out) != 0 {
		t.Fatalf("sent %v before the first note was played", out)
	}
	if !e.View().Waiting {
		t.Fatal("not waiting for the pickup D5")
	}
	if got := e.View().Expected; len(got) != 1 || got[0] != "n1" {
		t.Fatalf("expected %v, want [n1]", got)
	}

	e.NoteOn(100*time.Millisecond, 74, MIDIKeyboard) // D5
	if e.View().Waiting {
		t.Fatal("still waiting after the right note")
	}
	// The music now runs from 100 ms; at the downbeat it waits for G4,
	// and the left-hand chord comes in when G4 is played.
	all := run(e, 101*time.Millisecond, 2*time.Second)
	if len(noteOns(all, ScoreOut)) != 0 {
		t.Fatal("left hand played before G4")
	}
	out, _ := e.NoteOn(2*time.Second, 67, MIDIKeyboard)
	var chord []int
	for _, o := range out {
		if o.On {
			chord = append(chord, o.Pitch)
		}
	}
	if len(chord) != 2 || chord[0]+chord[1] != 55+59 {
		t.Fatalf("after G4 the program played %v, want the G3+B3 chord", chord)
	}
	// The music waited 100 ms for D5 and about 1.2 s for G4: both good,
	// as more than 50 ms.
	if st := e.View().Stats; st.Perfect != 0 || st.Good != 2 || st.Wrong != 0 {
		t.Errorf("stats %+v, want 2 good", st)
	}
}

func TestWrongAndEarlyNotes(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Wait, Hand: Right})
	e.Start(0)
	e.Advance(0)
	e.NoteOn(10*time.Millisecond, 73, MIDIKeyboard) // C#5: wrong
	if st := e.View().Stats; st.Wrong != 1 || st.LastWrong != 73 {
		t.Fatalf("stats %+v, want one wrong note (73)", st)
	}
	e.NoteOn(20*time.Millisecond, 74, MIDIKeyboard)

	// G4 is due a quarter note later (~667 ms). Played 150 ms early, it
	// counts, and the music does not stop there.
	run(e, 21*time.Millisecond, 20*time.Millisecond+whole/4-150*time.Millisecond)
	e.NoteOn(20*time.Millisecond+whole/4-150*time.Millisecond, 67, MIDIKeyboard)
	run(e, 20*time.Millisecond+whole/4-149*time.Millisecond, 20*time.Millisecond+whole/4+50*time.Millisecond)
	v := e.View()
	if hits := v.Stats.Perfect + v.Stats.Good; hits != 2 {
		t.Errorf("hits = %d, want 2", hits)
	}
	if v.Waiting && len(v.Expected) > 0 && v.Expected[0] == "n2" {
		t.Error("waiting for G4 although it was played early")
	}
}

func TestComputerKeysAnyOctave(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Wait, Hand: Right, AnyOctave: true})
	e.Start(0)
	e.Advance(0)
	e.NoteOn(time.Millisecond, 74-24, ComputerKeys) // D3 for D5
	if e.View().Waiting {
		t.Fatal("a D in another octave from the computer keys should count")
	}

	e = New(loadFeatures(t), Settings{Mode: Wait, Hand: Right, AnyOctave: true})
	e.Start(0)
	e.Advance(0)
	e.NoteOn(time.Millisecond, 74-24, MIDIKeyboard) // a real keyboard must be exact
	if !e.View().Waiting {
		t.Fatal("a D in another octave from a MIDI keyboard should not count")
	}
}

func TestCountInAndMetronome(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both, CountIn: 1, Metronome: true})
	e.Start(0)
	all := run(e, 0, 3*time.Second)
	clicks := noteOns(all, ClickOut)
	// Count-in: three beats of 3/4, accent first. Then the metronome:
	// the pickup beat, then the downbeat of bar 1 (accented).
	wantPitch := []int{clickAccent, clickBeat, clickBeat, clickBeat, clickAccent}
	if len(clicks) < len(wantPitch) {
		t.Fatalf("only %d clicks", len(clicks))
	}
	for i, p := range wantPitch {
		near(t, "click", clicks[i].at, time.Duration(i)*whole/4)
		if clicks[i].Pitch != p {
			t.Errorf("click %d is %d, want %d", i, clicks[i].Pitch, p)
		}
	}
	near(t, "first note after count-in", noteOns(all, ScoreOut)[0].at, 3*whole/4)
}

func TestPlayModeScoresInTime(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Play, Hand: Right})
	e.Start(0)
	ms := time.Millisecond
	all := run(e, 0, 20*ms)
	if _, r := e.NoteOn(20*ms, 74, MIDIKeyboard); r != Perfect { // D5, due at 0
		t.Errorf("D5 20 ms late: %v, want perfect", r)
	}
	all = append(all, run(e, 21*ms, whole/4+150*ms)...)
	if _, r := e.NoteOn(whole/4+150*ms, 67, MIDIKeyboard); r != Good { // G4, 150 ms late
		t.Errorf("G4 150 ms late: %v, want good", r)
	}
	all = append(all, run(e, whole/4+151*ms, 1100*ms)...)
	if _, r := e.NoteOn(1100*ms, 61, MIDIKeyboard); r != Wrong {
		t.Errorf("C#4: %v, want wrong", r)
	}
	all = append(all, run(e, 1101*ms, 1700*ms)...)

	// A4 (due at 1000 ms) and B4 (1333 ms) were never played.
	st := e.View().Stats
	if st.Perfect != 1 || st.Good != 1 || st.Wrong != 1 || st.Missed != 2 || st.Streak != 0 || st.BestStreak != 2 {
		t.Errorf("stats %+v, want 1 perfect, 1 good, 1 wrong, 2 missed, best streak 2", st)
	}
	if missed := e.View().Missed; !slices.Contains(missed, "n5") {
		t.Errorf("missed %v, want A4 (n5) among them", missed)
	}
	// The music kept time: the left-hand chord came on the downbeat, and
	// your own notes were not played for you.
	for _, s := range noteOns(all, ScoreOut) {
		if s.Pitch == 74 || s.Pitch == 67 {
			t.Errorf("the program played your note %d", s.Pitch)
		}
	}
	near(t, "left-hand chord", noteOns(all, ScoreOut)[0].at, whole/4)
}

func TestMetronomeSound(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both, CountIn: 1,
		ClickAccent: 34, ClickBeat: 33, ClickVolume: 0.5})
	e.Start(0)
	ons := noteOns(run(e, 0, time.Second), ClickOut)
	if len(ons) < 2 {
		t.Fatalf("%d clicks, want the count-in", len(ons))
	}
	if ons[0].Pitch != 34 || ons[0].Velocity != 60 || ons[1].Pitch != 33 || ons[1].Velocity != 45 {
		t.Errorf("clicks %d at %d, %d at %d; want the bell (34) at 60, then the click (33) at 45",
			ons[0].Pitch, ons[0].Velocity, ons[1].Pitch, ons[1].Velocity)
	}
}

func TestGuidePlaysYourPartQuietly(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Play, Hand: Right, Guide: true})
	e.Start(0)
	ons := noteOns(run(e, 0, 10*time.Millisecond), ScoreOut)
	if len(ons) != 1 || ons[0].Pitch != 74 || ons[0].Velocity != guideVelocity {
		t.Fatalf("played %v, want D5 at the guide's velocity", ons)
	}
}

func TestLoopRepeatsBars(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both, Loop: true, LoopFrom: 2, LoopTo: 3})
	e.Start(0)
	all := run(e, 0, 8*time.Second) // bars 2-3 last about 3.8 s
	if e.View().Finished {
		t.Fatal("finished although looping")
	}
	count := map[int]int{}
	for _, s := range noteOns(all, ScoreOut) {
		count[s.Pitch]++
	}
	if count[43] < 2 { // G2, first note of bar 2
		t.Errorf("bar 2 played %d times, want at least 2", count[43])
	}
	if count[50] > 0 || count[52] > 0 { // D3 in bar 4, E3 in bar 5
		t.Errorf("played past the loop: %v", count)
	}
	near(t, "loop starts at bar 2", noteOns(all, ScoreOut)[0].at, 0)
}

func TestStartsAtTheBarLine(t *testing.T) {
	// Bach's first right-hand note comes after a 16th rest: the music must
	// start at the bar line, so the first note comes a 16th after it.
	path := filepath.Join("..", "out", "bach-invention-01", "bach-invention-01.notes.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("run `out/bin/lpcheck testdata/bach-invention-01.ly` first")
	}
	s, err := score.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	e := New(s, Settings{Mode: Listen, Hand: Both})
	e.Start(0)
	ons := noteOns(run(e, 0, time.Second), ScoreOut)
	if len(ons) == 0 {
		t.Fatal("nothing played")
	}
	near(t, "first note (♩ = 60)", ons[0].at, 250*time.Millisecond)
}

func TestPedalMarkingsPlay(t *testing.T) {
	// features.ly: pedal down at bar 1 (1/4), changed at bar 2 (1), up at
	// bar 3 (7/4), on both staves' channels.
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both})
	e.Start(0)
	all := run(e, 0, 20*time.Second)
	type change struct {
		at    time.Duration
		value int
	}
	var pedal []change
	for i, s := range all {
		if s.Controller == SustainPedal && s.Channel == 0 {
			pedal = append(pedal, change{s.at, s.Value})
			// A pedal going down comes after the notes struck with it.
			if s.Value == 127 && (i == 0 || !all[i-1].On && all[i-1].Controller == 0) {
				t.Errorf("pedal down at %v not right after new notes", s.at)
			}
		}
	}
	want := []change{{whole / 4, 127}, {whole, 0}, {whole, 127}, {whole * 7 / 4, 0}}
	if len(pedal) != len(want) {
		t.Fatalf("pedal changes %v, want %v", pedal, want)
	}
	for i, w := range want {
		near(t, "pedal", pedal[i].at, w.at)
		if pedal[i].value != w.value {
			t.Errorf("pedal change %d is %d, want %d", i, pedal[i].value, w.value)
		}
	}
}

func TestPedalRestoredWhenStartingInside(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both})
	e.SeekPos(1.5) // the pedal went down at bar 2's downbeat
	out := e.Start(0)
	if len(out) != 2 || out[0].Controller != SustainPedal || out[0].Value != 127 {
		t.Fatalf("start sent %v, want the pedal down on both channels", out)
	}
	if stop := e.Stop(); !slices.ContainsFunc(stop, func(o Out) bool { return o.Controller == SustainPedal && o.Value == 0 }) {
		t.Errorf("stop sent %v, want the pedal up", stop)
	}
}

func TestNotesOutsideYourKeyboardPlayThemselves(t *testing.T) {
	// A keyboard from C4 to C5: the pickup D5 is out of reach, so the
	// program plays it and waits only from G4 on.
	e := New(loadFeatures(t), Settings{Mode: Wait, Hand: Right, KeyLow: 60, KeyHigh: 72})
	e.Start(0)
	ons := noteOns(run(e, 0, time.Second), ScoreOut)
	if len(ons) == 0 || ons[0].Pitch != 74 {
		t.Fatalf("played %v, want the D5 played for you", ons)
	}
	if v := e.View(); !v.Waiting || len(v.Expected) != 1 || v.Expected[0] != "n2" {
		t.Fatalf("waiting %v for %v, want to wait for G4 (n2)", v.Waiting, v.Expected)
	}
}

func TestCountInHoldsTheCursorAndCountsDown(t *testing.T) {
	// Two bars of count-in, starting in the middle of the piece at bar 2.
	e := New(loadFeatures(t), Settings{Mode: Listen, Hand: Both, CountIn: 2})
	e.SeekPos(1)
	e.Start(0)
	var left []int
	for beat := 0; beat < 6; beat++ {
		now := time.Duration(beat) * whole / 4
		e.Advance(now + time.Millisecond)
		v := e.View()
		if !v.CountingIn || v.Pos != 1 || v.Bar != 2 {
			t.Fatalf("at %v: counting in %v at %v (bar %d), want the cursor held at bar 2", now, v.CountingIn, v.Pos, v.Bar)
		}
		left = append(left, v.BeatsLeft)
	}
	if want := []int{6, 5, 4, 3, 2, 1}; !slices.Equal(left, want) {
		t.Errorf("beats left %v, want %v", left, want)
	}
}

func TestPlayableHands(t *testing.T) {
	e := New(loadFeatures(t), Settings{Mode: Wait, Hand: Right})
	for _, h := range []Hand{Right, Left, Both} {
		if !e.Playable(h) {
			t.Errorf("%v not playable on a full piano", h)
		}
	}
	// The left hand's notes all lie below C4.
	e.Configure(Settings{Mode: Wait, Hand: Right, KeyLow: 60, KeyHigh: 72})
	if !e.Playable(Right) || e.Playable(Left) || e.Playable(Both) {
		t.Errorf("on C4–C5: right %v, left %v, both %v; want only the right hand",
			e.Playable(Right), e.Playable(Left), e.Playable(Both))
	}
}

// testdata/repeats.ly: c | repeat 2 { d e } alternatives { f } { g } | a |
// repeat 3 { b } | c | segno: d (Fine the second time) e, D.S. al Fine.
func loadRepeats(t *testing.T) *score.Score {
	t.Helper()
	path := filepath.Join("..", "out", "repeats", "repeats.notes.json")
	if _, err := os.Stat(path); err != nil {
		t.Skip("run `out/bin/lpcheck testdata/repeats.ly` first to compile the test piece")
	}
	s, err := score.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func listenPitches(e *Engine) []int {
	e.Start(0)
	var got []int
	for _, o := range noteOns(run(e, 0, time.Minute), ScoreOut) {
		got = append(got, o.Pitch)
	}
	return got
}

func TestRepeatsAreFollowed(t *testing.T) {
	e := New(loadRepeats(t), Settings{Mode: Listen, Hand: Both})
	want := []int{60, 62, 64, 65, 62, 64, 67, 69, 71, 71, 71, 72, 74, 76, 74}
	if got := listenPitches(e); !slices.Equal(got, want) {
		t.Errorf("played %v, want %v", got, want)
	}
	if !e.View().Finished {
		t.Error("not finished at Fine")
	}

	e = New(loadRepeats(t), Settings{Mode: Listen, Hand: Both, IgnoreRepeats: true})
	want = []int{60, 62, 64, 65, 67, 69, 71, 72, 74, 76}
	if got := listenPitches(e); !slices.Equal(got, want) {
		t.Errorf("ignoring repeats, played %v, want %v", got, want)
	}
}

func TestWaitModeFollowsRepeatsToTheEnd(t *testing.T) {
	// The written music ends before the D.S. sends it back: the piece is
	// not over when the last written note is played.
	e := New(loadRepeats(t), Settings{Mode: Wait, Hand: Both})
	e.Start(0)
	var played []int
	for now := time.Duration(0); now < 2*time.Minute && !e.View().Finished; now += 10 * time.Millisecond {
		e.Advance(now)
		if v := e.View(); v.Waiting {
			for _, p := range v.ExpectedPitches {
				e.NoteOn(now, p, MIDIKeyboard)
				played = append(played, p)
			}
		}
	}
	want := []int{60, 62, 64, 65, 62, 64, 67, 69, 71, 71, 71, 72, 74, 76, 74}
	if !slices.Equal(played, want) || !e.View().Finished {
		t.Errorf("waited for %v (finished %v), want %v", played, e.View().Finished, want)
	}
}

func TestRepeatPassIsShown(t *testing.T) {
	e := New(loadRepeats(t), Settings{Mode: Listen, Hand: Both, Tempo: 1})
	e.Start(0)
	// ♩ = 60: a 4/4 bar is 4 s. The second time through bars 2-3 starts
	// after bars 1-4, at 16 s.
	run(e, 0, 17*time.Second)
	if v := e.View(); v.Pass != 2 || v.Bar != 2 {
		t.Errorf("at 17 s: bar %d, pass %d; want bar 2, the second time", v.Bar, v.Pass)
	}
}

func TestLoopPlaysItsBarsAsWritten(t *testing.T) {
	// Bars 2-4 hold the repeat and its first ending; looped, they play
	// straight through, again and again.
	e := New(loadRepeats(t), Settings{Mode: Listen, Hand: Both, Loop: true, LoopFrom: 2, LoopTo: 4})
	e.Start(0)
	var got []int
	for _, o := range noteOns(run(e, 0, 30*time.Second), ScoreOut) {
		got = append(got, o.Pitch)
	}
	if want := []int{62, 64, 65, 62, 64, 65}; len(got) < 6 || !slices.Equal(got[:6], want) {
		t.Errorf("loop played %v, want %v again and again", got, want)
	}
}

func TestHands(t *testing.T) {
	named := func(i int, name, group string) score.Named { return score.Named{Index: i, Name: name, Group: group} }
	for _, c := range []struct {
		name   string
		staves []score.Named
		want   map[int]Hand
	}{
		{"a singer above the piano's two staves",
			[]score.Named{named(0, "", ""), named(1, "pianoUp", "PianoStaff0"), named(2, "pianoDown", "PianoStaff0")},
			map[int]Hand{1: Right, 2: Left}},
		{"a choir's bass beside a piano",
			[]score.Named{named(0, "bass", "ChoirStaff0"), named(1, "", "PianoStaff1"), named(2, "", "PianoStaff1")},
			map[int]Hand{1: Right, 2: Left}},
		{"names, no group", []score.Named{named(0, "lh", ""), named(1, "rh", "")},
			map[int]Hand{0: Left, 1: Right}},
		{"a piano whose upper staff is named for the left hand",
			[]score.Named{named(0, "left", "PianoStaff0"), named(1, "right", "PianoStaff0")},
			map[int]Hand{0: Left, 1: Right}},
		{"no names", []score.Named{named(0, "", ""), named(1, "", ""), named(2, "", "")},
			map[int]Hand{0: Right, 1: Left}},
		{"one staff", []score.Named{named(0, "", "")}, map[int]Hand{0: Right}},
	} {
		if got := Hands(c.staves); !maps.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLongGraceRunsDontRunBackwards(t *testing.T) {
	// An eighth note, then four grace sixteenths (a quarter's worth)
	// before the next beat: they must come after the eighth starts.
	r := func(n, d int64) score.Rat { return score.Rat{Num: n, Den: d} }
	note := func(id string, main, grace score.Rat, pitch int) score.Note {
		return score.Note{ID: id, At: score.Moment{Main: main, Grace: grace}, Duration: r(1, 8), Pitch: pitch}
	}
	s := &score.Score{
		Staves: []score.Named{{Index: 0}},
		Notes: []score.Note{
			note("a", r(1, 8), r(0, 1), 60),
			note("g1", r(1, 4), r(-4, 16), 62), note("g2", r(1, 4), r(-3, 16), 64),
			note("g3", r(1, 4), r(-2, 16), 65), note("g4", r(1, 4), r(-1, 16), 67),
			note("b", r(1, 4), r(0, 1), 69),
		},
	}
	e := New(s, Settings{Mode: Listen, Hand: Both})
	for i := 1; i < len(e.groups); i++ {
		if e.groups[i].pos <= e.groups[i-1].pos {
			t.Errorf("group %d at %.4f comes before group %d at %.4f", i, e.groups[i].pos, i-1, e.groups[i-1].pos)
		}
	}
	if first := e.groups[1].pos; first < 1.0/8+1.0/16-epsilon {
		t.Errorf("the grace notes start at %.4f, in the first half of the eighth note", first)
	}
}

// With no score loaded yet, the program runs an engine of an empty one.
func TestNothingToPlay(t *testing.T) {
	e := New(&score.Score{}, Settings{Mode: Wait, Hand: Right, CountIn: 1, Metronome: true})
	e.Start(0)
	run(e, 0, 3*time.Second)
	e.NoteOn(time.Second, 60, ComputerKeys)
	e.Seek(1)
	e.SeekPos(1)
	e.SetLoop(0, true, 1, 2)
	e.Stop()
	e.View()
	if e.Playable(Right) || len(e.Bars()) != 1 {
		t.Errorf("an empty score: playable %v, bars %v", e.Playable(Right), e.Bars())
	}
}
