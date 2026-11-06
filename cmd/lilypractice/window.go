package main

import (
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/midikeys"
	"lilypractice/practice"
	"lilypractice/rsvg"
	"lilypractice/score"
	"lilypractice/turn"
	"lilypractice/view"
)

// The window: a header bar (title, stats, settings); on the left the
// scores of the folder; then, top to bottom, the score with its scroll
// bars, a divider you can drag, the piano, a timeline of the bars, and the
// transport bar (play, mode, hands, tempo, loop, metronome). While no
// score is ready, a message takes the place of all but the scores.
type window struct {
	s     *session
	pages *view.Pages

	file     string    // the score file practised, or ""
	fileTime time.Time // when it was changed, as loaded
	pending  string    // the one being loaded, or ""
	scores   []string  // the notes of each score in the file
	pagesOf  []int     // the page each score begins on, or -1
	score    int       // the one being practised
	load     loader
	side     sidebar
	msg      message

	v practice.View // the engine's, taken once a frame for all the drawing

	// These don't change once the engine is built.
	groupPos    []float64
	groupIDs    [][]string
	groupSystem []int // the line of music each group is on
	bars        []practice.Bar
	end         float64

	win        *gtk.ApplicationWindow
	titleLabel *gtk.Label
	header     *gtk.HeaderBar
	scorePick  *gtk.DropDown    // which score of the file, if it has several
	split      *gtk.Paned       // between the scores and the rest
	stack      *gtk.Stack       // the score, or a message
	area       *gtk.DrawingArea // the score
	piano      *gtk.DrawingArea
	timeline   *gtk.DrawingArea
	paned      *gtk.Paned // between the score and the piano
	vadj       *gtk.Adjustment
	hadj       *gtk.Adjustment
	hbar       *gtk.Scrollbar
	popovers   []*gtk.Popover
	c          controls

	zoom             float64
	originX          float64 // where the layout's left edge is drawn, in device pixels
	scrollX, scrollY float64 // device pixels
	targetX, targetY float64 // where the view is gliding to
	manualAt         time.Time
	syncingBars      bool // the scroll bars are being set to match the view
	lastFrame        time.Time
	dirty            bool // redraw on the next frame
	placeView        bool // follow the music as soon as the score has a size
	recompile        bool // run LilyPond on scores even if unchanged

	rotate   string        // the window is turned to the "left" or "right", or ""
	tip      gtk.Widgetter // the turned tooltip, made when first wanted
	tipLabel *gtk.Label    // its text

	keyBase  int          // MIDI note of the lowest computer key, z
	keysDown map[uint]int // keycode -> pitch, to ignore key repeat

	wrongSeen  int // wrong-note count already shown
	wrongAt    time.Time
	wasPlaying bool

	prefs        prefs
	prefsChanged bool
	savedAt      time.Time
	pianoPlaced  bool
	drag         loopDrag
	loopSet      bool // a loop was placed in this score
	heldWas      map[int]heldKey
	barAreaCache map[int][]rsvg.Rect
	systemBoxes  map[int]*rsvg.Rect // nil: not on the page
	commandKeys  commandKeys
	commandPath  string             // how far into a command the MIDI keyboard is; "" if not
	commandNext  []midikeys.Binding // what its keys do now
	countInWas   int                // bars of count-in before it was turned off

	// The mouse.
	pan                panState
	presses            int // clicks in a row, for double-clicks
	pointerX, pointerY float64
	hoverGroup         int    // the note group under the pointer, or -1
	hoverOther         string // or the note of another score of the file under it
	pianoHover         int    // the piano key under the pointer, or -1
	mouseKey           int    // the piano key held down with the mouse, or -1
}

// launch is what to show first: the scores of dir, and file if not "".
type launch struct {
	dir, file string
	score     int // which score of the file, from 0
	recompile bool
	rotate    string // "left" or "right" to turn the window, or ""
}

