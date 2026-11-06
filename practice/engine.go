// Package practice is the practice engine. It moves through the score in
// time, plays the parts you are not playing, and checks the notes you play:
// in wait mode it stops until you play them, in play mode it keeps time and
// scores you.
//
// It does no input or output itself: callers pass in the current time and
// the notes played, and send out the notes it returns. That keeps it free of
// MIDI and GTK, and lets the tests use made-up times.
//
// Positions are in whole notes from the start of the score.
package practice

import (
	"cmp"
	"math"
	"slices"
	"sort"
	"strings"
	"time"

	"lilypractice/score"
)

// Port says where an outgoing note goes.
type Port int

const (
	ScoreOut Port = iota // notes the program plays from the score
	ClickOut             // metronome and count-in
)

// Out is a note to send, or, when Controller is set, a controller change
// such as the sustain pedal.
type Out struct {
	Port       Port
	On         bool
	Channel    int
	Pitch      int
	Velocity   int
	Controller int // 0 for a note
	Value      int
}

// MIDI controllers for the three piano pedals.
const (
	SustainPedal   = 64
	SostenutoPedal = 66
	SoftPedal      = 67
)

type Mode int

const (
	Wait   Mode = iota // the music stops at each of your notes until you play them
	Play               // the music keeps time and your notes are scored
	Listen             // the program plays everything
)

func (m Mode) String() string { return [...]string{"Wait", "Play", "Listen"}[m] }

type Hand int

const (
	Right Hand = iota
	Left
	Both
)

func (h Hand) String() string { return [...]string{"right hand", "left hand", "both hands"}[h] }

// OtherStaff is the "hand" of a staff that is neither hand's, such as a
// third staff; the program always plays it.
const OtherStaff Hand = -1

type Source int

const (
	MIDIKeyboard Source = iota
	ComputerKeys
	Mouse // the piano on the screen, clicked
)

type Settings struct {
	Mode      Mode
	Hand      Hand
	Tempo     float64 // 1 = as written
	Metronome bool
	CountIn   int  // bars of clicks before the music starts; 0 for none
	AnyOctave bool // for computer keys: the right note in any octave counts
	Guide     bool // in play mode, also play your notes, quietly
	// The range of your keyboard; the program plays notes outside it.
	// Zero means the whole piano.
	KeyLow, KeyHigh int
	Loop            bool
	LoopFrom        int // bar numbers, both included
	LoopTo          int
	// IgnoreRepeats plays the music straight through, as written, rather
	// than going back at repeats and taking their endings in turn.
	IgnoreRepeats bool
	// The sound of the metronome and the count-in: General MIDI percussion
	// notes for the first beat of a bar and for the others (0: wood
	// blocks), and how loud (0: as normally, 1).
	ClickAccent, ClickBeat int
	ClickVolume            float64
	// ScorePedals plays the score's pedal marks in wait and play mode, in
	// place of your sustain pedal; in listen mode they are always played,
	// and yours does nothing. View still shows where the score's sustain
	// pedal is down.
	ScorePedals bool
}

// Result is how a key you pressed was judged.
type Result int

const (
	Ignored Result = iota // not scored: stopped, listen mode, or a note already played
	Perfect
	Good
	Wrong
)

const (
	perfectWindow          = 50 * time.Millisecond
	goodWindow             = 300 * time.Millisecond // also how early a note may be played
	defaultWholesPerMinute = 15.0                   // ♩ = 60, LilyPond's default
	scoreVelocity          = 80
	guideVelocity          = 50
	clickAccent            = 76
	clickBeat              = 77
	clickLength            = 1.0 / 64
	epsilon                = 1e-9
)

type note struct {
	id     string
	pitch  int
	staff  int
	end    float64 // when it is released, after any ties
	hidden bool    // can't be seen, so always played for you
}

// tied is a note a struck note is held into by a tie: it isn't struck itself.
type tied struct {
	id  string
	pos float64
}

type group struct {
	pos   float64 // main time plus any grace part: grace notes come before
	main  float64
	notes []note
	mine  []int // indices into notes that you play
}

type click struct {
	pos    float64
	accent bool
}

type meter struct{ pos, length, beat float64 }

type tempo struct{ pos, wholesPerMinute float64 }

// Bar is where a bar starts; a pickup belongs to the bar after it.
type Bar struct {
	Number int
	Pos    float64
}

type sounding struct {
	port    Port
	channel int
	pitch   int
}

type pendingOff struct {
	pos float64
	s   sounding
	id  string
}

// due is one of your notes whose time has come in play mode but which you
// have not played yet.
type due struct {
	id    string
	pitch int
	at    time.Duration
}

type Stats struct {
	Perfect, Good, Missed, Wrong int
	Streak, BestStreak           int
	LastWrong                    int // pitch of the last wrong key, -1 if none yet
}

// Accuracy is the share of your notes played right, in percent, counting
// wrong keys against you; -1 before anything was played.
func (s Stats) Accuracy() float64 {
	total := s.Perfect + s.Good + s.Missed + s.Wrong
	if total == 0 {
		return -1
	}
	return 100 * float64(s.Perfect+s.Good) / float64(total)
}

// pedalMark is a pedal going down or up in the score.
type pedalMark struct {
	pos        float64
	controller int
	down       bool
}

