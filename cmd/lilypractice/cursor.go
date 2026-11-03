package main

import (
	"math"
	"strconv"
	"time"

	"github.com/diamondburned/gotk4/pkg/cairo"

	"lilypractice/practice"
	"lilypractice/rsvg"
	"lilypractice/view"
)

// The cursor marks where the music is. It spans the whole line of music,
// from the top staff to the bottom one, and glides between the notes.

type cursorStyle struct{ id, name string }

var cursorStyles = []cursorStyle{
	{"column", "Column"},    // a translucent band, like Sightread's
	{"line", "Line"},        // a bold line with markers above and below
	{"bar", "Bar"},          // the current bar shaded, with a thin line
	{"notes", "Notes only"}, // no cursor; the notes to play are coloured
}

type cursorColour struct {
	name string
	c    view.Color
}

var cursorColours = []cursorColour{
	{"Violet", view.Color{R: 0.49, G: 0.23, B: 0.93}},
	{"Blue", view.Color{R: 0.15, G: 0.45, B: 0.95}},
	{"Green", view.Color{R: 0.10, G: 0.62, B: 0.30}},
	{"Orange", view.Color{R: 0.95, G: 0.50, B: 0.08}},
	{"Red", view.Color{R: 0.88, G: 0.15, B: 0.15}},
}

func (w *window) cursorColour() view.Color {
	if time.Since(w.wrongAt) < 400*time.Millisecond {
		return view.Red // a wrong key
	}
	for _, c := range cursorColours {
		if c.name == w.prefs.CursorColour {
			return c.c
		}
	}
	return cursorColours[0].c
}

// cursorPlace is where the cursor is, in page units.
type cursorPlace struct {
	x    float64
	line rsvg.Rect // the line of music it is on
	bar  int
}

// cursor places the cursor between the group just played and the next
// one, so it glides along a line of music; it jumps at line ends.
func (w *window) cursor(v practice.View) (cursorPlace, bool) {
	k := max(v.Group, 0)
	if k >= len(w.groupIDs) {
		return cursorPlace{}, false
	}
	a, ok := w.pages.Union(w.groupIDs[k])
	if !ok {
		return cursorPlace{}, false
	}
	x := a.X + a.W/2
	if k+1 < len(w.groupIDs) && v.Group >= 0 && w.groupSystem[k+1] == w.groupSystem[k] {
		if b, ok := w.pages.Union(w.groupIDs[k+1]); ok {
			f := (v.Pos - w.groupPos[k]) / (w.groupPos[k+1] - w.groupPos[k])
			x += math.Min(1, math.Max(0, f)) * (b.X + b.W/2 - x)
		}
	}
	line, ok := w.pages.SystemBox(w.groupSystem[k])
	if !ok { // no staves found: span the notes
		line = a
	}
	return cursorPlace{x: x, line: line, bar: v.Bar}, true
}

// drawCursor draws the cursor in the chosen style; cr is in page units
// times zoom, like the pages.
func (w *window) drawCursor(cr *cairo.Context, at cursorPlace) {
	c := w.cursorColour()
	z := w.zoom
	top, bottom := (at.line.Y-2.5)*z, (at.line.Y+at.line.H+2.5)*z
	x := at.x * z
	cr.Save()
	defer cr.Restore()
	switch w.prefs.CursorStyle {
	case "line":
		width := math.Max(2.5, 0.35*z)
		cr.SetSourceRGBA(c.R, c.G, c.B, 0.9)
		cr.Rectangle(x-width/2, top, width, bottom-top)
		cr.Fill()
		s := math.Max(5, 1.3*z) // markers pointing at the line
		cr.MoveTo(x-s, top-s*1.3)
		cr.LineTo(x+s, top-s*1.3)
		cr.LineTo(x, top)
		cr.ClosePath()
		cr.MoveTo(x-s, bottom+s*1.3)
		cr.LineTo(x+s, bottom+s*1.3)
		cr.LineTo(x, bottom)
		cr.ClosePath()
		cr.Fill()
	case "bar":
		for _, r := range w.barAreas()[at.bar] {
			if r.Y+r.H > at.line.Y && r.Y < at.line.Y+at.line.H {
				cr.SetSourceRGBA(c.R, c.G, c.B, 0.13)
				cr.Rectangle(r.X*z, top, r.W*z, bottom-top)
				cr.Fill()
			}
		}
		cr.SetSourceRGBA(c.R, c.G, c.B, 0.8)
		cr.Rectangle(x-1, top, 2, bottom-top)
		cr.Fill()
	case "notes":
	default: // column
		width := math.Max(14, 3*z)
		roundedRect(cr, x-width/2, top, width, bottom-top, math.Min(6, width/3))
		cr.SetSourceRGBA(c.R, c.G, c.B, 0.22)
		cr.FillPreserve()
		cr.SetSourceRGBA(c.R, c.G, c.B, 0.7)
		cr.SetLineWidth(1.5)
		cr.Stroke()
	}
}