func newWindow(app *gtk.Application, s *session, p prefs, start launch) {
	applyStyle(p.TextSize)
	w := &window{s: s, prefs: p, recompile: start.recompile, rotate: start.rotate,
		zoom: p.Zoom, keyBase: p.ComputerKeys, keysDown: make(map[uint]int),
		hoverGroup: -1, pianoHover: -1, mouseKey: -1}
	w.setPages(emptyPages())
	w.takeEngine()

	w.area = gtk.NewDrawingArea()
	w.area.SetHExpand(true)
	w.area.SetVExpand(true)
	w.area.AddCSSClass("score-area")
	w.area.SetDrawFunc(w.draw)
	w.addScoreGestures()
	w.addScoreMouse()

	// Scroll bars, kept in step with the view.
	w.vadj = gtk.NewAdjustment(0, 0, 1, 40, 400, 1)
	w.hadj = gtk.NewAdjustment(0, 0, 1, 40, 400, 1)
	for _, adj := range []*gtk.Adjustment{w.vadj, w.hadj} {
		adj.ConnectValueChanged(func() {
			if !w.syncingBars {
				w.manualAt = time.Now()
				w.scrollTo(w.hadj.Value(), w.vadj.Value())
			}
		})
	}
	w.hbar = gtk.NewScrollbar(gtk.OrientationHorizontal, w.hadj)
	scoreRow := gtk.NewBox(gtk.OrientationHorizontal, 0)
	scoreRow.Append(w.area)
	scoreRow.Append(gtk.NewScrollbar(gtk.OrientationVertical, w.vadj))
	score := gtk.NewBox(gtk.OrientationVertical, 0)
	score.Append(scoreRow)
	score.Append(w.hbar)

	w.piano = gtk.NewDrawingArea()
	w.piano.SetContentHeight(60) // the least; the divider sets the height
	w.piano.SetDrawFunc(w.drawPiano)
	w.addPianoMouse()

	// The divider between score and piano: drag it to size the piano.
	w.paned = gtk.NewPaned(gtk.OrientationVertical)
	w.paned.SetStartChild(score)
	w.paned.SetEndChild(w.piano)
	w.paned.SetResizeStartChild(true)
	w.paned.SetResizeEndChild(false)
	w.paned.SetShrinkStartChild(false)
	w.paned.SetShrinkEndChild(false)
	w.paned.SetWideHandle(true)
	w.paned.SetVExpand(true)
	w.paned.NotifyProperty("position", func() {
		if h := w.paned.Height() - w.paned.Position(); w.pianoPlaced && h != w.prefs.PianoHeight {
			w.prefs.PianoHeight = h
			w.prefsChanged = true
		}
	})

	w.timeline = gtk.NewDrawingArea()
	w.timeline.SetContentHeight(timelineHeight(w.textScale()))
	w.timeline.SetDrawFunc(w.drawTimeline)
	w.addTimelineGestures()
	w.addTimelineMouse()

	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.Append(w.paned)
	box.Append(w.timeline)
	box.Append(w.buildTransport())

	w.stack = gtk.NewStack()
	w.stack.AddNamed(box, "practice")
	w.stack.AddNamed(w.buildMessage(), "message")

	// The divider between the scores and the rest: drag it to size the list.
	w.split = gtk.NewPaned(gtk.OrientationHorizontal)
	w.split.SetStartChild(w.buildSidebar(start.dir))
	w.split.SetEndChild(w.stack)
	w.split.SetResizeStartChild(false)
	w.split.SetShrinkStartChild(false)
	w.split.SetShrinkEndChild(false)
	w.split.SetPosition(p.SidebarWidth)
	w.split.NotifyProperty("position", func() {
		// Not at a limit, where larger text pushes the list (its least
		// width grows, and so does the rest's): that isn't the width you
		// chose.
		least, _ := w.split.ObjectProperty("min-position").(int)
		most, _ := w.split.ObjectProperty("max-position").(int)
		if pos := w.split.Position(); w.sidebarShown() && pos > least && pos < most && pos != w.prefs.SidebarWidth {
			w.prefs.SidebarWidth = pos
			w.prefsChanged = true
		}
	})

	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture) // before buttons and focus moves
	keys.ConnectKeyPressed(w.keyPressed)
	keys.ConnectKeyReleased(w.keyReleased)

	w.win = gtk.NewApplicationWindow(app)
	w.win.SetDefaultSize(1280, 900)
	if w.turned() {
		// The header turns with the rest, so it is no title bar.
		all := gtk.NewBox(gtk.OrientationVertical, 0)
		all.Append(w.buildHeader())
		all.Append(w.split)
		w.win.SetChild(turn.New(all, w.rotate == "left"))
	} else {
		w.win.SetTitlebar(w.buildHeader())
		w.win.SetChild(w.split)
	}
	w.win.AddController(keys)
	w.win.AddTickCallback(func(gtk.Widgetter, gdk.FrameClocker) bool {
		w.tick()
		return true
	})
	w.win.ConnectDestroy(func() {
		w.stopLoading()
		if w.prefsChanged {
			w.prefs.save()
		}
	})
	w.addSideButtons()
	w.watchCommandKeys()
	w.setTitle(filepath.Base(start.dir))
	if start.file != "" {
		w.open(start.file, start.score)
	} else {
		w.showNothingChosen()
	}
	w.v = s.view()
	w.refresh(w.v, s.settings())
	w.turnAll(w.win)
	w.win.Present()
	w.dirty = true
}