type Engine struct {
	set       Settings
	staffHand map[int]Hand
	noteStaff map[string]int    // note id -> staff
	tiedOn    map[string][]tied // note id -> the notes tied on from it
	playable  [3]bool           // by Hand: you would have notes to play
	groups    []group
	clicks    []click
	meters    []meter
	tempos    []tempo
	bars      []Bar
	pedals    []pedalMark
	sustained bool  // the score marks the sustain pedal
	channels  []int // MIDI channels the score plays on
	end       float64
	spans     []span // the order the music is played in, repeats followed

	playing, waiting, finished bool
	nextPedal                  int
	pedalDown                  map[int]bool // controller -> down, as sent
	scorePedal                 map[int]bool // controller -> down in the score here, sent or not
	yourPedal                  bool         // your sustain pedal is down
	pos                        float64
	last                       time.Duration
	waitStart                  time.Duration
	next                       int // next group to reach
	nextClick                  int
	countIn                    []click
	countInEnd, countInBeat    float64 // where the music starts, and the count-in's beat
	loopFrom, loopTo           float64
	si                         int          // the span being played
	offs                       []pendingOff // sorted by pos
	sounding                   map[sounding]string
	hit                        map[string]bool
	missed                     map[string]bool
	due                        []due
	stats                      Stats
	lastHand                   Hand // of the note the last right key matched
}

func New(s *score.Score, set Settings) *Engine {
	if set.Tempo <= 0 {
		set.Tempo = 1
	}
	e := &Engine{
		set:        set,
		staffHand:  Hands(s.Staves),
		noteStaff:  make(map[string]int),
		tiedOn:     make(map[string][]tied),
		sounding:   make(map[sounding]string),
		hit:        make(map[string]bool),
		missed:     make(map[string]bool),
		pedalDown:  make(map[int]bool),
		scorePedal: make(map[int]bool),
		stats:      Stats{LastWrong: -1},
	}

	byID := make(map[string]score.Note, len(s.Notes))
	for _, n := range s.Notes {
		byID[n.ID] = n
	}
	tiedTo := make(map[string]string, len(s.Ties))
	for _, t := range s.Ties {
		tiedTo[t[0]] = t[1]
	}
	endRat := score.Rat{Num: 0, Den: 1}
	for _, o := range s.Onsets() {
		g := group{pos: position(o.At), main: o.At.Main.Float64()}
		for _, n := range o.Notes {
			last := n
			for to, ok := tiedTo[last.ID]; ok; to, ok = tiedTo[last.ID] {
				last = byID[to]
				e.tiedOn[n.ID] = append(e.tiedOn[n.ID], tied{to, position(last.At)})
			}
			end := position(last.At) + last.Duration.Float64()
			g.notes = append(g.notes, note{id: n.ID, pitch: n.Pitch, staff: n.Staff, end: end, hidden: n.Hidden})
			e.noteStaff[n.ID] = n.Staff
			e.end = math.Max(e.end, end)
			if ends := last.At.Main.Add(last.Duration); ends.Cmp(endRat) > 0 {
				endRat = ends
			}
		}
		e.groups = append(e.groups, g)
	}
	e.fitGraceNotes()
	for _, sp := range s.PlayOrder(endRat) {
		e.spans = append(e.spans, span{e.startOf(sp.From.Float64()), e.startOf(sp.To.Float64()), sp.Pass})
	}
	e.choosePlayerNotes()
	e.findPlayable()

	for _, t := range s.Tempos {
		e.tempos = append(e.tempos, tempo{position(t.At), t.WholesPerMinute.Float64()})
	}
	for _, ts := range s.TimeSignatures {
		e.meters = append(e.meters, meter{position(ts.At), ts.MeasureLength.Float64(), beatOf(ts)})
	}
	for _, b := range s.Bars {
		if p := position(b.At); p < e.end-epsilon && (len(e.bars) == 0 || b.Number != e.bars[len(e.bars)-1].Number) {
			e.bars = append(e.bars, Bar{b.Number, p})
		}
	}
	if len(e.bars) == 0 {
		e.bars = []Bar{{1, 0}}
	}
	e.clicks = e.buildClicks(s.Bars)
	e.placeLoop()

	// Pedals act on every staff's channel, like a piano's.
	controllers := map[string]int{"sustain": SustainPedal, "sostenuto": SostenutoPedal, "una-corda": SoftPedal}
	for _, p := range s.Pedals {
		if cc, ok := controllers[p.Pedal]; ok {
			e.pedals = append(e.pedals, pedalMark{position(p.At), cc, p.Down})
			e.sustained = e.sustained || cc == SustainPedal
		}
	}
	sort.SliceStable(e.pedals, func(i, j int) bool { return e.pedals[i].pos < e.pedals[j].pos })
	for _, st := range s.Staves {
		e.channels = append(e.channels, channelOf(st.Index))
	}
	return e
}

func (e *Engine) pedalOut(cc int, down bool) []Out {
	value := 0
	if down {
		value = 127
	}
	e.pedalDown[cc] = down
	var out []Out
	for _, ch := range e.channels {
		out = append(out, Out{Port: ScoreOut, Channel: ch, Controller: cc, Value: value})
	}
	return out
}

// youPedal says whether your sustain pedal is the one that counts, and
// not the score's: never in listen mode, and in wait and play mode unless
// the score's pedal marks are played, or if it has none. Only one counts
// at a time, and it holds every note, yours and the music's, as a
// piano's pedal does.
func (e *Engine) youPedal() bool {
	return e.set.Mode != Listen && (!e.set.ScorePedals || !e.sustained)
}

// scorePedalTo moves one of the score's pedals, and plays it unless you
// pedal.
func (e *Engine) scorePedalTo(cc int, down bool) []Out {
	e.scorePedal[cc] = down
	if e.youPedal() || e.pedalDown[cc] == down {
		return nil
	}
	return e.pedalOut(cc, down)
}

// YourPedal takes your sustain pedal going down or up. If it counts (see
// youPedal) it is played on the music's channels too, to hold its notes;
// counts says whether it does.
func (e *Engine) YourPedal(down bool) (out []Out, counts bool) {
	e.yourPedal = down
	if !e.youPedal() {
		return nil, false
	}
	if e.pedalDown[SustainPedal] == down {
		return nil, true
	}
	return e.pedalOut(SustainPedal, down), true
}

