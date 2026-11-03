// Package view draws the compiled score pages, laid out one above another
// or side by side, with chosen notes recoloured.
//
// Positions are in "page units": CSS pixels at 100% zoom, with the pages
// placed in the layout and Gap units between them.
package view

import (
	"math"

	"github.com/diamondburned/gotk4/pkg/cairo"

	"lilypractice/lilypond"
	"lilypractice/rsvg"
	"lilypractice/score"
)

const Gap = 16

type Color struct{ R, G, B float64 }

var (
	Blue  = Color{0.10, 0.35, 0.95}
	Green = Color{0.05, 0.60, 0.20}
	Red   = Color{0.85, 0.10, 0.10}
)

type Pages struct {
	docs       []*rsvg.Doc
	lefts      []float64 // where each page is in the layout
	tops       []float64
	notePage   map[string]int
	noteSystem map[string]int
	staves     []lilypond.Staff

	// Filled on demand; positions depend on the layout.
	boxes   map[string]rsvg.Rect // note heads
	systems map[int]rsvg.Rect    // lines of music

	// Each page rendered once per zoom level; recolouring a note only draws
	// that note on top.
	cacheZoom float64
	cache     []*cairo.Surface
}

func Open(out *lilypond.Output) (*Pages, error) {
	p := &Pages{notePage: out.NotePage, noteSystem: out.NoteSystem, staves: out.Staves}
	for _, path := range out.Pages {
		d, err := rsvg.Open(path)
		if err != nil {
			p.Close()
			return nil, err
		}
		p.docs = append(p.docs, d)
	}
	p.SetColumns(1)
	return p, nil
}

func (p *Pages) Close() {
	for _, d := range p.docs {
		d.Close()
	}
	p.docs = nil
}

// SetColumns lays the pages out in rows of this many, side by side.
func (p *Pages) SetColumns(columns int) {
	columns = max(columns, 1)
	p.lefts = make([]float64, len(p.docs))
	p.tops = make([]float64, len(p.docs))
	top := 0.0
	for row := 0; row*columns < len(p.docs); row++ {
		left, height := 0.0, 0.0
		for i := row * columns; i < min((row+1)*columns, len(p.docs)); i++ {
			p.lefts[i], p.tops[i] = left, top
			left += p.docs[i].Width + Gap
			height = math.Max(height, p.docs[i].Height)
		}
		top += height + Gap
	}
	p.boxes = make(map[string]rsvg.Rect)
	p.systems = make(map[int]rsvg.Rect)
}

// Size of the whole layout, in page units.
func (p *Pages) Size() (w, h float64) {
	for i, d := range p.docs {
		w = math.Max(w, p.lefts[i]+d.Width)
		h = math.Max(h, p.tops[i]+d.Height)
	}
	return w, h
}

// PageSpan returns the top and height of page i, in page units.
func (p *Pages) PageSpan(i int) (top, height float64) {
	return p.tops[i], p.docs[i].Height
}

// place moves a box from page i's own coordinates into the layout.
func (p *Pages) place(r rsvg.Rect, i int) rsvg.Rect {
	r.X += p.lefts[i]
	r.Y += p.tops[i]
	return r
}

// NoteBox returns the box of a note head, in page units. The strikes of a
// tremolo share their note's head.
func (p *Pages) NoteBox(id string) (rsvg.Rect, bool) {
	id = score.Head(id)
	if r, ok := p.boxes[id]; ok {
		return r, true
	}
	i, ok := p.notePage[id]
	if !ok {
		return rsvg.Rect{}, false
	}
	r, err := p.docs[i].Bounds(id, p.docs[i].Viewport())
	if err != nil {
		return rsvg.Rect{}, false
	}
	r = p.place(r, i)
	p.boxes[id] = r
	return r, true
}

// Union returns the box around several note heads, in page units.
func (p *Pages) Union(ids []string) (rsvg.Rect, bool) {
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	found := false
	for _, id := range ids {
		r, ok := p.NoteBox(id)
		if !ok {
			continue
		}
		found = true
		x0, y0 = math.Min(x0, r.X), math.Min(y0, r.Y)
		x1, y1 = math.Max(x1, r.X+r.W), math.Max(y1, r.Y+r.H)
	}
	return rsvg.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, found
}