// takeEngine reads what the window keeps of the engine's score.
func (w *window) takeEngine() {
	w.groupPos, w.groupIDs = w.s.eng.Groups()
	w.groupSystem = w.groupSystem[:0]
	for _, ids := range w.groupIDs {
		w.groupSystem = append(w.groupSystem, w.pages.System(ids[0]))
	}
	w.bars = w.s.eng.Bars()
	w.end = w.s.eng.End()
	w.barAreaCache = nil
	w.hoverGroup, w.hoverOther = -1, ""
}

// chooseScore practises another score of the file: a fresh start, with
// the same settings but no loop, and the view at its beginning.
func (w *window) chooseScore(i int) {
	if w.switchScore(i) {
		w.follow(w.s.view(), true)
	}
}

// switchScore practises another score of the file from its start, with the
// same settings but no loop; the view stays where it is. It says whether
// it did.
func (w *window) switchScore(i int) bool {
	if i == w.score || i < 0 || i >= len(w.scores) {
		return false
	}
	sc, err := score.Load(w.scores[i])
	if err != nil {
		fmt.Fprintln(os.Stderr, "lilypractice:", err)
		return false
	}
	w.replaceEngine(practice.New(sc, w.blankSettings()))
	w.score = i
	w.keepPlayableHand()
	if w.scorePick != nil && w.scorePick.Selected() != uint(i) {
		w.c.updating = true // not a choice of yours
		w.scorePick.SetSelected(uint(i))
		w.c.updating = false
	}
	return true
}

// tick runs every frame: it follows the music, glides the view, keeps the
// scroll bars in step and updates the widgets.
func (w *window) tick() {
	now := time.Now()
	dt := math.Min(0.1, now.Sub(w.lastFrame).Seconds())
	w.lastFrame = now

	if !w.pianoPlaced && w.paned.Height() > 0 {
		w.setPianoHeight(w.prefs.PianoHeight)
		w.pianoPlaced = true
		w.placeView = true
	}
	if w.placeView && w.area.Width() > 0 && w.area.Height() > 0 {
		w.follow(w.s.view(), true)
		w.placeView = false
	}
	w.checkFolder()

	// Ticks come before drawing in each frame, so the drawing uses this.
	w.v = w.s.view()
	v := w.v
	if v.Stats.Wrong != w.wrongSeen {
		if v.Stats.Wrong > w.wrongSeen { // not when a wrong key is taken back
			w.wrongAt = now
		}
		w.wrongSeen = v.Stats.Wrong
		w.dirty = true
	}
	if path, next := w.s.commandMode(); path != w.commandPath {
		w.commandPath, w.commandNext = path, next
		w.dirty = true
	}
	if v.Playing != w.wasPlaying { // one more frame after stopping or finishing
		w.wasPlaying = v.Playing
		w.dirty = true
	}
	if v.Playing {
		w.follow(v, false)
	}
	if w.glide(dt) {
		w.dirty = true
	}
	// Drawn again only when something changed: GTK then redraws the
	// whole window, which costs.
	if v.Playing || w.dirty || time.Since(w.wrongAt) < time.Second {
		w.area.QueueDraw()
		w.timeline.QueueDraw()
		w.piano.QueueDraw()
		w.syncScrollBars()
		w.dirty = false
	}
	if held := w.s.heldKeys(); !maps.Equal(held, w.heldWas) { // keys change at any time
		w.heldWas = held
		w.piano.QueueDraw()
	}
	if d := w.s.detected(); d.done {
		w.setKeyboard(d.low, d.high)
	}
	w.checkCommandKeys()
	if w.prefsChanged && now.Sub(w.savedAt) > time.Second {
		w.prefs.save()
		w.prefsChanged, w.savedAt = false, now
	}
	w.refresh(v, w.s.settings())
}

func (w *window) syncScrollBars() {
	pw, ph := w.pages.Size()
	width, height := float64(w.area.Width()), float64(w.area.Height())
	w.syncingBars = true
	w.vadj.Configure(w.scrollY, 0, math.Max(ph*w.zoom, height), 40, height*0.9, height)
	w.hadj.Configure(w.scrollX, 0, math.Max(pw*w.zoom, width), 40, width*0.9, width)
	w.syncingBars = false
	w.hbar.SetVisible(pw*w.zoom > width+1)
}

