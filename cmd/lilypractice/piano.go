package main

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/midikeys"
	"lilypractice/practice"
)

// The piano under the score shows the notes to play (in each hand's
// colour, with a dot), the keys you hold (darker when right, red when
// wrong), and the letters of the computer keys. It spans your keyboard's
// range, all 88 keys unless set otherwise, with the keys in a real
// piano's proportions: as wide as the height allows, centred. The keys
// are drawn with depth, as seen from above the keyboard, and they move:
// the keys you hold and the notes the music plays go down.

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
	ivory        = rgb{0.96, 0.95, 0.89} // warm, as Pianoteq's
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
	blackKeyFront  = 0.16       // of its length, its rounded front as seen from above
)

// frontShowing is how much of a key's front, front high at rest, shows
// when the key is down by depth: the key slip hides it as the key sinks,
// and more of it shows as the key bounces back above its rest.
func frontShowing(front, depth float64) float64 { return front * (1 - 0.5*depth) }

// The keys move as a piano's do: down at once when pressed, with a little
// bounce off the key bed, and back up when let go, rising past their rest
// before they settle.

// spring is a key in motion: how far down it is, from 0 at rest to 1 on
// the key bed, and how fast it moves, in depths a second.
type spring struct{ depth, speed float64 }

// keyMotion is the piano's keys that aren't at rest.
type keyMotion map[int]*spring

// Each key moves as a damped spring: going down quick and firm, coming
// back up slower and bouncier.
const (
	pressHz, pressDamping     = 14.0, 0.55 // the damping of critical damping
	releaseHz, releaseDamping = 6.0, 0.35
	keyBedBounce              = 0.3 // of its speed a key keeps off the key bed
)

// step moves the keys on by dt seconds, those of down towards the key bed
// and the others back to rest, and says whether any moved.
func (m keyMotion) step(dt float64, down map[int]bool) bool {
	for p := range down {
		if m[p] == nil {
			m[p] = &spring{}
		}
	}
	const h = 0.001 // seconds a step: the springs are stiff
	moved := false
	for p, s := range m {
		target, hz, damping := 0.0, releaseHz, releaseDamping
		if down[p] {
			target, hz, damping = 1, pressHz, pressDamping
		}
		if s.depth == target && s.speed == 0 {
			continue
		}
		moved = true
		w := 2 * math.Pi * hz
		for t := 0.0; t < dt; t += h {
			s.speed += (w*w*(target-s.depth) - 2*damping*w*s.speed) * h
			s.depth += s.speed * h
			if s.depth > 1 {
				s.depth, s.speed = 1, -s.speed*keyBedBounce
			}
		}
		if math.Abs(s.depth-target) < 0.002 && math.Abs(s.speed) < 0.05 {
			s.depth, s.speed = target, 0
			if target == 0 {
				delete(m, p)
			}
		}
	}
	return moved
}

// depth is how far down key p is.
func (m keyMotion) depth(p int) float64 {
	if s := m[p]; s != nil {
		return s.depth
	}
	return 0
}

// sustain is the notes a sustain pedal holds: those whose keys came up
// while it was down, until it comes up.
type sustain struct {
	was, holds map[int]bool // the keys down last time; the notes held
}

// follow takes the keys down now, and whether the pedal is.
func (s *sustain) follow(down map[int]bool, pedal bool) {
	defer func() { s.was = down }()
	if !pedal {
		s.holds = nil
		return
	}
	if s.holds == nil {
		s.holds = make(map[int]bool)
	}
	for p := range s.was {
		if !down[p] {
			s.holds[p] = true
		}
	}
}

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
	slip            float64 // the key slip's height, below the keys
	kw, h           float64 // a white key's width and length
	front           float64 // how much of a white key's front shows at rest
	bw, bh          float64 // a black key's
	whiteX          map[int]float64
	// The keys are seen in perspective, as Pianoteq shows them: going back
	// from their front edge, their tops narrow towards a point high above
	// the middle of the keyboard, up from it; going down, their fronts
	// narrow towards a point below it, down from it.
	mid, up, down float64
}

