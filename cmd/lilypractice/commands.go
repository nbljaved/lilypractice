package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/diamondburned/gotk4/pkg/cairo"
	glib "github.com/diamondburned/gotk4/pkg/core/glib"

	"lilypractice/midikeys"
	"lilypractice/practice"
)

// command is something to do that has a name, so that keys of a MIDI
// keyboard can do it (see package midikeys). Most have a computer key too.
type command struct {
	name, what string
	run        func(w *window)
}

var commands = []command{
	{"play-stop", "play / stop", (*window).togglePlay},
	{"start", "back to the start", (*window).restart},
	{"bar-back", "back a bar", func(w *window) { w.moveBars(-1) }},
	{"bar-forward", "forward a bar", func(w *window) { w.moveBars(1) }},
	{"note-back", "back a note", func(w *window) { w.stepNote(-1) }},
	{"note-forward", "forward a note", func(w *window) { w.stepNote(1) }},
	{"slower", "tempo −5%", func(w *window) { w.changeTempo(-0.05) }},
	{"faster", "tempo +5%", func(w *window) { w.changeTempo(0.05) }},
	{"metronome", "metronome on / off", func(w *window) { w.setMetronome(!w.s.settings().Metronome) }},
	{"count-in", "count-in on / off", (*window).toggleCountIn},
	{"loop", "loop on / off", (*window).toggleLoop},
	{"guide", "guide on / off", func(w *window) { w.setGuide(!w.s.settings().Guide) }},
	{"next-mode", "wait → play → listen", func(w *window) { w.setMode((w.s.settings().Mode + 1) % 3) }},
	{"wait-mode", "wait mode", func(w *window) { w.setMode(practice.Wait) }},
	{"play-mode", "play mode", func(w *window) { w.setMode(practice.Play) }},
	{"listen-mode", "listen mode", func(w *window) { w.setMode(practice.Listen) }},
	{"next-hand", "right → left → both hands", (*window).nextHand},
	{"right-hand", "right hand", func(w *window) { w.setHand(practice.Right) }},
	{"left-hand", "left hand", func(w *window) { w.setHand(practice.Left) }},
	{"both-hands", "both hands", func(w *window) { w.setHand(practice.Both) }},
	{"pages", "one or two pages", func(w *window) { w.setColumns(3 - max(w.prefs.Columns, 1)) }},
}

func findCommand(name string) (command, bool) {
	for _, c := range commands {
		if c.name == name {
			return c, true
		}
	}
	return command{}, false
}

func (w *window) runCommand(name string) {
	if c, ok := findCommand(name); ok && w.loaded() {
		c.run(w)
		w.dirty = true
	}
}

// moveBars goes n bars back or forward from the bar the music is in, to
// the start of that bar. While playing, the music goes on from there.
func (w *window) moveBars(n int) {
	v := w.s.view()
	i := 0
	for j, b := range w.bars {
		if b.Pos <= v.Pos+1e-9 {
			i = j
		}
	}
	i = min(max(i+n, 0), len(w.bars)-1)
	w.seekPos(w.bars[i].Pos)
	w.manualAt = time.Time{}
	w.follow(w.s.view(), false)
}

// nextHand goes right → left → both, skipping hands with nothing to play.
func (w *window) nextHand() {
	h := w.s.settings().Hand
	for range 3 {
		if h = (h + 1) % 3; w.s.playable(h) {
			w.setHand(h)
			return
		}
	}
}

// toggleCountIn turns the count-in off, or back on with as many bars as
// before.
func (w *window) toggleCountIn() {
	if bars := w.s.settings().CountIn; bars > 0 {
		w.countInWas = bars
		w.setCountIn(0)
	} else {
		w.setCountIn(max(w.countInWas, 1))
	}
}

func (w *window) toggleLoop() { w.switchLoop(!w.s.settings().Loop) }

// switchLoop turns the loop on or off; the first time, it loops four bars
// from the current one.
func (w *window) switchLoop(on bool) {
	set := w.s.settings()
	from, to := set.LoopFrom, set.LoopTo
	if on && !w.loopSet {
		from = w.s.view().Bar
		to = from + 3
	}
	w.setLoop(on, from, to)
}

// commandKeys keeps the MIDI keyboard's commands up to date with their
// file, $XDG_CONFIG_HOME/lilypractice/commands.json: it is read again when
// it changes.
type commandKeys struct {
	path    string
	modTime time.Time
	checked time.Time
	m       *midikeys.Map
	err     error // what was wrong with the file
	changed bool  // since the settings panel last showed them
}

func commandsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "lilypractice", "commands.json")
}

