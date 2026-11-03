package main

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/rsvg"
)

// Looping with the mouse: drag across bars, on the timeline or the score,
// to loop them; drag the loop's edges on the timeline to change it. A
// click without dragging moves the start point, a double-click plays from
// there; shift-click loops up to that bar. More of the mouse is in
// mouse.go.

type dragMode int

const (
	notDragging dragMode = iota
	pressed              // pressed, not moved yet: may still be a click
	makingLoop
	movingStart // the loop's first bar
	movingEnd   // the loop's last bar
)

type loopDrag struct {
	mode     dragMode
	fixed    int // the bar that stays put
	from, to int // the loop being drawn
}

// shownLoop is the loop to draw: the one being dragged, or the real one.
func (w *window) shownLoop() (from, to int, on bool) {
	if w.drag.mode >= makingLoop {
		return w.drag.from, w.drag.to, true
	}
	set := w.s.settings()
	return set.LoopFrom, set.LoopTo, set.Loop
}

func (w *window) dragTo(bar int) {
	switch w.drag.mode {
	case makingLoop, movingStart, movingEnd:
		w.drag.from, w.drag.to = min(w.drag.fixed, bar), max(w.drag.fixed, bar)
	}
	w.dirty = true
}

func (w *window) finishDrag() bool {
	mode := w.drag.mode
	w.drag.mode = notDragging
	if mode >= makingLoop {
		w.setLoop(true, w.drag.from, w.drag.to)
		return true
	}
	return false
}

// loopTo loops from the current bar (or the loop's start) to bar.
func (w *window) loopTo(bar int) {
	set := w.s.settings()
	from := set.LoopFrom
	if !set.Loop || from > bar {
		from = w.s.view().Bar
	}
	w.setLoop(true, min(from, bar), max(from, bar))
}

// Bars by number and position.

func (w *window) barAt(pos float64) int {
	n := w.bars[0].Number
	for _, b := range w.bars {
		if b.Pos > pos+1e-9 {
			break
		}
		n = b.Number
	}
	return n
}

// barSpan is where bar n starts and ends.
func (w *window) barSpan(n int) (start, end float64) {
	for i, b := range w.bars {
		if b.Number == n {
			end = w.end
			if i+1 < len(w.bars) {
				end = w.bars[i+1].Pos
			}
			return b.Pos, end
		}
	}
	return 0, 0
}

// barNearEdge is the bar starting (or, for the loop's end, ending) at the
// bar line nearest pos.
func (w *window) barNearEdge(pos float64, end bool) int {
	best, dist := 0, math.Inf(1)
	for i, b := range w.bars {
		edge := b.Pos
		if end {
			edge = w.end
			if i+1 < len(w.bars) {
				edge = w.bars[i+1].Pos
			}
		}
		if d := math.Abs(edge - pos); d < dist {
			best, dist = b.Number, d
		}
	}
	return best
}

// The timeline.

func (w *window) timelinePos(x float64) float64 {
	pos := (x - timelineMargin) / (float64(w.timeline.Width()) - 2*timelineMargin) * w.end
	return math.Max(0, math.Min(pos, w.end))
}

// nearLoopEdge says whether x is on the start or end edge of the loop.
func (w *window) nearLoopEdge(x float64) dragMode {
	from, to, on := w.s.loopSpan()
	if !on {
		return notDragging
	}
	width := w.timeline.Width()
	switch {
	case math.Abs(x-w.timelineX(from, width)) < 7:
		return movingStart
	case math.Abs(x-w.timelineX(to, width)) < 7:
		return movingEnd
	}
	return notDragging
}

func (w *window) addTimelineGestures() {
	var startX float64
	var shift bool
	drag := gtk.NewGestureDrag()
	drag.ConnectDragBegin(func(x, _ float64) {
		startX, shift = x, drag.CurrentEventState()&gdk.ShiftMask != 0
		set := w.s.settings()
		switch w.nearLoopEdge(x) {
		case movingStart:
			w.drag = loopDrag{mode: movingStart, fixed: set.LoopTo, from: set.LoopFrom, to: set.LoopTo}
		case movingEnd:
			w.drag = loopDrag{mode: movingEnd, fixed: set.LoopFrom, from: set.LoopFrom, to: set.LoopTo}
		default:
			bar := w.barAt(w.timelinePos(x))
			w.drag = loopDrag{mode: pressed, fixed: bar, from: bar, to: bar}
		}
	})
	drag.ConnectDragUpdate(func(dx, _ float64) {
		pos := w.timelinePos(startX + dx)
		switch w.drag.mode {
		case pressed:
			if math.Abs(dx) > 4 {
				w.drag.mode = makingLoop
				w.dragTo(w.barAt(pos))
			}
		case makingLoop:
			w.dragTo(w.barAt(pos))
		case movingStart:
			w.dragTo(w.barNearEdge(pos, false))
		case movingEnd:
			w.dragTo(w.barNearEdge(pos, true))
		}
	})
	drag.ConnectDragEnd(func(_, _ float64) {
		bar := w.drag.fixed
		if w.drag.mode == pressed {
			w.drag.mode = notDragging
			start, _ := w.barSpan(bar)
			switch {
			case shift:
				w.loopTo(bar)
			case w.presses >= 2:
				w.playFrom(start)
			default:
				w.seekPos(start)
			}
			return
		}
		w.finishDrag()
	})
	w.timeline.AddController(drag)

	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(x, _ float64) {
		if w.nearLoopEdge(x) != notDragging {
			w.timeline.SetCursorFromName("ew-resize")
		} else {
			w.timeline.SetCursorFromName("pointer")
		}
	})
	w.timeline.AddController(motion)
}