func (w *window) pianoLayout(width, height int) pianoLayout {
	const felt, slip = 7.0, 6.0
	l := pianoLayout{felt: felt, top: felt, slip: slip, whiteX: make(map[int]float64)}
	l.low, l.high = w.keyRange()
	whites := 0
	for p := l.low; p <= l.high; p++ {
		if !isBlack(p) {
			whites++
		}
	}
	l.kw = math.Min(float64(width)/float64(whites), (float64(height)-felt-slip)/whiteKeyLength)
	l.h = l.kw * whiteKeyLength
	l.front = math.Min(l.h*0.07, l.kw*0.45)
	l.bw, l.bh = l.kw*blackKeyWidth, l.h*blackKeyLength
	l.left = math.Floor((float64(width) - l.kw*float64(whites)) / 2)
	l.mid = l.left + l.kw*float64(whites)/2
	l.up, l.down = 145*l.kw, 45*l.kw // as Pianoteq's
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

// edge is where the white keys' tops end at rest, and their fronts begin.
func (l pianoLayout) edge() float64 { return l.top + l.h - l.front }

// narrowing is how much narrower the piano is drawn at y, about its middle.
func (l pianoLayout) narrowing(y float64) float64 {
	e := l.edge()
	if y < e {
		return (e - y) / l.up
	}
	return (y - e) / l.down
}

// at is where the point x, y of the flat piano is drawn.
func (l pianoLayout) at(x, y float64) (float64, float64) {
	return l.mid + (x-l.mid)*(1-l.narrowing(y)), y
}

// blackX is the left edge of black key p.
func (l pianoLayout) blackX(p int) float64 {
	return l.whiteX[p-1] + l.kw + l.kw*blackKeyShift[p%12] - l.bw/2
}

// keyAt is the key drawn at a point, and how far along it the point is,
// from 0 at the back to 1 at the front.
func (l pianoLayout) keyAt(x, y float64) (pitch int, along float64, ok bool) {
	if y < l.top || y > l.top+l.h {
		return 0, 0, false
	}
	x = l.mid + (x-l.mid)/(1-l.narrowing(y)) // on the flat piano
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

// pen draws on the piano in its flat coordinates, as the piano is seen.
type pen struct {
	cr             *cairo.Context
	l              pianoLayout
	x, y           float64 // the current point
	startX, startY float64 // where the path began
}

func (p *pen) moveTo(x, y float64) {
	p.x, p.y, p.startX, p.startY = x, y, x, y
	p.cr.MoveTo(p.l.at(x, y))
}

// lineTo draws a line, bent where it crosses the keys' edge, where the
// piano's lean changes.
func (p *pen) lineTo(x, y float64) {
	if e := p.l.edge(); (p.y-e)*(y-e) < 0 {
		p.cr.LineTo(p.l.at(p.x+(x-p.x)*(e-p.y)/(y-p.y), e))
	}
	p.x, p.y = x, y
	p.cr.LineTo(p.l.at(x, y))
}

func (p *pen) curveTo(x1, y1, x2, y2, x, y float64) {
	ax, ay := p.l.at(x1, y1)
	bx, by := p.l.at(x2, y2)
	cx, cy := p.l.at(x, y)
	p.x, p.y = x, y
	p.cr.CurveTo(ax, ay, bx, by, cx, cy)
}

func (p *pen) close() {
	p.lineTo(p.startX, p.startY)
	p.cr.ClosePath()
}

func (p *pen) rect(x, y, w, h float64) {
	p.moveTo(x, y)
	p.lineTo(x+w, y)
	p.lineTo(x+w, y+h)
	p.lineTo(x, y+h)
	p.close()
}

// roundedBottom outlines a rectangle with rounded bottom corners.
func (p *pen) roundedBottom(x, y, w, h, r float64) {
	r = math.Min(r, math.Min(w/2, h/2))
	const k = 0.5523 // a quarter circle's Bézier handles, of its radius
	p.moveTo(x, y)
	p.lineTo(x+w, y)
	p.lineTo(x+w, y+h-r)
	p.curveTo(x+w, y+h-r+k*r, x+w-r+k*r, y+h, x+w-r, y+h)
	p.lineTo(x+r, y+h)
	p.curveTo(x+r-k*r, y+h, x, y+h-r+k*r, x, y+h-r)
	p.close()
}

// leaning sets g, a gradient on the flat piano, as the source, leaning as
// the keys' tops about x do, so that it follows them.
func (p *pen) leaning(g *cairo.Pattern, x float64) {
	s, e := (x-p.l.mid)/p.l.up, p.l.edge()
	p.cr.Transform(cairo.NewMatrix(1, 0, s, 1, -s*e, 0))
	p.cr.SetSource(g)
	p.cr.Transform(cairo.NewMatrix(1, 0, -s, 1, s*e, 0))
}

func (w *window) drawPiano(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
	v := w.v
	set := w.s.settings()

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
	for _, b := range w.commandNext { // the keys that do something now
		for _, k := range b.Step() {
			if b.Command != midikeys.Leave && k < midikeys.Pedal {
				look[k] = keyLook{c: commandColour}
			}
		}
	}
	for p, k := range w.s.heldKeys() {
		switch k.result {
		case practice.Perfect, practice.Good:
			look[p] = keyLook{c: handColour(k.hand).shade(0.82)}
		case practice.Wrong:
			look[p] = keyLook{c: wrongKey}
		default:
			look[p] = keyLook{c: unjudgedKey}
		}
	}
	w.paintPiano(cr, w.pianoLayout(width, height), look)
}

// paintPiano paints the keys of l, lit as look says and down as far as
// they have moved.
func (w *window) paintPiano(cr *cairo.Context, l pianoLayout, look map[int]keyLook) {
	low, high := l.low, l.high
	kw, h, left, top, felt, right := l.kw, l.h, l.left, l.top, l.felt, l.right()
	pn := &pen{cr: cr, l: l}

	cr.SetSourceRGB(0.08, 0.08, 0.10) // around the piano, as the timeline
	cr.Paint()
	cr.SetSourceRGB(0.36, 0.32, 0.27) // shows in the gaps between the keys' tops
	pn.rect(left, top, right-left, l.edge()-top)
	cr.Fill()
	cr.SetSourceRGB(0.10, 0.10, 0.10) // and between their fronts
	pn.rect(left, l.edge(), right-left, top+h-l.edge())
	cr.Fill()
	cr.SetSourceRGB(0.14, 0.15, 0.16) // the key slip, which the keys' fronts sink behind
	pn.rect(left, top+h, right-left, l.slip)
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
		d := w.moving.depth(p)
		whiteKey(pn, x+0.5, top, kw-1, h, l.front, k.c, d)
		// What is on the key goes down with it, as its top grows longer.
		stretch := (h - frontShowing(l.front, d)) / (h - l.front)
		on := func(y float64) (float64, float64) { return l.at(x+kw/2, top+y*stretch) }
		text := rgb{0.30, 0.30, 0.36}
		if lit {
			text = k.c.shade(0.45)
		}
		// The dot goes below the black keys, where the white key is whole.
		dx, dy := on(h*blackKeyLength + h*(1-blackKeyLength)*0.2)
		switch {
		case k.mark:
			dot(cr, dx, dy, math.Min(4, kw*0.18), k.c.shade(0.55))
		case k.ring:
			ring(cr, dx, dy, math.Min(4, kw*0.18), k.c.shade(0.55))
		}
		if k := w.computerKeyLabel(p); k != "" {
			lx, ly := on(h - l.front - 17)
			label(cr, k, lx, ly, math.Min(11, kw*0.5), text)
		}
		if p%12 == 0 {
			lx, ly := on(h - l.front - 3)
			label(cr, noteName(p), lx, ly, math.Min(9, kw*0.4), rgb{0.55, 0.55, 0.6})
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
		d := w.moving.depth(p)
		blackShadow(pn, bx, top, bw, bh, d)
		blackKey(pn, bx, top, bw, bh, k.c, d)
		front := bh * blackKeyFront
		stretch := (bh - frontShowing(front, d)) / (bh - front)
		dx, dy := l.at(bx+bw/2, top+bh*0.4*stretch)
		switch {
		case k.mark:
			dot(cr, dx, dy, math.Min(3.5, bw*0.2), rgb{1, 1, 1})
		case k.ring:
			ring(cr, dx, dy, math.Min(3.5, bw*0.2), rgb{1, 1, 1})
		}
		if k := w.computerKeyLabel(p); k != "" {
			lx, ly := l.at(bx+bw/2, top+(bh-front-2)*stretch)
			label(cr, k, lx, ly, math.Min(10, kw*0.45), rgb{0.88, 0.88, 0.92})
		}
	}

	// The fallboard, the felt strip along it, and the long shadow they
	// cast on the keys.
	cr.SetSourceRGB(0.07, 0.07, 0.08)
	pn.rect(left, 0, right-left, felt)
	cr.Fill()
	cr.SetSourceRGB(0.34, 0.02, 0.03)
	pn.rect(left, felt*0.45, right-left, felt*0.55)
	cr.Fill()
	s, _ := cairo.NewPatternLinear(0, felt, 0, felt+h*0.17)
	s.AddColorStopRGBA(0, 0, 0, 0, 0.5)
	s.AddColorStopRGBA(1, 0, 0, 0, 0)
	cr.SetSource(s)
	pn.rect(left, felt, right-left, h*0.17)
	cr.Fill()
}

// whiteKey draws a white key of length h, front high of its front showing
// at rest, down by depth, flat as Pianoteq's: a top with a lit left edge,
// then its front, darker, with a dark crease under the top's edge. Down,
// its front sinks behind the key slip, and the keys beside it, now
// higher, shade its edges, more on the left.
func whiteKey(p *pen, x, y, w, h, front float64, c rgb, depth float64) {
	cr := p.cr
	end := h - frontShowing(front, depth) // the top's length

	f := h - end
	g, _ := cairo.NewPatternLinear(0, y+end, 0, y+h)
	stop(g, 0, c.shade(0.44))
	stop(g, math.Min(0.35, 3/f), c.shade(0.71))
	stop(g, 1-math.Min(0.3, 1.5/f), c.shade(0.69))
	stop(g, 1, c.shade(0.42))
	cr.SetSource(g)
	p.roundedBottom(x, y+end, w, f, 2.5)
	cr.Fill()

	cr.SetSourceRGB(c.r, c.g, c.b)
	p.rect(x, y, w, end)
	cr.Fill()
	cr.SetSourceRGBA(1, 1, 1, 0.6)
	p.rect(x, y, 1, end)
	cr.Fill()
	cr.SetSourceRGBA(0, 0, 0, 0.18)
	p.rect(x+w-1, y, 1, end)
	cr.Fill()

	if tilt := math.Max(0, depth); tilt > 0 {
		for _, s := range []struct{ from, width, alpha float64 }{{x, w * 0.18, 0.2}, {x + w, -w * 0.08, 0.12}} {
			g, _ := cairo.NewPatternLinear(s.from, 0, s.from+s.width, 0)
			g.AddColorStopRGBA(0, 0, 0, 0, s.alpha*tilt)
			g.AddColorStopRGBA(1, 0, 0, 0, 0)
			p.rect(math.Min(s.from, s.from+s.width), y, math.Abs(s.width), end)
			p.leaning(g, x+w/2)
			cr.Fill()
		}
	}
}

// blackKey draws a black key down by depth, as Pianoteq's: a dark
// outline, a top whose right part, sloping away, is darker, and at the
// front a lighter cap, with rounded shoulders, that rounds down to its
// lower edge. Down, less of its cap shows.
func blackKey(p *pen, x, y, w, h float64, c rgb, depth float64) {
	cr := p.cr
	front := frontShowing(h*blackKeyFront, depth)
	end := y + h - front // where the cap starts
	l, r := x+1, x+w-1   // inside the outline
	shoulder := end + front*0.55

	cr.SetSourceRGB(c.shade(0.55).r, c.shade(0.55).g, c.shade(0.55).b)
	p.roundedBottom(x, y, w, h, 2.5)
	cr.Fill()

	top := c.shade(1.045)
	cr.SetSourceRGB(top.r, top.g, top.b)
	p.rect(l, y, r-l, shoulder-y)
	cr.Fill()
	slope := top.shade(0.75)
	cr.SetSourceRGB(slope.r, slope.g, slope.b)
	p.rect(l+(r-l)*0.68, y, (r-l)*0.32, shoulder-y)
	cr.Fill()

	cr.Save()
	p.roundedBottom(l, y, r-l, h-1, 2)
	cr.Clip()
	in := (r - l) * 0.15
	p.moveTo(l, y+h)
	p.lineTo(l, shoulder)
	p.curveTo(l, end, l, end, l+in, end)
	p.lineTo(r-in, end)
	p.curveTo(r, end, r, end, r, shoulder)
	p.lineTo(r, y+h)
	p.close()
	g, _ := cairo.NewPatternLinear(0, end, 0, y+h-1)
	stop(g, 0, c.shade(1.25))
	stop(g, 0.7, c.shade(1.25))
	stop(g, 1, c.shade(0.6))
	cr.SetSource(g)
	cr.Fill()
	cr.Restore()
}

// blackShadow draws the shadow a black key casts on the white keys to its
// right, soft and wide; it narrows as the key sinks towards them.
func blackShadow(p *pen, x, y, w, h, depth float64) {
	width := w * 0.55 * (1 - 0.5*math.Max(0, depth))
	g, _ := cairo.NewPatternLinear(x+w, 0, x+w+width, 0)
	g.AddColorStopRGBA(0, 0, 0, 0, 0.27)
	g.AddColorStopRGBA(1, 0, 0, 0, 0)
	p.rect(x+w-1, y, width+1, h-1)
	p.leaning(g, x+w)
	p.cr.Fill()
}

// stop adds colour c to gradient g at offset at.
func stop(g *cairo.Pattern, at float64, c rgb) { g.AddColorStopRGB(at, c.r, c.g, c.b) }

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
