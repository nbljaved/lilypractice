package main

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/practice"
)

// The piano under the score shows the notes to play (in each hand's
// colour, with a dot), the keys you hold (darker when right, red when
// wrong), and the letters of the computer keys. It spans your keyboard's
// range, all 88 keys unless set otherwise, with the keys in a real
// piano's proportions: as wide as the height allows, centred.

type rgb struct{ r, g, b float64 }

// shade darkens a colour below 1 and lightens it towards white above 1.
func (c rgb) shade(f float64) rgb {
	if f <= 1 {
		return rgb{c.r * f, c.g * f, c.b * f}
	}
	t := f - 1
	return rgb{c.r + (1-c.r)*t, c.g + (1-c.g)*t, c.b + (1-c.b)*t}
}

var (
	ivory        = rgb{0.97, 0.97, 0.96}
	ebony        = rgb{0.10, 0.10, 0.12}
	wrongKey     = rgb{0.94, 0.30, 0.30}
	unjudgedKey  = rgb{0.70, 0.70, 0.74}
	bottomRowKey = []string{"z", "s", "x", "d", "c", "v", "g", "b", "h", "n", "j", "m", ",", "l", ".", ";", "/"}
	topRowKey    = []string{"q", "2", "w", "3", "e", "r", "5", "t", "6", "y", "7", "u", "i", "9", "o", "0", "p", "[", "=", "]"}
)

// handColour is how each hand's notes show on the piano: the right hand
// blue and the left orange, as in Sightread; a third staff violet.
func handColour(h practice.Hand) rgb {
	switch h {
	case practice.Right:
		return rgb{0.27, 0.56, 0.96}
	case practice.Left:
		return rgb{0.96, 0.52, 0.18}
	}
	return rgb{0.62, 0.48, 0.96}
}

// keyLook is how one key is lit.
type keyLook struct {
	c    rgb
	mark bool // a dot: play this key now
	ring bool // a ring: or this one, the same note on the computer keys
}

// A real piano's white key is about 23.5 mm wide and 150 mm long; a black
// key 13.7 mm wide and 95 mm long.
const (
	whiteKeyLength = 150 / 23.5 // in white key widths
	blackKeyWidth  = 13.7 / 23.5
	blackKeyLength = 95.0 / 150 // of a white key's
)

// blackKeyShift is how far a black key sits from the line between its two
// white keys, in white key widths. On a piano the five black keys of an
// octave aren't centred on those lines: C♯ and D♯ share three white keys
// evenly, and F♯, G♯ and A♯ four.
var blackKeyShift = map[int]float64{1: -0.1, 3: 0.1, 6: -1.0 / 7, 8: 0, 10: 1.0 / 7}

func isBlack(pitch int) bool {
	switch pitch % 12 {
	case 1, 3, 6, 8, 10:
		return true
	}
	return false
}

// keyRange is the range of keys drawn: your keyboard's, widened to white
// keys at both ends.
func (w *window) keyRange() (low, high int) {
	low, high = w.prefs.KeyLow, w.prefs.KeyHigh
	if isBlack(low) {
		low--
	}
	if isBlack(high) {
		high++
	}
	return low, high
}

// computerKeyLabel is the computer key that plays pitch, if any.
func (w *window) computerKeyLabel(pitch int) string {
	off := pitch - w.keyBase
	switch {
	case off >= 0 && off < len(bottomRowKey):
		return bottomRowKey[off]
	case off >= len(bottomRowKey) && off < 12+len(topRowKey):
		return topRowKey[off-12]
	}
	return ""
}

// nearestComputerKey is the computer key nearest to pitch that plays the
// same note in another octave.
func (w *window) nearestComputerKey(pitch int) (int, bool) {
	best, found := 0, false
	for q := w.keyBase; q < w.keyBase+12+len(topRowKey); q++ {
		if (q-pitch)%12 == 0 && (!found || abs(q-pitch) < abs(best-pitch)) {
			best, found = q, true
		}
	}
	return best, found
}