// Actions, shared by the buttons and the keyboard.

func (w *window) do(f func(now time.Duration) []practice.Out) {
	w.s.do(f)
	w.dirty = true
}

func (w *window) togglePlay() {
	eng := w.s.eng
	w.do(func(now time.Duration) []practice.Out {
		if eng.View().Playing {
			return eng.Stop()
		}
		return eng.Start(now)
	})
	w.manualAt = time.Time{} // follow the music again
}

// restart goes back to the start, of the loop if there is one, and
// clears the stats. While playing, the music goes on from there.
func (w *window) restart() {
	w.seek(func(eng *practice.Engine) {
		eng.Rewind()
		eng.ResetStats()
	})
	w.follow(w.s.view(), false)
}

func (w *window) stop() {
	w.do(func(time.Duration) []practice.Out { return w.s.eng.Stop() })
}

// stepNote moves n notes (struck together, a chord counts once) back or
// forward.
func (w *window) stepNote(n int) {
	w.seek(func(eng *practice.Engine) { eng.Seek(eng.Next() + n) })
	w.manualAt = time.Time{}
	w.follow(w.s.view(), false)
}

func (w *window) seekPos(pos float64) {
	w.seek(func(eng *practice.Engine) { eng.SeekPos(pos) })
}

// seek moves the music with move; while playing, playback restarts there.
func (w *window) seek(move func(eng *practice.Engine)) {
	eng := w.s.eng
	w.do(func(now time.Duration) []practice.Out {
		playing := eng.View().Playing
		out := eng.Stop()
		move(eng)
		if playing {
			out = append(out, eng.Start(now)...)
		}
		return out
	})
}

func (w *window) configure(change func(*practice.Settings)) {
	eng := w.s.eng
	w.do(func(time.Duration) []practice.Out {
		set := eng.Settings()
		change(&set)
		return eng.Configure(set)
	})
}

// setMode picks the mode. Your keyboard's range matters to the hands in
// wait and play mode only, so the hand may have to change.
func (w *window) setMode(m practice.Mode) {
	w.configure(func(s *practice.Settings) { s.Mode = m })
	w.keepPlayableHand()
}

// setHand picks the hand you practise, if it has notes to play.
func (w *window) setHand(h practice.Hand) {
	if w.s.playable(h) {
		w.configure(func(s *practice.Settings) { s.Hand = h })
	}
}

func (w *window) changeTempo(by float64) {
	eng := w.s.eng
	w.do(func(now time.Duration) []practice.Out { return eng.SetTempo(now, eng.Settings().Tempo+by) })
}

func (w *window) setLoop(on bool, from, to int) {
	w.loopSet = w.loopSet || on
	w.do(func(now time.Duration) []practice.Out { return w.s.eng.SetLoop(now, on, from, to) })
}

func (w *window) setting(set func(on bool)) func(bool) {
	return func(on bool) {
		w.do(func(time.Duration) []practice.Out {
			set(on)
			return nil
		})
	}
}

func (w *window) setMetronome(on bool) { w.setting(w.s.eng.SetMetronome)(on) }

// setFollowRepeats goes back at repeats and takes their endings in turn,
// or plays straight through.
func (w *window) setFollowRepeats(on bool) {
	w.setting(w.s.eng.SetIgnoreRepeats)(!on)
	w.prefs.IgnoreRepeats = !on
	w.prefsChanged = true
}

// setPedalMarks plays the score's pedal marks, or leaves the pedalling to
// you.
func (w *window) setPedalMarks(on bool) {
	w.do(func(time.Duration) []practice.Out { return w.s.eng.SetPedalYourself(!on) })
	w.prefs.PedalYourself = !on
	w.prefsChanged = true
}
func (w *window) setGuide(on bool)     { w.setting(w.s.eng.SetGuide)(on) }
func (w *window) setAnyOctave(on bool) { w.setting(w.s.eng.SetAnyOctave)(on) }

// setCountIn sets the bars of count-in; 0 turns it off.
func (w *window) setCountIn(bars int) {
	bars = min(max(bars, 0), 4)
	w.do(func(time.Duration) []practice.Out {
		w.s.eng.SetCountIn(bars)
		return nil
	})
	w.prefs.CountIn = bars
	w.prefsChanged = true
}

