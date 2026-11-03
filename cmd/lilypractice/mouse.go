package main

import (
	"fmt"
	"math"
	"time"

	"github.com/diamondburned/gotk4/pkg/cairo"
	glib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/practice"
)

// The mouse, beyond looping (loop.go):
//
//	score     click a note: start there; double-click: play from there;
//	          right-click: a menu for that note and bar; drag empty space,
//	          or drag with the middle button: move the view; Ctrl+wheel:
//	          zoom around the pointer; Shift+wheel: scroll sideways
//	timeline  double-click: play from that bar; right-click: the bar's menu
//	piano     click or drag over the keys: play them (louder towards the
//	          front of a key); right-click: set the computer keys' octave
//	          or your keyboard's range from that key
//	transport the wheel over the tempo changes it; over the bar number,
//	          moves a bar
//	anywhere  the mouse's back and forward buttons: back or forward a bar

// menuItem is an entry of a pop-up menu; one without run is a separator.
// A choice among several shows a tick when it is the one chosen.
type menuItem struct {
	label  string
	run    func()
	choice bool
	chosen bool
}

// popupMenu shows a menu at x, y in widget; Esc or Ctrl+G closes it.
func (w *window) popupMenu(widget gtk.Widgetter, x, y float64, items []menuItem) {
	p := gtk.NewPopover()
	box := gtk.NewBox(gtk.OrientationVertical, 0)
	box.AddCSSClass("menu")
	for _, it := range items {
		if it.run == nil {
			box.Append(gtk.NewSeparator(gtk.OrientationHorizontal))
			continue
		}
		l := gtk.NewLabel(it.label)
		l.SetXAlign(0)
		b := gtk.NewButton()
		if !it.choice {
			b.SetChild(l)
		} else {
			tick := gtk.NewLabel("")
			if it.chosen {
				tick.SetText("✓")
			}
			tick.SetWidthChars(2)
			tick.SetXAlign(0)
			row := gtk.NewBox(gtk.OrientationHorizontal, 4)
			row.Append(tick)
			row.Append(l)
			b.SetChild(row)
		}
		b.AddCSSClass("flat")
		b.AddCSSClass("menu-item")
		run := it.run
		b.ConnectClicked(func() {
			p.Popdown()
			run()
		})
		box.Append(b)
	}
	p.SetChild(box)
	p.SetHasArrow(false)
	p.SetPosition(gtk.PosBottom)
	p.SetHAlign(gtk.AlignStart)
	r := gdk.NewRectangle(int(x), int(y), 1, 1)
	p.SetPointingTo(&r)
	keys := gtk.NewEventControllerKey()
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		if keyval == gdk.KEY_Escape || keyval == gdk.KEY_g && state&gdk.ControlMask != 0 {
			p.Popdown()
			return true
		}
		return false
	})
	p.AddController(keys)
	p.ConnectClosed(func() { glib.IdleAdd(p.Unparent) })
	p.SetParent(widget)
	p.Popup()
}

// playFrom plays from pos, starting the music if it is stopped.
func (w *window) playFrom(pos float64) {
	eng := w.s.eng
	w.do(func(now time.Duration) []practice.Out {
		out := eng.Stop()
		eng.SeekPos(pos)
		return append(out, eng.Start(now)...)
	})
	w.manualAt = time.Time{}
	w.follow(w.s.view(), false)
}

// barMenu is the menu for a place in the music: at pos, in bar.
func (w *window) barMenu(widget gtk.Widgetter, x, y float64, pos float64, bar int) {
	set := w.s.settings()
	last := w.bars[len(w.bars)-1].Number
	items := []menuItem{
		{label: "Play from here", run: func() { w.playFrom(pos) }},
		{label: "Start here", run: func() { w.seekPos(pos) }},
		{},
		{label: fmt.Sprintf("Loop bar %d", bar), run: func() { w.setLoop(true, bar, bar) }},
	}
	from := w.s.view().Bar
	if set.Loop && set.LoopFrom <= bar {
		from = set.LoopFrom
	}
	if from != bar {
		a, b := min(from, bar), max(from, bar)
		items = append(items, menuItem{label: fmt.Sprintf("Loop bars %d–%d", a, b), run: func() { w.setLoop(true, a, b) }})
	}
	if bar < last {
		to := min(bar+3, last)
		items = append(items, menuItem{label: fmt.Sprintf("Loop bars %d–%d", bar, to), run: func() { w.setLoop(true, bar, to) }})
	}
	if set.Loop {
		items = append(items, menuItem{label: "Loop off", run: func() { w.setLoop(false, set.LoopFrom, set.LoopTo) }})
	}
	pages := "Two pages side by side"
	if w.prefs.Columns == 2 {
		pages = "One page"
	}
	items = append(items, menuItem{},
		menuItem{label: "Fit to width", run: w.fitWidth},
		menuItem{label: pages, run: func() { w.setColumns(3 - max(w.prefs.Columns, 1)) }})
	w.popupMenu(widget, x, y, items)
}