// System is the line of music (system) a note is on, or -1.
func (p *Pages) System(id string) int {
	if s, ok := p.noteSystem[score.Head(id)]; ok {
		return s
	}
	return -1
}

// SystemBox is the box around the staves of a line of music, in page
// units: from the top line of its top staff to the bottom line of its
// bottom staff.
func (p *Pages) SystemBox(system int) (rsvg.Rect, bool) {
	if r, ok := p.systems[system]; ok {
		return r, true
	}
	x0, y0 := math.Inf(1), math.Inf(1)
	x1, y1 := math.Inf(-1), math.Inf(-1)
	found := false
	for _, st := range p.staves {
		if st.System != system {
			continue
		}
		r, err := p.docs[st.Page].Bounds(st.ID, p.docs[st.Page].Viewport())
		if err != nil {
			continue
		}
		r = p.place(r, st.Page)
		found = true
		x0, y0 = math.Min(x0, r.X), math.Min(y0, r.Y)
		x1, y1 = math.Max(x1, r.X+r.W), math.Max(y1, r.Y+r.H)
	}
	if !found {
		return rsvg.Rect{}, false
	}
	r := rsvg.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	p.systems[system] = r
	return r, true
}

// Draw paints the pages at this zoom, then the highlighted notes in colour.
// cr is in device pixels with (0, 0) at the top left of the layout.
func (p *Pages) Draw(cr *cairo.Context, zoom float64, highlight map[string]Color) {
	if zoom != p.cacheZoom {
		p.cache = make([]*cairo.Surface, len(p.docs))
		p.cacheZoom = zoom
	}
	clipLeft, clipTop, clipRight, clipBottom := cr.ClipExtents()
	for i, d := range p.docs {
		left, top := p.lefts[i]*zoom, p.tops[i]*zoom
		if top+d.Height*zoom < clipTop || top > clipBottom || left+d.Width*zoom < clipLeft || left > clipRight {
			continue
		}
		if p.cache[i] == nil {
			p.cache[i] = renderPage(d, zoom)
		}
		cr.SetSourceSurface(p.cache[i], left, top)
		cr.Paint()
	}
	for id, c := range highlight {
		id = score.Head(id)
		i, ok := p.notePage[id]
		if !ok {
			continue
		}
		// Only notes in view: drawing one looks it up in the page, which is slow.
		if r, ok := p.NoteBox(id); !ok || r.X*zoom > clipRight || (r.X+r.W)*zoom < clipLeft ||
			r.Y*zoom > clipBottom || (r.Y+r.H)*zoom < clipTop {
			continue
		}
		cr.Save()
		cr.Translate(p.lefts[i]*zoom, p.tops[i]*zoom)
		cr.Scale(zoom, zoom)
		p.docs[i].RenderTinted(cr, id, p.docs[i].Viewport(), c.R, c.G, c.B)
		cr.Restore()
	}
}

func renderPage(d *rsvg.Doc, zoom float64) *cairo.Surface {
	w, h := int(math.Ceil(d.Width*zoom)), int(math.Ceil(d.Height*zoom))
	s := cairo.CreateImageSurface(cairo.FormatARGB32, w, h)
	cr := cairo.Create(s)
	cr.SetSourceRGB(1, 1, 1)
	cr.Paint()
	cr.Scale(zoom, zoom)
	d.Render(cr, d.Viewport())
	cr.Close()
	return s
}

// DrawCursor draws a vertical line through a group of note heads, reaching a
// little above and below them. (lprender uses it; the program has its own.)
func DrawCursor(cr *cairo.Context, zoom float64, box rsvg.Rect, c Color) {
	const reach = 6 // page units above and below the notes
	x := (box.X + box.W/2) * zoom
	cr.Save()
	cr.SetSourceRGBA(c.R, c.G, c.B, 0.35)
	cr.SetLineWidth(math.Max(2, 1.2*zoom))
	cr.MoveTo(x, (box.Y-reach)*zoom)
	cr.LineTo(x, (box.Y+box.H+reach)*zoom)
	cr.Stroke()
	cr.Restore()
}