// watchCommandKeys reads the command keys and hands them to the session;
// checkCommandKeys, called every frame, reads them again after a change.
func (w *window) watchCommandKeys() {
	w.commandKeys.path = commandsPath()
	w.loadCommandKeys()
}

func (w *window) checkCommandKeys() {
	k := &w.commandKeys
	if time.Since(k.checked) < time.Second {
		return
	}
	k.checked = time.Now()
	if info, err := os.Stat(k.path); err == nil && !info.ModTime().Equal(k.modTime) {
		w.loadCommandKeys()
	}
}

func (w *window) loadCommandKeys() {
	k := &w.commandKeys
	if k.path == "" {
		return
	}
	c, err := midikeys.Load(k.path)
	if info, statErr := os.Stat(k.path); statErr == nil {
		k.modTime = info.ModTime()
	}
	k.checked = time.Now()
	if err == nil {
		k.m, err = midikeys.New(c, func(name string) bool {
			_, ok := findCommand(name)
			return ok
		})
	}
	k.err, k.changed = err, true
	if err != nil {
		fmt.Fprintln(os.Stderr, "lilypractice: command keys:", err)
	}
	if k.m == nil {
		return
	}
	w.s.setCommandKeys(k.m, func(name string) {
		glib.IdleAdd(func() { w.runCommand(name) })
	})
}

// commandColour marks the keys that do something in command mode.
var commandColour = rgb{0.20, 0.78, 0.66}

// commandWhat says what a key does in command mode, for showing.
func commandWhat(b midikeys.Binding) string {
	switch b.Command {
	case midikeys.Leave:
		return "leave command mode"
	case "":
		return "…" // more keys to come
	}
	what := b.Command
	if c, ok := findCommand(b.Command); ok {
		what = c.what
	}
	if b.Stay {
		what += " ↻"
	}
	return what
}

// drawCommandMode shows, over the bottom of the score, that the MIDI
// keyboard is in command mode and what its keys do now.
func (w *window) drawCommandMode(cr *cairo.Context, width, height int) {
	scale := w.textScale()
	size, line, pad, gap := 14*scale, 21*scale, 16*scale, 28*scale
	title := "Command mode · " + w.commandPath
	foot := "↻ keeps command mode on. Any other key leaves it."
	var keys, whats []string
	for _, b := range w.commandNext {
		keys = append(keys, b.Keys)
		whats = append(whats, commandWhat(b))
	}
	widest := func(texts []string) float64 {
		wd := 0.0
		for _, t := range texts {
			wd = math.Max(wd, cr.TextExtents(t).XAdvance)
		}
		return wd
	}
	cr.SetFontSize(size)
	cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightBold)
	keysW, titleW := widest(keys), widest([]string{title})
	cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, cairo.FontWeightNormal)
	whatW, footW := widest(whats), widest([]string{foot})

	columns := 1
	if len(keys) > 6 {
		columns = 2
	}
	rows := (len(keys) + columns - 1) / columns
	colW := keysW + size + whatW
	boxW := math.Max(float64(columns)*colW+float64(columns-1)*gap, math.Max(titleW, footW)) + 2*pad
	boxH := 2*pad + size + line*float64(rows+2)
	x := (float64(width) - boxW) / 2
	y := math.Max(8, float64(height)-boxH-16*scale)

	roundedRect(cr, x, y, boxW, boxH, 10*scale)
	cr.SetSourceRGBA(0.08, 0.08, 0.10, 0.9)
	cr.FillPreserve()
	cr.SetSourceRGB(commandColour.r, commandColour.g, commandColour.b)
	cr.SetLineWidth(2)
	cr.Stroke()

	base := y + pad + size // the title's baseline
	text := func(t string, tx, ty float64, weight cairo.FontWeight, c rgb) {
		cr.SelectFontFace("sans-serif", cairo.FontSlantNormal, weight)
		cr.SetSourceRGB(c.r, c.g, c.b)
		cr.MoveTo(tx, ty)
		cr.ShowText(t)
	}
	text(title, x+pad, base, cairo.FontWeightBold, rgb{1, 1, 1})
	for i := range keys {
		tx := x + pad + float64(i/rows)*(colW+gap)
		ty := base + line*(1.5+float64(i%rows))
		text(keys[i], tx, ty, cairo.FontWeightBold, commandColour)
		text(whats[i], tx+keysW+size, ty, cairo.FontWeightNormal, rgb{0.86, 0.86, 0.9})
	}
	text(foot, x+pad, base+line*(float64(rows)+1.8), cairo.FontWeightNormal, rgb{0.6, 0.6, 0.66})
}