func abs(n int) int { return max(n, -n) }

// pianoLayout is where the keys are drawn.
type pianoLayout struct {
	low, high       int     // the keys drawn
	left, top, felt float64 // the first key's left edge; the keys' top
	kw, h           float64 // a white key's width and length
	bw, bh          float64 // a black key's
	whiteX          map[int]float64
}

func (w *window) pianoLayout(width, height int) pianoLayout {
	const felt = 7.0
	l := pianoLayout{felt: felt, top: felt, whiteX: make(map[int]float64)}
	l.low, l.high = w.keyRange()
	whites := 0
	for p := l.low; p <= l.high; p++ {
		if !isBlack(p) {
			whites++
		}
	}
	l.kw = math.Min(float64(width)/float64(whites), (float64(height)-felt)/whiteKeyLength)
	l.h = l.kw * whiteKeyLength
	l.bw, l.bh = l.kw*blackKeyWidth, l.h*blackKeyLength
	l.left = math.Floor((float64(width) - l.kw*float64(whites)) / 2)
	x := l.left
	for p := l.low; p <= l.high; p++ {
		if !isBlack(p) {
			l.whiteX[p] = x
			x += l.kw
		}
	}
	return l
}

func (l pianoLayout) right() float64 { return l.left + l.kw*float64(len(l.whiteX)) }

// blackX is the left edge of black key p.
func (l pianoLayout) blackX(p int) float64 {
	return l.whiteX[p-1] + l.kw + l.kw*blackKeyShift[p%12] - l.bw/2
}

// keyAt is the key at a point, and how far along it the point is, from 0
// at the back to 1 at the front.
func (l pianoLayout) keyAt(x, y float64) (pitch int, along float64, ok bool) {
	if y < l.top || y > l.top+l.h {
		return 0, 0, false
	}
	if y <= l.top+l.bh {
		for p := l.low; p <= l.high; p++ {
			if bx := l.blackX(p); isBlack(p) && x >= bx && x < bx+l.bw {
				return p, (y - l.top) / l.bh, true
			}
		}
	}
	for p, wx := range l.whiteX {
		if x >= wx && x < wx+l.kw {
			return p, (y - l.top) / l.h, true
		}
	}
	return 0, 0, false
}

