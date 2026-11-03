package main

import (
	"math"
	"strconv"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// The timeline is the whole piece as one strip: bars, how far you are, and
// the loop, with handles on its edges. The mouse handling is in loop.go.

const timelineMargin = 14.0

func (w *window) timelineX(pos float64, width int) float64 {
	return timelineMargin + pos/w.end*(float64(width)-2*timelineMargin)
}

func (w *window) drawTimeline(_ *gtk.DrawingArea, cr *cairo.Context, width, height int) {
	v := w.s.view()
	h := float64(height)
	mid := h/2 + 4

	cr.SetSourceRGB(0.08, 0.08, 0.10)
	cr.Paint()

	if from, to, on := w.shownLoop(); on {
		start, _ := w.barSpan(from)
		_, end := w.barSpan(to)
		x0, x1 := w.timelineX(start, width), w.timelineX(end, width)
		cr.SetSourceRGBA(0.65, 0.55, 0.98, 0.22)
		cr.Rectangle(x0, 2, x1-x0, h-4)
		cr.Fill()
		// Handles to drag.
		cr.SetSourceRGBA(0.65, 0.55, 0.98, 0.95)
		for _, x := range []float64{x0, x1} {
			cr.Rectangle(x-2, 2, 4, h-4)
			cr.Fill()
			cr.Arc(x, mid, 5, 0, 2*math.Pi)
			cr.Fill()
		}
	}

	// The track, and the part already played.
	left, right := w.timelineX(0, width), w.timelineX(w.end, width)
	cr.SetSourceRGB(0.22, 0.22, 0.27)
	cr.Rectangle(left, mid-3, right-left, 6)
	cr.Fill()
	now := w.timelineX(math.Max(0, math.Min(v.Pos, w.end)), width)
	cr.SetSourceRGB(0.49, 0.23, 0.93)
	cr.Rectangle(left, mid-3, now-left, 6)
	cr.Fill()

	// Bar lines, with numbers spaced so they don't collide.
	cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightNormal)
	every := 1
	for _, n := range []int{1, 2, 4, 5, 10, 20, 50} {
		every = n
		if float64(n)*(right-left)/float64(len(w.bars)) >= 28 {
			break
		}
	}
	for _, b := range w.bars {
		x := w.timelineX(b.Pos, width)
		cr.SetSourceRGBA(1, 1, 1, 0.25)
		cr.Rectangle(x, mid-6, 1, 12)
		cr.Fill()
		if b.Number%every == 0 || b.Number == w.bars[0].Number {
			label(cr, strconv.Itoa(b.Number), x, 10, 9, rgb{0.6, 0.6, 0.66})
		}
	}

	cr.SetSourceRGB(1, 1, 1)
	cr.Arc(now, mid, 6, 0, 2*math.Pi)
	cr.Fill()
}