// placePedals readies the score's pedals for playing from pos: those
// already down before pos are pressed now.
func (e *Engine) placePedals(pos float64) []Out {
	e.nextPedal = sort.Search(len(e.pedals), func(i int) bool { return e.pedals[i].pos >= pos-epsilon })
	clear(e.scorePedal)
	for _, p := range e.pedals[:e.nextPedal] {
		e.scorePedal[p.controller] = p.down
	}
	return e.pressPedals()
}

// pressPedals plays the pedals that are down here: the score's, or yours
// if it counts.
func (e *Engine) pressPedals() []Out {
	if e.youPedal() {
		if e.yourPedal && !e.pedalDown[SustainPedal] {
			return e.pedalOut(SustainPedal, true)
		}
		return nil
	}
	var out []Out
	for cc, down := range e.scorePedal {
		if down {
			out = append(out, e.scorePedalTo(cc, true)...)
		}
	}
	return out
}

// liftPedals releases every pedal the program holds down.
func (e *Engine) liftPedals() []Out {
	var out []Out
	for cc, down := range e.pedalDown {
		if down {
			out = append(out, e.pedalOut(cc, false)...)
		}
	}
	return out
}

// position places grace notes just before their main note.
func position(m score.Moment) float64 { return m.Main.Float64() + m.Grace.Float64() }

// Hands decides which staff each hand reads. A piano's own two staves (a
// PianoStaff or GrandStaff) come first, so that a singer's or a horn's
// staff above them is played for you; then staves named for each hand
// ("right", "lh", "upper", "pianoDown", "links"...); then the first
// two. The upper of two staves is the right hand's unless names say
// otherwise. Other staves are always played by the program.
func Hands(staves []score.Named) map[int]Hand {
	byName := func(st score.Named) (Hand, bool) {
		name := strings.ToLower(st.Name)
		for _, w := range []string{"right", "upper", "up", "treble", "rechts", "oben", "droite", "dessus", "destra"} {
			if strings.Contains(name, w) {
				return Right, true
			}
		}
		for _, w := range []string{"left", "lower", "down", "bass", "links", "unten", "gauche", "dessous", "sinistra"} {
			if strings.Contains(name, w) {
				return Left, true
			}
		}
		switch name {
		case "rh", "md", "r":
			return Right, true
		case "lh", "mg", "l":
			return Left, true
		}
		return 0, false
	}
	// pair gives two staves, top first, to the hands.
	pair := func(top, bottom score.Named) map[int]Hand {
		h := map[int]Hand{top.Index: Right, bottom.Index: Left}
		if ht, ok := byName(top); ok && ht == Left {
			if hb, ok := byName(bottom); ok && hb == Right {
				h[top.Index], h[bottom.Index] = Left, Right
			}
		}
		return h
	}

	sorted := slices.Clone(staves)
	slices.SortFunc(sorted, func(a, b score.Named) int { return a.Index - b.Index })
	groups := map[string][]score.Named{}
	var order []string
	for _, st := range sorted {
		if strings.HasPrefix(st.Group, "PianoStaff") || strings.HasPrefix(st.Group, "GrandStaff") {
			if groups[st.Group] == nil {
				order = append(order, st.Group)
			}
			groups[st.Group] = append(groups[st.Group], st)
		}
	}
	for _, g := range order {
		if len(groups[g]) == 2 {
			return pair(groups[g][0], groups[g][1])
		}
	}

	h := make(map[int]Hand)
	var named [2]bool
	for _, st := range sorted {
		if hand, ok := byName(st); ok {
			h[st.Index] = hand
			named[hand] = true
		}
	}
	// Names alone decide if they name both hands, or name the only staff:
	// "melody" and "bass" are the first two staves.
	if named[Right] && named[Left] || len(sorted) == 1 && len(h) == 1 {
		return h
	}
	if len(sorted) >= 2 {
		return pair(sorted[0], sorted[1])
	}
	for _, st := range sorted {
		h[st.Index] = Right
	}
	return h
}

func beatOf(ts score.TimeSignature) float64 {
	if ts.Numerator == nil || ts.Denominator == nil {
		return 0.25
	}
	num, den := *ts.Numerator, *ts.Denominator
	if den >= 8 && num > 3 && num%3 == 0 { // 6/8, 9/8, 12/8: dotted beats
		return 3 / float64(den)
	}
	return 1 / float64(den)
}

func (e *Engine) choosePlayerNotes() {
	for gi := range e.groups {
		g := &e.groups[gi]
		g.mine = g.mine[:0]
		if e.set.Mode == Listen {
			continue
		}
		for i, n := range g.notes {
			// Notes you can't reach, or can't see, are played for you.
			reachable := !n.hidden && (e.set.KeyLow == 0 || (n.pitch >= e.set.KeyLow && n.pitch <= e.set.KeyHigh))
			if hand, ok := e.staffHand[n.staff]; ok && reachable && (e.set.Hand == Both || hand == e.set.Hand) {
				g.mine = append(g.mine, i)
			}
		}
	}
}

// findPlayable sees which hands have notes on your keyboard, or in listen
// mode, which have notes at all. Both needs both: with only one hand's
// notes it is the same as that hand.
func (e *Engine) findPlayable() {
	e.playable = [3]bool{}
	for _, g := range e.groups {
		for _, n := range g.notes {
			hand, ok := e.staffHand[n.staff]
			if ok && !n.hidden && (e.set.Mode == Listen || e.set.KeyLow == 0 || n.pitch >= e.set.KeyLow && n.pitch <= e.set.KeyHigh) {
				e.playable[hand] = true
			}
		}
	}
	e.playable[Both] = e.playable[Right] && e.playable[Left]
}

// Playable reports whether choosing this hand makes sense: the score has
// notes for it on your keyboard, or in listen mode, notes for it at all.
func (e *Engine) Playable(h Hand) bool { return h >= Right && h <= Both && e.playable[h] }

func (e *Engine) meterAt(pos float64) meter {
	m := meter{0, 1, 0.25}
	for _, mt := range e.meters {
		if mt.pos > pos+epsilon {
			break
		}
		m = mt
	}
	return m
}