func (w *window) resetStats() {
	w.do(func(time.Duration) []practice.Out {
		w.s.eng.ResetStats()
		return nil
	})
}

func (w *window) setKeyBase(base int) {
	w.keyBase = min(max(base, 12), 96)
	w.prefs.ComputerKeys = w.keyBase
	w.prefsChanged = true
	w.dirty = true
}

func (w *window) setZoom(zoom float64) {
	w.zoom = math.Min(6, math.Max(0.3, zoom))
	w.prefs.Zoom = w.zoom
	w.prefsChanged = true
	w.follow(w.s.view(), true)
	w.dirty = true
}

// setColumns shows one page at a time, or two side by side, fitted to the
// width of the view.
func (w *window) setColumns(columns int) {
	w.prefs.Columns = columns
	w.prefsChanged = true
	w.pages.SetColumns(columns)
	w.barAreaCache, w.systemBoxes = nil, nil
	w.fitWidth()
}

// setTextSize sizes the text of everything but the score and the piano
// (whose letters grow with it), in percent.
func (w *window) setTextSize(size int) {
	size = min(max(size, minTextSize), maxTextSize)
	if size == w.prefs.TextSize { // at a limit: restyling every widget would change nothing
		return
	}
	w.prefs.TextSize = size
	w.prefsChanged = true
	applyStyle(w.prefs.TextSize)
	w.timeline.SetContentHeight(timelineHeight(w.textScale()))
	w.sidebarLeast()
	w.split.SetPosition(w.prefs.SidebarWidth) // back to it, if there's room again
	w.dirty = true
}

// textScale is the text size as a factor, for the text the program draws
// itself.
func (w *window) textScale() float64 { return float64(w.prefs.TextSize) / 100 }

func (w *window) setPianoHeight(height int) {
	height = min(max(height, 60), 400)
	w.paned.SetPosition(w.paned.Height() - height)
	w.prefs.PianoHeight = height
	w.prefsChanged = true
}

func (w *window) setCursor(style, colour string) {
	w.prefs.CursorStyle, w.prefs.CursorColour = style, colour
	w.prefsChanged = true
	w.dirty = true
}

// setKeyboard sets the range of your keyboard; notes outside it are played
// for you.
func (w *window) setKeyboard(low, high int) {
	if high-low < 11 {
		return
	}
	w.prefs.KeyLow, w.prefs.KeyHigh = low, high
	w.prefsChanged = true
	w.configure(func(s *practice.Settings) { s.KeyLow, s.KeyHigh = low, high })
	w.keepPlayableHand()
}

// keepPlayableHand moves to a hand with notes to play, should the one
// chosen have none on your keyboard.
func (w *window) keepPlayableHand() {
	if h := w.s.settings().Hand; !w.s.playable(h) {
		for _, other := range []practice.Hand{practice.Right, practice.Left} {
			if w.s.playable(other) {
				w.configure(func(s *practice.Settings) { s.Hand = other })
				return
			}
		}
	}
}

// The score.

func (w *window) draw(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
	v := w.v
	cr.SetSourceRGB(0.137, 0.137, 0.16) // shows between and around pages
	cr.Paint()

	pageWidth, _ := w.pages.Size()
	w.originX = -w.scrollX
	if free := float64(width) - pageWidth*w.zoom; free > 0 {
		w.originX = free / 2
	}
	cr.Save()
	cr.Translate(w.originX, -w.scrollY)

	// Notes coloured by hand, like the piano; then yours just played and missed.
	highlight := map[string]view.Color{}
	byHand := func(h practice.Hand) view.Color {
		c := handColour(h).shade(0.85)
		return view.Color{R: c.r, G: c.g, B: c.b}
	}
	for i, id := range v.Sounding {
		highlight[id] = byHand(v.SoundingHands[i])
	}
	for _, id := range v.Missed {
		highlight[id] = view.Red
	}
	for _, id := range v.Hit {
		highlight[id] = view.Green
	}
	for i, id := range v.Expected {
		highlight[id] = byHand(v.ExpectedHands[i])
	}
	w.pages.Draw(cr, w.zoom, highlight)
	w.drawLoopOnScore(cr)
	w.drawHover(cr)
	if at, ok := w.cursor(v); ok {
		w.drawCursor(cr, at)
	}
	cr.Restore()

	if v.CountingIn && v.BeatsLeft > 0 {
		w.drawCountdown(cr, v.BeatsLeft, width, height)
	}
	if w.commandPath != "" {
		w.drawCommandMode(cr, width, height)
	}
}