// viewMenu is the menu for a place in the score with no notes.
func (w *window) viewMenu(x, y float64) {
	pages := "Two pages side by side"
	if w.prefs.Columns == 2 {
		pages = "One page"
	}
	w.popupMenu(w.area, x, y, []menuItem{
		{label: "Fit to width", run: w.fitWidth},
		{label: pages, run: func() { w.setColumns(3 - max(w.prefs.Columns, 1)) }},
	})
}

// The score.

// panState is a drag that moves the view.
type panState struct {
	active       bool
	fromX, fromY float64 // the scroll position when it began
}

func (w *window) startPan() {
	w.pan = panState{true, w.scrollX, w.scrollY}
	w.area.SetCursorFromName("grabbing")
}

func (w *window) panBy(dx, dy float64) {
	w.manualAt = time.Now()
	w.scrollTo(w.pan.fromX-dx, w.pan.fromY-dy)
}

func (w *window) endPan() {
	w.pan.active = false
	w.area.SetCursorFromName("grab")
}

func (w *window) addScoreMouse() {
	// Clicks: how many in a row (for double-clicks, read by the drag in
	// loop.go), and the right button's menu.
	clicks := gtk.NewGestureClick()
	clicks.ConnectPressed(func(n int, _, _ float64) { w.presses = n })
	w.area.AddController(clicks)

	right := gtk.NewGestureClick()
	right.SetButton(gdk.BUTTON_SECONDARY)
	right.ConnectPressed(func(_ int, x, y float64) {
		if g := w.groupAt(x, y); g >= 0 {
			w.barMenu(w.area, x, y, w.groupPos[g], w.barAt(w.groupPos[g]))
		} else {
			w.viewMenu(x, y)
		}
	})
	w.area.AddController(right)

	middle := gtk.NewGestureDrag()
	middle.SetButton(gdk.BUTTON_MIDDLE)
	middle.ConnectDragBegin(func(_, _ float64) { w.startPan() })
	middle.ConnectDragUpdate(w.panBy)
	middle.ConnectDragEnd(func(_, _ float64) { w.endPan() })
	w.area.AddController(middle)

	// Hovering: the pointer and the note a click would pick.
	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(x, y float64) {
		w.pointerX, w.pointerY = x, y
		g := w.groupAt(x, y)
		if g != w.hoverGroup {
			w.hoverGroup = g
			w.dirty = true
		}
		switch {
		case w.pan.active:
		case g >= 0:
			w.area.SetCursorFromName("pointer")
		default:
			w.area.SetCursorFromName("grab")
		}
	})
	motion.ConnectLeave(func() {
		w.hoverGroup = -1
		w.dirty = true
	})
	w.area.AddController(motion)

	wheel := gtk.NewEventControllerScroll(gtk.EventControllerScrollBothAxes)
	wheel.ConnectScroll(func(dx, dy float64) bool {
		state := wheel.CurrentEventState()
		switch {
		case state&gdk.ControlMask != 0:
			w.zoomAt(math.Pow(1.1, -dy), w.pointerX, w.pointerY)
			return true
		case state&gdk.ShiftMask != 0 && dx == 0:
			dx, dy = dy, 0
		}
		w.manualAt = time.Now()
		w.scrollTo(w.scrollX+dx*60, w.scrollY+dy*60)
		return true
	})
	w.area.AddController(wheel)
}

// zoomAt zooms by factor, keeping the point under x, y where it is.
func (w *window) zoomAt(factor, x, y float64) {
	px, py := (x-w.originX)/w.zoom, (y+w.scrollY)/w.zoom
	zoom := math.Min(6, math.Max(0.3, w.zoom*factor))
	if zoom == w.zoom {
		return
	}
	w.zoom = zoom
	w.prefs.Zoom = zoom
	w.prefsChanged = true
	w.manualAt = time.Now()
	w.scrollTo(px*zoom-x, py*zoom-y)
}

// drawHover marks the note a click would pick, faintly, with its bar
// number; cr is in page units times zoom, like the pages.
func (w *window) drawHover(cr *cairo.Context) {
	g := w.hoverGroup
	if g < 0 || g >= len(w.groupIDs) || w.drag.mode >= makingLoop || w.pan.active {
		return
	}
	a, ok := w.pages.Union(w.groupIDs[g])
	if !ok {
		return
	}
	line, ok := w.pages.SystemBox(w.groupSystem[g])
	if !ok {
		line = a
	}
	z := w.zoom
	x := (a.X + a.W/2) * z
	top, bottom := (line.Y-2.5)*z, (line.Y+line.H+2.5)*z
	width := math.Max(12, 2.6*z)
	roundedRect(cr, x-width/2, top, width, bottom-top, math.Min(6, width/3))
	cr.SetSourceRGBA(0.35, 0.35, 0.45, 0.10)
	cr.FillPreserve()
	cr.SetSourceRGBA(0.35, 0.35, 0.45, 0.35)
	cr.SetLineWidth(1)
	cr.Stroke()
	label(cr, fmt.Sprintf("bar %d", w.barAt(w.groupPos[g])), x, top-4, 11, rgb{0.45, 0.45, 0.55})
}