func (e *Engine) wholesPerMinute(pos float64) float64 {
	w := defaultWholesPerMinute
	for i, t := range e.tempos {
		if i > 0 && t.pos > pos+epsilon {
			break
		}
		w = t.wholesPerMinute
	}
	return w
}

// buildClicks puts a click on every beat, accented on the first of each bar.
func (e *Engine) buildClicks(bars []score.Bar) []click {
	var downbeats []float64
	for _, b := range bars {
		d := b.At.Main.Float64() - b.Position.Float64()
		if len(downbeats) == 0 || d > downbeats[len(downbeats)-1]+epsilon {
			downbeats = append(downbeats, d)
		}
	}
	if len(downbeats) == 0 {
		downbeats = []float64{0}
	}
	var clicks []click
	m := e.meterAt(downbeats[0])
	for t := downbeats[0] - m.beat; t > -epsilon; t -= m.beat { // pickup bar
		clicks = append(clicks, click{t, false})
	}
	for i, d := range downbeats {
		next := e.end
		if i+1 < len(downbeats) {
			next = math.Min(next, downbeats[i+1])
		}
		m := e.meterAt(d)
		for t := d; t < next-epsilon; t += m.beat {
			clicks = append(clicks, click{t, t == d})
		}
	}
	sort.Slice(clicks, func(i, j int) bool { return clicks[i].pos < clicks[j].pos })
	return clicks
}

// placeLoop turns the loop's bar numbers into positions.
func (e *Engine) placeLoop() {
	from, to := e.set.LoopFrom, e.set.LoopTo
	first, last := e.bars[0].Number, e.bars[len(e.bars)-1].Number
	from = min(max(from, first), last)
	to = min(max(to, from), last)
	e.set.LoopFrom, e.set.LoopTo = from, to
	e.loopFrom, e.loopTo = e.end, e.end
	for i, b := range e.bars {
		if b.Number == from {
			e.loopFrom = e.startOf(b.Pos)
		}
		if b.Number == to && i+1 < len(e.bars) {
			e.loopTo = e.startOf(e.bars[i+1].Pos)
		}
	}
}

func (e *Engine) looping() bool { return e.set.Loop && e.loopTo > e.loopFrom }

// span is a stretch of music played straight through; see score.Span.
type span struct {
	from, to float64
	pass     int
}

// followingRepeats says whether the music goes back at repeats: unless
// they are ignored, or a loop is on, which plays its bars as written.
func (e *Engine) followingRepeats() bool {
	return !e.set.IgnoreRepeats && len(e.spans) > 1 && !e.looping()
}

// repeatAhead says whether the music jumps at the end of this span: to
// the next, or to stop at Fine. The last span needs no jump otherwise.
func (e *Engine) repeatAhead() bool {
	if !e.followingRepeats() || e.si >= len(e.spans) {
		return false
	}
	return e.si+1 < len(e.spans) || e.spans[e.si].to < e.end-epsilon
}

// spanAt is the first span that plays pos.
func (e *Engine) spanAt(pos float64) int {
	for i, sp := range e.spans {
		if pos >= sp.from-epsilon && pos < sp.to-epsilon {
			return i
		}
	}
	return 0
}

func (e *Engine) Settings() Settings { return e.set }

// Bars lists where each bar starts, for drawing a timeline.
func (e *Engine) Bars() []Bar { return e.bars }

// End is where the music ends.
func (e *Engine) End() float64 { return e.end }

// Configure stops playback and applies new settings.
func (e *Engine) Configure(set Settings) []Out {
	out := e.Stop()
	if set.Tempo <= 0 {
		set.Tempo = 1
	}
	e.set = set
	e.choosePlayerNotes()
	e.findPlayable()
	e.placeLoop()
	return out
}

// SetTempo changes the tempo factor, also while playing.
func (e *Engine) SetTempo(now time.Duration, tempo float64) []Out {
	out := e.Advance(now)
	e.set.Tempo = math.Min(2, math.Max(0.1, tempo))
	return out
}

func (e *Engine) SetMetronome(on bool) { e.set.Metronome = on }

// SetClick changes the metronome's sound, also while playing; see
// Settings.
func (e *Engine) SetClick(accent, beat int, volume float64) {
	e.set.ClickAccent, e.set.ClickBeat, e.set.ClickVolume = accent, beat, volume
}

// ClickChannel is the MIDI channel of the metronome and count-in: General
// MIDI's percussion (channel 10, counted from 0).
const ClickChannel = 9

// ClickVelocity is how hard a click is struck, for the first beat of a
// bar or another, at a volume (0 counts as 1).
func ClickVelocity(accent bool, volume float64) int {
	v := 90.0
	if accent {
		v = 120
	}
	if volume > 0 {
		v *= volume
	}
	return int(math.Round(math.Min(127, math.Max(1, v))))
}
func (e *Engine) SetCountIn(bars int)  { e.set.CountIn = max(bars, 0) }
func (e *Engine) SetAnyOctave(on bool) { e.set.AnyOctave = on }
func (e *Engine) SetGuide(on bool)     { e.set.Guide = on }

// SetScorePedals starts or stops playing the score's pedal marks in wait
// and play mode, also while playing: if that changes whose pedal counts,
// the pedals down now come up, and the other's go down.
func (e *Engine) SetScorePedals(on bool) []Out {
	was := e.youPedal()
	e.set.ScorePedals = on
	if e.youPedal() == was {
		return nil
	}
	return append(e.liftPedals(), e.pressPedals()...)
}

// SetIgnoreRepeats plays straight through or follows the repeats, also
// while playing; switched on midway, the first time through counts.
func (e *Engine) SetIgnoreRepeats(on bool) {
	e.set.IgnoreRepeats = on
	e.si = e.spanAt(e.pos)
}

