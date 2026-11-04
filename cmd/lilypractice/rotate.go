package main

import (
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/turn"
)

// -rotate left (or right) shows the whole window turned a quarter turn,
// for a screen held on its side: a tablet showing this one through
// Sunshine and Moonlight, say, which then has the score on its tall side.
// Menus, panels and tooltips turn with it; GTK's folder dialog does not.

func (w *window) turned() bool { return w.rotate != "" }

// shownHeight is the window's height as you see it.
func (w *window) shownHeight() int {
	if w.turned() {
		return w.win.Width()
	}
	return w.win.Height()
}

// turnAll turns the popovers and tooltips of widget and all within it.
func (w *window) turnAll(widget gtk.Widgetter) {
	if !w.turned() {
		return
	}
	if p, ok := widget.(*gtk.Popover); ok {
		w.turnPopover(p)
	}
	w.turnTooltip(widget)
	for c := gtk.BaseWidget(widget).FirstChild(); c != nil; c = gtk.BaseWidget(c).NextSibling() {
		w.turnAll(c)
	}
}

// The sides of a popover, anticlockwise.
var sides = []gtk.PositionType{gtk.PosTop, gtk.PosLeft, gtk.PosBottom, gtk.PosRight}

// turnPopover turns what p shows, and has it open on the side it would
// open on as you see it.
func (w *window) turnPopover(p *gtk.Popover) {
	child := p.Child()
	if !w.turned() || child == nil || gtk.BaseWidget(child).CSSName() == "turn" {
		return
	}
	left := w.rotate == "left"
	for i, s := range sides {
		if s == p.Position() {
			step := 3
			if left {
				step = 1
			}
			p.SetPosition(sides[(i+step)%len(sides)])
			break
		}
	}
	// Lined up along the same edge, as you see it.
	h, v := p.HAlign(), p.VAlign()
	if left {
		p.SetHAlign(v)
		p.SetVAlign(flipAlign(h))
	} else {
		p.SetHAlign(flipAlign(v))
		p.SetVAlign(h)
	}
	p.SetChild(nil)
	p.SetChild(turn.New(child, left))
}

func flipAlign(a gtk.Align) gtk.Align {
	switch a {
	case gtk.AlignStart:
		return gtk.AlignEnd
	case gtk.AlignEnd:
		return gtk.AlignStart
	}
	return a
}

// turnTooltip has widget's tooltip, if it has or gets one, turned.
func (w *window) turnTooltip(widget gtk.Widgetter) {
	b := gtk.BaseWidget(widget)
	if !w.turned() || b.HasCSSClass("turned-tip") {
		return
	}
	b.AddCSSClass("turned-tip")
	b.ConnectQueryTooltip(func(_, _ int, _ bool, tip *gtk.Tooltip) bool {
		text := b.TooltipText()
		if text == "" {
			return false
		}
		if w.tip == nil {
			w.tipLabel = gtk.NewLabel("")
			w.tipLabel.SetWrap(true)
			w.tipLabel.SetMaxWidthChars(50)
			w.tip = turn.New(w.tipLabel, w.rotate == "left")
		}
		w.tipLabel.SetText(text)
		tip.SetCustom(w.tip)
		return true
	})
}