// The score.

func (w *window) addScoreGestures() {
	var startX, startY float64
	var shift bool
	start := -1 // group pressed on
	drag := gtk.NewGestureDrag()
	drag.ConnectDragBegin(func(x, y float64) {
		startX, startY = x, y
		shift = drag.CurrentEventState()&gdk.ShiftMask != 0
		start = w.groupAt(x, y)
		w.drag = loopDrag{}
		if start < 0 { // no notes here: move the view
			w.startPan()
			return
		}
		bar := w.barAt(w.groupPos[start])
		w.drag = loopDrag{mode: pressed, fixed: bar, from: bar, to: bar}
	})
	drag.ConnectDragUpdate(func(dx, dy float64) {
		if w.pan.active {
			w.panBy(dx, dy)
			return
		}
		if w.drag.mode == notDragging {
			return
		}
		if w.drag.mode == pressed && math.Hypot(dx, dy) > 6 {
			w.drag.mode = makingLoop
		}
		if w.drag.mode == makingLoop {
			if g := w.groupAt(startX+dx, startY+dy); g >= 0 {
				w.dragTo(w.barAt(w.groupPos[g]))
			}
		}
	})
	drag.ConnectDragEnd(func(_, _ float64) {
		if w.pan.active {
			w.endPan()
			return
		}
		if w.drag.mode != pressed {
			w.finishDrag()
			return
		}
		w.drag.mode = notDragging
		switch {
		case shift:
			w.loopTo(w.barAt(w.groupPos[start]))
		case w.presses >= 2:
			w.playFrom(w.groupPos[start])
		default:
			w.seekPos(w.groupPos[start])
		}
	})
	w.area.AddController(drag)
}

// groupAt is the note group a click at a point in the score area picks:
// on the line of music under the point, the one nearest across; or -1.
func (w *window) groupAt(x, y float64) int {
	px, py := (x-w.originX)/w.zoom, (y+w.scrollY)/w.zoom
	best, bestDist := -1, math.Inf(1)
	for i, ids := range w.groupIDs {
		b, ok := w.pages.Union(ids)
		if !ok {
			continue
		}
		line, ok := w.systemBox(w.groupSystem[i])
		if !ok {
			line = b
		}
		if py < line.Y-20 || py > line.Y+line.H+20 || px < line.X-10 || px > line.X+line.W+10 {
			continue
		}
		if d := math.Abs(b.X + b.W/2 - px); d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// systemBox is pages.SystemBox, remembered: groupAt asks on every move of
// the pointer.
func (w *window) systemBox(system int) (rsvg.Rect, bool) {
	if w.systemBoxes == nil {
		w.systemBoxes = make(map[int]*rsvg.Rect)
	}
	r, seen := w.systemBoxes[system]
	if !seen {
		if box, ok := w.pages.SystemBox(system); ok {
			r = &box
		}
		w.systemBoxes[system] = r
	}
	if r == nil {
		return rsvg.Rect{}, false
	}
	return *r, true
}

// barAreas finds, for each bar, the parts of the score it covers: one
// rectangle per line of music, as tall as that line's staves, in page
// units.
func (w *window) barAreas() map[int][]rsvg.Rect {
	if w.barAreaCache != nil {
		return w.barAreaCache
	}
	// The groups of each line of music, in order.
	var systems []int
	groups := map[int][]int{}
	for i, s := range w.groupSystem {
		if _, seen := groups[s]; !seen {
			systems = append(systems, s)
		}
		groups[s] = append(groups[s], i)
	}
	areas := make(map[int][]rsvg.Rect)
	for _, s := range systems {
		gs := groups[s]
		line, ok := w.pages.SystemBox(s)
		if !ok {
			line, _ = w.pages.Union(w.groupIDs[gs[0]])
		}
		for k := 0; k < len(gs); {
			bar := w.barAt(w.groupPos[gs[k]])
			first, _ := w.pages.Union(w.groupIDs[gs[k]])
			end := k
			for end < len(gs) && w.barAt(w.groupPos[gs[end]]) == bar {
				end++
			}
			right := line.X + line.W
			if end < len(gs) {
				next, _ := w.pages.Union(w.groupIDs[gs[end]])
				right = next.X - 1.5
			}
			left := first.X - 1.5
			areas[bar] = append(areas[bar], rsvg.Rect{X: left, Y: line.Y - 3, W: right - left, H: line.H + 6})
			k = end
		}
	}
	w.barAreaCache = areas
	return areas
}

// drawLoopOnScore shades the looped bars on the score; cr is in page
// units times zoom, like the pages.
func (w *window) drawLoopOnScore(cr *cairo.Context) {
	from, to, on := w.shownLoop()
	if !on {
		return
	}
	areas := w.barAreas()
	z := w.zoom
	for bar := from; bar <= to; bar++ {
		for i, r := range areas[bar] {
			cr.SetSourceRGBA(0.49, 0.23, 0.93, 0.10)
			cr.Rectangle(r.X*z, r.Y*z, r.W*z, r.H*z)
			cr.Fill()
			cr.SetSourceRGBA(0.49, 0.23, 0.93, 0.85)
			if bar == from && i == 0 {
				cr.Rectangle(r.X*z-1.5, r.Y*z, 3, r.H*z)
				cr.Fill()
			}
			if bar == to && i == len(areas[bar])-1 {
				cr.Rectangle((r.X+r.W)*z-1.5, r.Y*z, 3, r.H*z)
				cr.Fill()
			}
		}
	}
}