// The timeline.

func (w *window) addTimelineMouse() {
	clicks := gtk.NewGestureClick()
	clicks.ConnectPressed(func(n int, _, _ float64) { w.presses = n })
	w.timeline.AddController(clicks)

	right := gtk.NewGestureClick()
	right.SetButton(gdk.BUTTON_SECONDARY)
	right.ConnectPressed(func(_ int, x, y float64) {
		bar := w.barAt(w.timelinePos(x))
		start, _ := w.barSpan(bar)
		w.barMenu(w.timeline, x, y, start, bar)
	})
	w.timeline.AddController(right)
}

// The piano.

func (w *window) addPianoMouse() {
	var startX, startY float64
	// press plays the key at x, y, after letting go of the one before.
	press := func(x, y float64) {
		p, along, ok := w.pianoLayout(w.piano.Width(), w.piano.Height()).keyAt(x, y)
		if ok && p == w.mouseKey {
			return
		}
		w.releaseMouseKey()
		if ok {
			w.mouseKey = p
			w.s.sound(p, int(45+75*math.Min(1, along)), true, practice.Mouse)
			w.dirty = true
		}
	}
	drag := gtk.NewGestureDrag()
	drag.ConnectDragBegin(func(x, y float64) {
		startX, startY = x, y
		press(x, y)
	})
	drag.ConnectDragUpdate(func(dx, dy float64) { press(startX+dx, startY+dy) })
	drag.ConnectDragEnd(func(_, _ float64) { w.releaseMouseKey() })
	w.piano.AddController(drag)

	right := gtk.NewGestureClick()
	right.SetButton(gdk.BUTTON_SECONDARY)
	right.ConnectPressed(func(_ int, x, y float64) {
		p, _, ok := w.pianoLayout(w.piano.Width(), w.piano.Height()).keyAt(x, y)
		if !ok {
			return
		}
		c := p - p%12
		items := []menuItem{
			{label: fmt.Sprintf("Computer keys from %s (z)", noteName(c)), run: func() { w.setKeyBase(c) }},
			{},
		}
		if w.prefs.KeyHigh-p >= 11 {
			items = append(items, menuItem{label: fmt.Sprintf("Your keyboard starts at %s", noteName(p)),
				run: func() { w.setKeyboard(p, w.prefs.KeyHigh) }})
		}
		if p-w.prefs.KeyLow >= 11 {
			items = append(items, menuItem{label: fmt.Sprintf("Your keyboard ends at %s", noteName(p)),
				run: func() { w.setKeyboard(w.prefs.KeyLow, p) }})
		}
		items = append(items, menuItem{label: "All 88 keys", run: func() { w.setKeyboard(21, 108) }})
		w.popupMenu(w.piano, x, y, items)
	})
	w.piano.AddController(right)

	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(x, y float64) {
		p, _, ok := w.pianoLayout(w.piano.Width(), w.piano.Height()).keyAt(x, y)
		if !ok {
			p = -1
		}
		if p != w.pianoHover {
			w.pianoHover = p
			if ok {
				w.piano.SetCursorFromName("pointer")
			} else {
				w.piano.SetCursorFromName("default")
			}
		}
	})
	motion.ConnectLeave(func() { w.pianoHover = -1 })
	w.piano.AddController(motion)
}

func (w *window) releaseMouseKey() {
	if w.mouseKey >= 0 {
		w.s.sound(w.mouseKey, 0, false, practice.Mouse)
		w.mouseKey = -1
		w.dirty = true
	}
}

// The rest of the window.

// addSideButtons makes the mouse's back and forward buttons (8 and 9)
// move a bar, wherever the pointer is.
func (w *window) addSideButtons() {
	side := gtk.NewGestureClick()
	side.SetButton(0) // any; the others are left alone
	side.SetPropagationPhase(gtk.PhaseCapture)
	side.ConnectPressed(func(_ int, _, _ float64) {
		switch side.CurrentButton() {
		case 8:
			w.moveBars(-1)
		case 9:
			w.moveBars(1)
		default:
			return
		}
		side.SetState(gtk.EventSequenceClaimed)
	})
	w.win.AddController(side)
}

// wheelSteps turns scrolling over a widget into steps: one per notch of
// a mouse wheel, or per stretch of a touchpad's smooth scrolling. step
// gets +1 for down, -1 for up.
func wheelSteps(widget gtk.Widgetter, step func(int)) {
	var sum float64
	wheel := gtk.NewEventControllerScroll(gtk.EventControllerScrollVertical)
	wheel.ConnectScroll(func(_, dy float64) bool {
		sum += dy
		for ; sum >= 1; sum-- {
			step(1)
		}
		for ; sum <= -1; sum++ {
			step(-1)
		}
		return true
	})
	gtk.BaseWidget(widget).AddController(wheel)
}