// SetLoop changes the loop, also while playing: if the music is outside
// the new loop it jumps to its start.
func (e *Engine) SetLoop(now time.Duration, on bool, from, to int) []Out {
	out := e.Advance(now)
	e.set.Loop, e.set.LoopFrom, e.set.LoopTo = on, from, to
	e.placeLoop()
	if !e.looping() {
		e.si = e.spanAt(e.pos)
		return out
	}
	if e.playing && len(e.countIn) == 0 && (e.pos < e.loopFrom-epsilon || e.pos >= e.loopTo-epsilon) {
		out = append(out, e.silence()...)
		out = append(out, e.jump(e.loopFrom)...)
	} else if !e.playing && (e.next >= len(e.groups) || e.groups[e.next].pos < e.loopFrom-epsilon || e.groups[e.next].pos >= e.loopTo-epsilon) {
		e.SeekPos(e.loopFrom)
	}
	return out
}

// LoopSpan is where the loop starts and ends, and whether it is on.
func (e *Engine) LoopSpan() (from, to float64, on bool) { return e.loopFrom, e.loopTo, e.looping() }

// Rewind goes back to the start of the loop, or of the piece, while stopped.
func (e *Engine) Rewind() {
	if e.looping() {
		e.SeekPos(e.loopFrom)
	} else {
		e.Seek(0)
	}
}

// ResetStats starts the score counts again.
func (e *Engine) ResetStats() {
	e.stats = Stats{LastWrong: -1}
	clear(e.missed)
}

// Stats are the score counts so far.
func (e *Engine) Stats() Stats { return e.stats }

// SetStats puts back score counts from before some keys were played,
// when they turn out not to have been music (keys for a command).
func (e *Engine) SetStats(s Stats) { e.stats = s }

// Next is the group playback starts from, or reaches next.
func (e *Engine) Next() int { return e.next }

// Seek moves to group i. It only works while stopped.
func (e *Engine) Seek(i int) {
	if e.playing || len(e.groups) == 0 {
		return
	}
	i = min(max(i, 0), len(e.groups)-1)
	e.next = i
	e.pos = e.groups[i].pos
	e.finished = false
	clear(e.hit)
	clear(e.missed)
	e.due = nil
}

// SeekPos moves to the first group at or after pos, while stopped.
func (e *Engine) SeekPos(pos float64) {
	pos = e.startOf(pos)
	e.Seek(sort.Search(len(e.groups), func(i int) bool { return e.groups[i].pos >= pos-epsilon }))
}

// fitGraceNotes keeps grace notes just before the note they lead to,
// taking their time from the note before; a run too long for that is
// squeezed into the second half of its time, so that the music never runs
// backwards.
func (e *Engine) fitGraceNotes() {
	for i := 0; i < len(e.groups); {
		m := e.groups[i].main
		if e.groups[i].pos >= m-epsilon {
			i++
			continue
		}
		j := i // groups i to j-1 are the grace notes before m, earliest first
		for j < len(e.groups) && math.Abs(e.groups[j].main-m) < epsilon && e.groups[j].pos < m-epsilon {
			j++
		}
		before := m - 0.125
		if i > 0 {
			before = e.groups[i-1].pos
		}
		if lead, room := m-e.groups[i].pos, (m-before)/2; lead > room {
			for k := i; k < j; k++ {
				e.groups[k].pos = m - (m-e.groups[k].pos)*room/lead
			}
		}
		i = j
	}
}

// startOf is where the music at main time x begins: at x, or just before
// it for grace notes leading to it, which go with the note they lead to.
// Jumps, loops and bars start there.
func (e *Engine) startOf(x float64) float64 {
	i := sort.Search(len(e.groups), func(i int) bool { return e.groups[i].main >= x-epsilon })
	if i < len(e.groups) && math.Abs(e.groups[i].main-x) < epsilon {
		return math.Min(x, e.groups[i].pos)
	}
	return x
}

// Start plays from the current group, after a bar of count-in if enabled.
// It returns the pedals already down at that point.
func (e *Engine) Start(now time.Duration) []Out {
	if e.playing || len(e.groups) == 0 {
		return nil
	}
	if e.next >= len(e.groups) {
		e.Seek(0)
	}
	if e.looping() && (e.groups[e.next].pos < e.loopFrom-epsilon || e.groups[e.next].pos >= e.loopTo-epsilon) {
		e.SeekPos(e.loopFrom)
	}
	// Starting from the first notes of a bar, start at its bar line, so
	// that the count-in leads to the downbeat even after a rest.
	start := e.groups[e.next].pos
	bar := e.bars[0].Pos
	for _, b := range e.bars {
		if b.Pos > start+epsilon {
			break
		}
		bar = b.Pos
	}
	if sort.Search(len(e.groups), func(i int) bool { return e.groups[i].pos >= bar-epsilon }) == e.next {
		start = bar
	}
	e.playing, e.waiting, e.finished = true, false, false
	e.last = now
	e.pos = start
	e.si = e.spanAt(start)
	e.nextClick = sort.Search(len(e.clicks), func(i int) bool { return e.clicks[i].pos >= start-epsilon })
	e.countIn = nil
	e.countInEnd = start
	if e.set.CountIn > 0 {
		m := e.meterAt(start)
		perBar := int(math.Round(m.length / m.beat))
		length := float64(e.set.CountIn) * m.length
		for k := 0; k < perBar*e.set.CountIn; k++ {
			e.countIn = append(e.countIn, click{start - length + float64(k)*m.beat, k%perBar == 0})
		}
		e.pos = start - length
		e.countInBeat = m.beat
	}
	return e.placePedals(start)
}

// Stop stops playback and silences everything the program was playing.
func (e *Engine) Stop() []Out {
	out := e.silence()
	e.countIn = nil
	e.due = nil
	e.playing, e.waiting = false, false
	if e.next < len(e.groups) {
		e.pos = e.groups[e.next].pos
	}
	return out
}