func (w *window) drawPiano(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
	v := w.s.view()
	set := w.s.settings()
	held := w.s.heldKeys()
	l := w.pianoLayout(width, height)
	low, high := l.low, l.high

	// Later rules win: notes playing, then notes to play, then your keys.
	look := make(map[int]keyLook)
	for i, p := range v.SoundingPitches {
		look[p] = keyLook{c: handColour(v.SoundingHands[i])}
	}
	for i, p := range v.ExpectedPitches {
		look[p] = keyLook{c: handColour(v.ExpectedHands[i]).shade(1.3), mark: true}
	}
	// A note beyond the computer keys counts in any octave: ring the
	// nearest one that plays it. Not once you play a MIDI keyboard.
	if set.AnyOctave && set.Mode != practice.Listen && !w.s.playedMIDI() {
		for i, p := range v.ExpectedPitches {
			if q, ok := w.nearestComputerKey(p); ok && q != p {
				if !look[q].mark {
					look[q] = keyLook{c: handColour(v.ExpectedHands[i]).shade(1.65), ring: true}
				}
			}
		}
	}
	for p, k := range held {
		switch k.result {
		case practice.Perfect, practice.Good:
			look[p] = keyLook{c: handColour(k.hand).shade(0.82)}
		case practice.Wrong:
			look[p] = keyLook{c: wrongKey}
		default:
			look[p] = keyLook{c: unjudgedKey}
		}
	}

	kw, h, left, top, felt, right := l.kw, l.h, l.left, l.top, l.felt, l.right()

	cr.SetSourceRGB(0.08, 0.08, 0.10) // around the piano, as the timeline
	cr.Paint()
	cr.SetSourceRGB(0.05, 0.05, 0.06) // shows in the gaps between keys
	cr.Rectangle(left, top, right-left, h)
	cr.Fill()
	cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightBold)

	for p := low; p <= high; p++ {
		if isBlack(p) {
			continue
		}
		x := l.whiteX[p]
		k, lit := look[p]
		switch {
		case !lit && p == w.pianoHover:
			k.c = rgb{0.88, 0.90, 0.97}
		case !lit:
			k.c = ivory
		}
		_, down := held[p]
		whiteKey(cr, x+0.5, top, kw-1, h, k.c, down)
		text := rgb{0.30, 0.30, 0.36}
		if lit {
			text = k.c.shade(0.45)
		}
		// The dot goes below the black keys, where the white key is whole.
		dotY := top + h*blackKeyLength + h*(1-blackKeyLength)*0.2
		switch {
		case k.mark:
			dot(cr, x+kw/2, dotY, math.Min(4, kw*0.18), k.c.shade(0.55))
		case k.ring:
			ring(cr, x+kw/2, dotY, math.Min(4, kw*0.18), k.c.shade(0.55))
		}
		if k := w.computerKeyLabel(p); k != "" {
			label(cr, k, x+kw/2, top+h-22, math.Min(11, kw*0.5), text)
		}
		if p%12 == 0 {
			label(cr, noteName(p), x+kw/2, top+h-8, math.Min(9, kw*0.4), rgb{0.55, 0.55, 0.6})
		}
	}

	bw, bh := l.bw, l.bh
	for p := low; p <= high; p++ {
		if !isBlack(p) {
			continue
		}
		bx := l.blackX(p)
		k, lit := look[p]
		switch {
		case !lit && p == w.pianoHover:
			k.c = rgb{0.25, 0.26, 0.34}
		case !lit:
			k.c = ebony
		default:
			k.c = k.c.shade(0.8) // a black key stays darker than a white one
		}
		_, down := held[p]
		// The black key's shadow falls on the white keys.
		cr.SetSourceRGBA(0, 0, 0, 0.32)
		roundedBottom(cr, bx+1.5, top, bw+1.5, bh+3, 3)
		cr.Fill()
		blackKey(cr, bx, top, bw, bh, k.c, down)
		switch {
		case k.mark:
			dot(cr, bx+bw/2, top+bh*0.4, math.Min(3.5, bw*0.2), rgb{1, 1, 1})
		case k.ring:
			ring(cr, bx+bw/2, top+bh*0.4, math.Min(3.5, bw*0.2), rgb{1, 1, 1})
		}
		if k := w.computerKeyLabel(p); k != "" {
			label(cr, k, bx+bw/2, top+bh-12, math.Min(10, kw*0.45), rgb{0.88, 0.88, 0.92})
		}
	}

	// The felt strip, and the shadow it casts on the keys.
	f, _ := cairo.NewPatternLinear(0, 0, 0, felt)
	f.AddColorStopRGB(0, 0.42, 0.07, 0.09)
	f.AddColorStopRGB(1, 0.28, 0.03, 0.05)
	cr.SetSource(f)
	cr.Rectangle(left, 0, right-left, felt)
	cr.Fill()
	s, _ := cairo.NewPatternLinear(0, felt, 0, felt+10)
	s.AddColorStopRGBA(0, 0, 0, 0, 0.35)
	s.AddColorStopRGBA(1, 0, 0, 0, 0)
	cr.SetSource(s)
	cr.Rectangle(left, felt, right-left, 10)
	cr.Fill()
}