// The keyboard.

func (w *window) keyPressed(keyval, keycode uint, state gdk.ModifierType) bool {
	if keyval == gdk.KEY_Shift_L || keyval == gdk.KEY_Shift_R { // the sustain pedal
		w.s.computerPedal(true)
		return false
	}
	if keyval == gdk.KEY_F9 {
		w.setSidebar(!w.sidebarShown())
		return true
	}
	if keyval == gdk.KEY_Escape || keyval == gdk.KEY_g && state&gdk.ControlMask != 0 {
		if w.closePopovers() || w.s.detectStep() != 0 {
			w.s.cancelDetect()
			return true
		}
		if keyval == gdk.KEY_Escape {
			w.stop()
		}
		return true
	}
	if state&gdk.ControlMask != 0 {
		if !w.loaded() && keyval != gdk.KEY_q && keyval != gdk.KEY_w && keyval != gdk.KEY_o {
			return false
		}
		switch keyval {
		case gdk.KEY_plus, gdk.KEY_equal, gdk.KEY_KP_Add:
			w.setZoom(w.zoom * 1.2)
		case gdk.KEY_minus, gdk.KEY_KP_Subtract:
			w.setZoom(w.zoom / 1.2)
		case gdk.KEY_0:
			w.fitWidth()
		case gdk.KEY_q, gdk.KEY_w:
			w.win.Close()
		case gdk.KEY_o:
			w.chooseFolder()
		case gdk.KEY_Left:
			w.moveBars(-1)
		case gdk.KEY_Right:
			w.moveBars(1)
		case gdk.KEY_1:
			w.setMode(practice.Wait)
		case gdk.KEY_2:
			w.setMode(practice.Play)
		case gdk.KEY_3:
			w.setMode(practice.Listen)
		case gdk.KEY_r:
			w.setHand(practice.Right)
		case gdk.KEY_l:
			w.setHand(practice.Left)
		case gdk.KEY_b:
			w.setHand(practice.Both)
		default:
			return false
		}
		return true
	}
	if offset, ok := pianoKeys[keycode]; ok && state&gdk.AltMask == 0 {
		if _, down := w.keysDown[keycode]; !down { // ignore key repeat
			pitch := w.keyBase + offset
			w.keysDown[keycode] = pitch
			w.s.computerKey(pitch, true)
			w.dirty = true
		}
		return true
	}
	if !w.loaded() { // the computer keys play, but there is nothing to practise
		return false
	}

	set := w.s.settings()
	switch keyval {
	case gdk.KEY_space:
		w.togglePlay()
	case gdk.KEY_Home:
		w.restart()
	case gdk.KEY_Left:
		w.stepNote(-1)
	case gdk.KEY_Right:
		w.stepNote(1)
	case gdk.KEY_Up:
		w.changeTempo(0.05)
	case gdk.KEY_Down:
		w.changeTempo(-0.05)
	case gdk.KEY_Page_Up:
		w.setKeyBase(w.keyBase + 12)
	case gdk.KEY_Page_Down:
		w.setKeyBase(w.keyBase - 12)
	case gdk.KEY_Tab, gdk.KEY_ISO_Left_Tab:
		w.nextHand()
	case gdk.KEY_F2:
		w.setMode((set.Mode + 1) % 3)
	case gdk.KEY_F3:
		w.setMetronome(!set.Metronome)
	case gdk.KEY_F4:
		w.setAnyOctave(!set.AnyOctave)
	case gdk.KEY_F5:
		w.toggleCountIn()
	case gdk.KEY_F6:
		w.toggleLoop()
	case gdk.KEY_F7:
		w.setGuide(!set.Guide)
	case gdk.KEY_F8:
		w.setColumns(3 - max(w.prefs.Columns, 1)) // one page ↔ two pages
	default:
		return false
	}
	w.dirty = true
	return true
}

func (w *window) keyReleased(keyval, keycode uint, _ gdk.ModifierType) {
	if keyval == gdk.KEY_Shift_L || keyval == gdk.KEY_Shift_R {
		w.s.computerPedal(false)
		return
	}
	if pitch, ok := w.keysDown[keycode]; ok {
		delete(w.keysDown, keycode)
		w.s.computerKey(pitch, false)
	}
}

// closePopovers closes any open settings, stats or loop panel; true if
// one was open.
func (w *window) closePopovers() bool {
	closed := false
	for _, p := range w.popovers {
		if p.Visible() {
			p.Popdown()
			closed = true
		}
	}
	return closed
}