// silence releases every note and pedal the program is holding. The
// score's pedals are up too, until the music goes on.
func (e *Engine) silence() []Out {
	var out []Out
	for s := range e.sounding {
		out = append(out, Out{Port: s.port, Channel: s.channel, Pitch: s.pitch})
	}
	clear(e.sounding)
	e.offs = nil
	out = append(out, e.liftPedals()...)
	clear(e.scorePedal)
	return out
}

// jump moves the music to pos, for the loop or a repeat. Your notes start
// afresh, but those just due may still be played, a little late. It
// returns the pedals already down at pos.
func (e *Engine) jump(pos float64) []Out {
	e.next = sort.Search(len(e.groups), func(i int) bool { return e.groups[i].pos >= pos-epsilon })
	e.nextClick = sort.Search(len(e.clicks), func(i int) bool { return e.clicks[i].pos >= pos-epsilon })
	e.pos = pos
	e.countInEnd = min(e.countInEnd, pos)
	e.waiting = false
	clear(e.hit)
	clear(e.missed)
	return e.placePedals(pos)
}

// countingIn says whether the count-in is on: the music hasn't started.
func (e *Engine) countingIn() bool { return e.playing && e.pos < e.countInEnd-epsilon }

func (e *Engine) speed() float64 { // whole notes per second
	pos := e.pos
	if len(e.countIn) > 0 {
		pos = e.countInEnd
	}
	return e.wholesPerMinute(math.Max(pos, 0)) * e.set.Tempo / 60
}

type eventKind int

const (
	none eventKind = iota
	evOff
	evLoop
	evRepeat // the end of a span: on to the next
	evCountIn
	evClick
	evGroup
	evPedal
)

// nextEvent finds the earliest pending event. At equal positions notes
// are released first; then the loop jumps back (so the bar after the loop
// is not played); then pedals come up; then clicks; then new notes; then
// pedals go down, so that a pedal change catches the new notes.
func (e *Engine) nextEvent() (eventKind, float64) {
	kind, at := none, math.Inf(1)
	consider := func(k eventKind, pos float64) {
		if pos < at-epsilon {
			kind, at = k, pos
		}
	}
	pedalUp := e.nextPedal < len(e.pedals) && !e.pedals[e.nextPedal].down && len(e.countIn) == 0
	pedalDown := e.nextPedal < len(e.pedals) && e.pedals[e.nextPedal].down && len(e.countIn) == 0
	if len(e.offs) > 0 {
		consider(evOff, e.offs[0].pos)
	}
	if e.looping() && len(e.countIn) == 0 {
		consider(evLoop, e.loopTo)
	} else if e.repeatAhead() && len(e.countIn) == 0 {
		consider(evRepeat, e.spans[e.si].to)
	}
	if pedalUp {
		consider(evPedal, e.pedals[e.nextPedal].pos)
	}
	if len(e.countIn) > 0 {
		consider(evCountIn, e.countIn[0].pos)
	}
	if e.nextClick < len(e.clicks) {
		consider(evClick, e.clicks[e.nextClick].pos)
	}
	if e.next < len(e.groups) {
		consider(evGroup, e.groups[e.next].pos)
	}
	if pedalDown {
		consider(evPedal, e.pedals[e.nextPedal].pos)
	}
	return kind, at
}

// Advance moves the music forward to time now and returns what to send.
func (e *Engine) Advance(now time.Duration) []Out {
	dt := (now - e.last).Seconds()
	e.last = now
	e.expireDue(now)
	if !e.playing || e.waiting {
		return nil
	}
	speed := e.speed()
	target := e.pos + math.Max(dt, 0)*speed
	// wallAt is when position pos was reached, between the last call and now.
	wallAt := func(pos float64) time.Duration {
		return now - time.Duration((target-pos)/speed*float64(time.Second))
	}
	var out []Out
	for {
		kind, at := e.nextEvent()
		if kind == none || at > target+epsilon {
			break
		}
		e.pos = math.Max(e.pos, at)
		switch kind {
		case evOff:
			o := e.offs[0]
			e.offs = e.offs[1:]
			if e.sounding[o.s] == o.id {
				delete(e.sounding, o.s)
				out = append(out, Out{Port: o.s.port, Channel: o.s.channel, Pitch: o.s.pitch})
			}
		case evLoop:
			over := target - e.loopTo
			out = append(out, e.silence()...)
			out = append(out, e.jump(e.loopFrom)...)
			target = e.loopFrom + math.Max(over, 0)
		case evRepeat:
			over := target - e.spans[e.si].to
			e.si++
			switch {
			case e.si == len(e.spans): // Fine
				out = append(out, e.silence()...)
				e.next = len(e.groups)
				e.playing, e.finished = false, true
				return out
			case math.Abs(e.spans[e.si].from-e.spans[e.si-1].to) < epsilon:
				// The next span goes on from here (a new pass of a repeat
				// begins): nothing to jump over.
			default:
				out = append(out, e.silence()...)
				out = append(out, e.jump(e.spans[e.si].from)...)
				target = e.spans[e.si].from + math.Max(over, 0)
			}
		case evPedal:
			p := e.pedals[e.nextPedal]
			e.nextPedal++
			out = append(out, e.scorePedalTo(p.controller, p.down)...)
		case evCountIn:
			out = append(out, e.playClick(e.countIn[0])...)
			e.countIn = e.countIn[1:]
		case evClick:
			if e.set.Metronome && len(e.countIn) == 0 {
				out = append(out, e.playClick(e.clicks[e.nextClick])...)
			}
			e.nextClick++
		case evGroup:
			g := &e.groups[e.next]
			if e.set.Mode == Wait && !e.allHit(g) {
				e.waiting = true
				e.waitStart = wallAt(at)
				return out
			}
			if e.set.Mode == Play {
				for _, i := range g.mine {
					if n := g.notes[i]; !e.hit[n.id] {
						e.due = append(e.due, due{n.id, n.pitch, wallAt(at)})
					}
				}
			}
			out = append(out, e.release(e.next)...)
			e.next++
		}
	}
	e.pos = target
	if e.next >= len(e.groups) && len(e.offs) == 0 && !e.looping() && !e.repeatAhead() {
		out = append(out, e.silence()...) // a pedal may still be down
		e.playing, e.finished = false, true
	}
	return out
}