// whiteKey draws a white key: lit from the front, shaded under the felt,
// with a darker front edge. A key held down shows less of its front.
func whiteKey(cr *cairo.Context, x, y, w, h float64, c rgb, down bool) {
	front := 0.035
	if down {
		c = c.shade(0.94)
		front = 0.012
	}
	g, _ := cairo.NewPatternLinear(0, y, 0, y+h)
	g.AddColorStopRGB(0, c.shade(0.86).r, c.shade(0.86).g, c.shade(0.86).b)
	g.AddColorStopRGB(0.08, c.r, c.g, c.b)
	g.AddColorStopRGB(1-front-0.01, c.shade(1.06).r, c.shade(1.06).g, c.shade(1.06).b)
	g.AddColorStopRGB(1-front, c.shade(0.80).r, c.shade(0.80).g, c.shade(0.80).b)
	g.AddColorStopRGB(1, c.shade(0.70).r, c.shade(0.70).g, c.shade(0.70).b)
	cr.SetSource(g)
	roundedBottom(cr, x, y, w, h, 4)
	cr.Fill()
}

// blackKey draws a black key: a top that catches some light towards the
// front, a lighter bevel at the front, and a faint edge highlight.
func blackKey(cr *cairo.Context, x, y, w, h float64, c rgb, down bool) {
	bevel := h * 0.11
	if down {
		bevel = h * 0.04
	}
	body := h - bevel
	g, _ := cairo.NewPatternLinear(0, y, 0, y+body)
	g.AddColorStopRGB(0, c.shade(0.75).r, c.shade(0.75).g, c.shade(0.75).b)
	g.AddColorStopRGB(1, c.shade(1.12).r, c.shade(1.12).g, c.shade(1.12).b)
	cr.SetSource(g)
	roundedBottom(cr, x, y, w, h, 2.5)
	cr.Fill()

	b, _ := cairo.NewPatternLinear(0, y+body, 0, y+h)
	b.AddColorStopRGB(0, c.shade(1.32).r, c.shade(1.32).g, c.shade(1.32).b)
	b.AddColorStopRGB(1, c.shade(1.05).r, c.shade(1.05).g, c.shade(1.05).b)
	cr.SetSource(b)
	roundedBottom(cr, x+1, y+body, w-2, bevel-1, 2)
	cr.Fill()

	cr.SetSourceRGBA(1, 1, 1, 0.09)
	cr.Rectangle(x+1, y, 1, body-2)
	cr.Fill()
}

// roundedBottom outlines a rectangle with rounded bottom corners.
func roundedBottom(cr *cairo.Context, x, y, w, h, r float64) {
	r = math.Min(r, math.Min(w/2, h/2))
	cr.NewPath()
	cr.MoveTo(x, y)
	cr.LineTo(x+w, y)
	cr.LineTo(x+w, y+h-r)
	cr.Arc(x+w-r, y+h-r, r, 0, math.Pi/2)
	cr.LineTo(x+r, y+h)
	cr.Arc(x+r, y+h-r, r, math.Pi/2, math.Pi)
	cr.ClosePath()
}

// ring draws a circle's outline.
func ring(cr *cairo.Context, x, y, r float64, c rgb) {
	cr.SetSourceRGB(c.r, c.g, c.b)
	cr.SetLineWidth(1.5)
	cr.NewPath()
	cr.Arc(x, y, r, 0, 2*math.Pi)
	cr.Stroke()
}

func dot(cr *cairo.Context, x, y, r float64, c rgb) {
	cr.SetSourceRGB(c.r, c.g, c.b)
	cr.NewPath()
	cr.Arc(x, y, r, 0, 2*math.Pi)
	cr.Fill()
}

// label draws text centred on x with its baseline at y.
func label(cr *cairo.Context, text string, x, y, size float64, c rgb) {
	cr.SetFontSize(size)
	ext := cr.TextExtents(text)
	cr.SetSourceRGB(c.r, c.g, c.b)
	cr.MoveTo(x-ext.Width/2-ext.XBearing, y)
	cr.ShowText(text)
}