func roundedRect(cr *cairo.Context, x, y, w, h, r float64) {
	cr.NewPath()
	cr.Arc(x+w-r, y+r, r, -math.Pi/2, 0)
	cr.Arc(x+w-r, y+h-r, r, 0, math.Pi/2)
	cr.Arc(x+r, y+h-r, r, math.Pi/2, math.Pi)
	cr.Arc(x+r, y+r, r, math.Pi, 3*math.Pi/2)
	cr.ClosePath()
}

// drawCountdown shows the beats left before the music starts, large, in
// the middle of the score area.
func (w *window) drawCountdown(cr *cairo.Context, beats, width, height int) {
	c := w.cursorColour()
	x, y := float64(width)/2, float64(height)*0.4
	cr.NewPath()
	cr.Arc(x, y, 70, 0, 2*math.Pi)
	cr.SetSourceRGBA(0.08, 0.08, 0.10, 0.72)
	cr.FillPreserve()
	cr.SetSourceRGBA(c.R, c.G, c.B, 0.95)
	cr.SetLineWidth(4)
	cr.Stroke()
	cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightBold)
	cr.SetFontSize(72)
	text := strconv.Itoa(beats)
	ext := cr.TextExtents(text)
	cr.SetSourceRGB(1, 1, 1)
	cr.MoveTo(x-ext.Width/2-ext.XBearing, y-ext.Height/2-ext.YBearing)
	cr.ShowText(text)
}

// Scrolling. The view glides towards where it should be, rather than
// jumping, and stops following the music for a moment after you scroll.

const glideTime = 0.18 // seconds to cover about two thirds of the way

func (w *window) maxScroll() (x, y float64) {
	pw, ph := w.pages.Size()
	return math.Max(0, pw*w.zoom-float64(w.area.Width())), math.Max(0, ph*w.zoom-float64(w.area.Height()))
}

// scrollTo moves the view at once, as when you scroll it yourself.
func (w *window) scrollTo(x, y float64) {
	mx, my := w.maxScroll()
	w.scrollX, w.scrollY = math.Min(mx, math.Max(0, x)), math.Min(my, math.Max(0, y))
	w.targetX, w.targetY = w.scrollX, w.scrollY
	w.dirty = true
}

// glideTo sets where the view should glide to.
func (w *window) glideTo(x, y float64) {
	mx, my := w.maxScroll()
	w.targetX, w.targetY = math.Min(mx, math.Max(0, x)), math.Min(my, math.Max(0, y))
}

// glide moves the view part of the way to its target; true if it moved.
func (w *window) glide(dt float64) bool {
	dx, dy := w.targetX-w.scrollX, w.targetY-w.scrollY
	if math.Abs(dx) < 0.5 && math.Abs(dy) < 0.5 {
		w.scrollX, w.scrollY = w.targetX, w.targetY
		return false
	}
	f := 1 - math.Exp(-dt/glideTime)
	w.scrollX += dx * f
	w.scrollY += dy * f
	return true
}

// follow keeps the cursor's line of music in the upper part of the view
// and the cursor away from its sides, unless you scrolled a moment ago.
func (w *window) follow(v practice.View, now bool) {
	if !now && time.Since(w.manualAt) < 3*time.Second {
		return
	}
	at, ok := w.cursor(v)
	if !ok {
		return
	}
	x, y := w.targetX, w.targetY
	width, height := float64(w.area.Width()), float64(w.area.Height())
	top, bottom := (at.line.Y-12)*w.zoom, (at.line.Y+at.line.H+12)*w.zoom
	if top < y || bottom > y+height*0.7 {
		y = top - height*0.2
	}
	if cx := at.x * w.zoom; cx < x+width*0.1 || cx > x+width*0.9 {
		x = cx - width*0.35
	}
	if now {
		w.scrollTo(x, y)
	} else {
		w.glideTo(x, y)
	}
}

// fitWidth zooms so the pages fill the width of the view.
func (w *window) fitWidth() {
	pw, _ := w.pages.Size()
	if width := float64(w.area.Width()); width > 0 && pw > 0 {
		w.setZoom((width - 24) / pw)
	}
}