// expireDue counts your notes as missed once they are too late to play.
func (e *Engine) expireDue(now time.Duration) {
	kept := e.due[:0]
	for _, d := range e.due {
		if now-d.at > goodWindow {
			e.missed[d.id] = true
			e.stats.Missed++
			e.stats.Streak = 0
		} else {
			kept = append(kept, d)
		}
	}
	e.due = kept
}

func (e *Engine) playClick(c click) []Out {
	pitch := cmp.Or(e.set.ClickBeat, clickBeat)
	if c.accent {
		pitch = cmp.Or(e.set.ClickAccent, clickAccent)
	}
	velocity := ClickVelocity(c.accent, e.set.ClickVolume)
	s := sounding{ClickOut, ClickChannel, pitch}
	id := "click"
	e.sounding[s] = id
	e.addOff(pendingOff{e.pos + clickLength, s, id})
	return []Out{{Port: ClickOut, On: true, Channel: ClickChannel, Pitch: pitch, Velocity: velocity}}
}

// heard reports whether the program plays a note at all: in listen mode
// only the hand chosen is heard, and any staff that is neither hand's.
func (e *Engine) heard(n note) bool {
	if e.set.Mode != Listen || e.set.Hand == Both {
		return true
	}
	hand, ok := e.staffHand[n.staff]
	return !ok || hand == e.set.Hand
}

// release plays the notes of group gi that are not yours; in play mode
// with the guide on, it plays yours too, more quietly. In listen mode it
// plays the notes of the hand you listen to.
func (e *Engine) release(gi int) []Out {
	g := &e.groups[gi]
	var out []Out
	mine := make(map[int]bool, len(g.mine))
	for _, i := range g.mine {
		mine[i] = true
	}
	for i, n := range g.notes {
		if !e.heard(n) {
			continue
		}
		velocity := scoreVelocity
		if mine[i] {
			if e.set.Mode != Play || !e.set.Guide {
				continue
			}
			velocity = guideVelocity
		}
		s := sounding{ScoreOut, channelOf(n.staff), n.pitch}
		if _, on := e.sounding[s]; on { // same key still down: strike it again
			out = append(out, Out{Port: ScoreOut, Channel: s.channel, Pitch: s.pitch})
		}
		e.sounding[s] = n.id
		e.addOff(pendingOff{n.end, s, n.id})
		out = append(out, Out{Port: ScoreOut, On: true, Channel: s.channel, Pitch: n.pitch, Velocity: velocity})
	}
	if gi >= 2 { // forget hits that are no longer shown
		for _, n := range e.groups[gi-2].notes {
			delete(e.hit, n.id)
		}
	}
	return out
}

// channelOf gives each staff its own MIDI channel, skipping percussion.
func channelOf(staff int) int {
	if staff >= ClickChannel {
		staff++
	}
	return min(staff, 15)
}

func (e *Engine) addOff(o pendingOff) {
	i := sort.Search(len(e.offs), func(i int) bool { return e.offs[i].pos > o.pos })
	e.offs = append(e.offs, pendingOff{})
	copy(e.offs[i+1:], e.offs[i:])
	e.offs[i] = o
}

func (e *Engine) allHit(g *group) bool {
	for _, i := range g.mine {
		if !e.hit[g.notes[i].id] {
			return false
		}
	}
	return true
}

func (e *Engine) untilPos(pos float64) time.Duration {
	return time.Duration((pos - e.pos) / e.speed() * float64(time.Second))
}

// upcoming is the group whose notes you should play now: the one being
// waited for, or the next one with your notes if it is close enough.
func (e *Engine) upcoming() int {
	if e.waiting {
		return e.next
	}
	for k := e.next; k < len(e.groups); k++ {
		if e.untilPos(e.groups[k].pos) > goodWindow {
			return -1
		}
		if len(e.groups[k].mine) > 0 {
			return k
		}
	}
	return -1
}

func (e *Engine) matches(want, got int, src Source) bool {
	if src == ComputerKeys && e.set.AnyOctave {
		return (want-got)%12 == 0
	}
	return want == got
}

// NoteOn handles a key you pressed and says how it was judged.
func (e *Engine) NoteOn(now time.Duration, pitch int, src Source) ([]Out, Result) {
	if !e.playing || e.set.Mode == Listen {
		return nil, Ignored
	}
	out := e.Advance(now)

	// A note of yours that is already due (play mode), played late.
	for i, d := range e.due {
		if e.matches(d.pitch, pitch, src) {
			e.due = append(e.due[:i], e.due[i+1:]...)
			e.hit[d.id] = true
			e.lastHand = e.handOf(d.id)
			return out, e.judge(now - d.at)
		}
	}

	// The notes being waited for, or coming up soon.
	k := e.upcoming()
	if k < 0 {
		if e.countingIn() {
			return out, Ignored // finding the first note
		}
		return out, e.wrong(pitch)
	}
	g := &e.groups[k]
	var lateness time.Duration
	if e.waiting && k == e.next {
		lateness = now - e.waitStart
	} else {
		lateness = -e.untilPos(g.pos)
	}
	result := Ignored
	for _, i := range g.mine {
		n := g.notes[i]
		if !e.matches(n.pitch, pitch, src) || e.hit[n.id] {
			continue
		}
		e.hit[n.id] = true
		e.lastHand = e.handOf(n.id)
		if result == Ignored {
			result = e.judge(lateness)
		}
	}
	if result == Ignored {
		for _, i := range g.mine {
			if e.matches(g.notes[i].pitch, pitch, src) {
				return out, Ignored // played again: neither right nor wrong
			}
		}
		return out, e.wrong(pitch)
	}
	if e.waiting && k == e.next && e.allHit(g) {
		e.waiting = false
		e.last = now
		out = append(out, e.Advance(now)...)
	}
	return out, result
}

// judge scores a right note by how far off time it was. In wait mode that
// is how long the music had to wait for it.
func (e *Engine) judge(off time.Duration) Result {
	r := Good
	if off.Abs() <= perfectWindow {
		r = Perfect
		e.stats.Perfect++
	} else {
		e.stats.Good++
	}
	e.stats.Streak++
	e.stats.BestStreak = max(e.stats.BestStreak, e.stats.Streak)
	return r
}

func (e *Engine) wrong(pitch int) Result {
	e.stats.Wrong++
	e.stats.Streak = 0
	e.stats.LastWrong = pitch
	return Wrong
}

// UntilNext is how long until Advance has something to do, or a negative
// duration when it is waiting for you or stopped.
func (e *Engine) UntilNext() time.Duration {
	if !e.playing || e.waiting {
		if len(e.due) > 0 {
			return goodWindow
		}
		return -1
	}
	kind, at := e.nextEvent()
	if kind == none {
		return -1
	}
	return max(e.untilPos(at), 0)
}

// View is what the screen needs to show.
type View struct {
	Playing, Waiting, Finished, CountingIn bool
	Pos                                    float64
	Group                                  int      // last group at or before Pos
	Bar                                    int      // bar number at Pos
	Expected                               []string // your notes to play now
	ExpectedPitches                        []int
	ExpectedHands                          []Hand   // which hand plays each expected pitch
	Hit                                    []string // your notes just played
	Missed                                 []string // your notes missed in this pass
	Sounding                               []string // notes the program is playing
	SoundingPitches                        []int
	SoundingHands                          []Hand
	Stats                                  Stats
	QuarterBPM                             float64
	BeatsLeft                              int // during the count-in: 4, 3, 2, 1
	// Pass is the time through the repeat the music is in, from 1, while
	// playing with repeats followed; otherwise 0.
	Pass int
	// The score marks the sustain pedal, and it is down here (whether
	// played or, if you pedal, not).
	PedalMarks, ScorePedal bool
	// YouPedal: your sustain pedal counts, not the score's.
	YouPedal bool
}

func (e *Engine) View() View {
	v := View{
		Playing:    e.playing,
		Waiting:    e.waiting,
		Finished:   e.finished,
		CountingIn: e.countingIn(),
		Pos:        e.pos,
		Stats:      e.stats,
		QuarterBPM: e.speed() * 60 * 4,
		PedalMarks: e.sustained,
		ScorePedal: e.scorePedal[SustainPedal],
		YouPedal:   e.youPedal(),
	}
	if e.playing && e.followingRepeats() && e.si < len(e.spans) {
		v.Pass = e.spans[e.si].pass
	}
	if v.CountingIn {
		// The music hasn't started: show where it will.
		v.Pos = e.countInEnd
		v.BeatsLeft = int(math.Ceil((e.countInEnd-e.pos)/e.countInBeat - 1e-6))
	}
	v.Group = sort.Search(len(e.groups), func(i int) bool { return e.groups[i].pos > v.Pos+epsilon }) - 1
	v.Bar = e.bars[0].Number
	for _, b := range e.bars {
		if b.Pos > v.Pos+epsilon {
			break
		}
		v.Bar = b.Number
	}
	k := e.next
	if e.playing {
		k = e.upcoming()
	}
	if k >= 0 && k < len(e.groups) {
		for _, i := range e.groups[k].mine {
			if n := e.groups[k].notes[i]; !e.hit[n.id] {
				v.Expected = append(v.Expected, n.id)
				v.ExpectedPitches = append(v.ExpectedPitches, n.pitch)
				v.ExpectedHands = append(v.ExpectedHands, e.handOf(n.id))
			}
		}
	}
	for _, d := range e.due {
		v.Expected = append(v.Expected, d.id)
		v.ExpectedPitches = append(v.ExpectedPitches, d.pitch)
		v.ExpectedHands = append(v.ExpectedHands, e.handOf(d.id))
	}
	for id, hit := range e.hit {
		if hit {
			v.Hit = append(v.Hit, id)
		}
	}
	for id := range e.missed {
		v.Missed = append(v.Missed, id)
	}
	for s, id := range e.sounding {
		if s.port != ScoreOut {
			continue
		}
		hand := e.handOf(id)
		v.Sounding = append(v.Sounding, id)
		v.SoundingPitches = append(v.SoundingPitches, s.pitch)
		v.SoundingHands = append(v.SoundingHands, hand)
		// A note held on by a tie lights up as the music reaches it.
		for _, t := range e.tiedOn[id] {
			if t.pos <= v.Pos+epsilon {
				v.Sounding = append(v.Sounding, t.id)
				v.SoundingPitches = append(v.SoundingPitches, s.pitch)
				v.SoundingHands = append(v.SoundingHands, hand)
			}
		}
	}
	return v
}

// Groups returns where each group is and the ids of its notes, for drawing.
func (e *Engine) Groups() (pos []float64, ids [][]string) {
	for _, g := range e.groups {
		pos = append(pos, g.pos)
		var gi []string
		for _, n := range g.notes {
			gi = append(gi, n.id)
		}
		ids = append(ids, gi)
	}
	return pos, ids
}

// handOf is the hand that plays a note, or OtherStaff.
func (e *Engine) handOf(id string) Hand {
	if h, ok := e.staffHand[e.noteStaff[id]]; ok {
		return h
	}
	return OtherStaff
}

// LastHand is the hand of the note that the last right key matched.
func (e *Engine) LastHand() Hand { return e.lastHand }
