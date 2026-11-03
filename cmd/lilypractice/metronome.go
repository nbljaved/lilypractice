package main

import (
	"fmt"
	"time"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/practice"
)

// clickSound is a sound for the metronome and the count-in: General MIDI
// percussion notes for the first beat of a bar and for the others. Any
// GM soundfont has them; FluidR3 has all of these.
type clickSound struct {
	name         string
	accent, beat int
}

var clickSounds = []clickSound{
	{"Wood block", 76, 77},
	{"Metronome", 34, 33}, // a bell, then clicks
	{"Claves", 75, 75},
	{"Side stick", 37, 37},
	{"Cowbell", 56, 56},
	{"Hi-hat", 46, 42}, // open, then closed
	{"Triangle", 81, 80},
	{"Hand clap", 39, 39},
	{"Sticks", 31, 31},
}

// clickSoundNamed finds a sound by name, or gives the first.
func clickSoundNamed(name string) clickSound {
	for _, c := range clickSounds {
		if c.name == name {
			return c
		}
	}
	return clickSounds[0]
}

// setClick changes the metronome's sound and volume (in percent) and, if
// the music is stopped, plays it once so you hear it.
func (w *window) setClick(name string, volume int) {
	c := clickSoundNamed(name)
	volume = min(max(volume, 10), 140)
	w.do(func(time.Duration) []practice.Out {
		w.s.eng.SetClick(c.accent, c.beat, float64(volume)/100)
		return nil
	})
	w.prefs.ClickSound, w.prefs.ClickVolume = c.name, volume
	w.prefsChanged = true
	if !w.s.view().Playing {
		w.s.previewClick(c.accent, c.beat, float64(volume)/100)
	}
}

// addMetronomeMenu gives the metronome button a right-click menu of its
// sounds and volume.
func (w *window) addMetronomeMenu(button *gtk.ToggleButton) {
	right := gtk.NewGestureClick()
	right.SetButton(gdk.BUTTON_SECONDARY)
	right.ConnectPressed(func(_ int, x, y float64) {
		var items []menuItem
		for _, c := range clickSounds {
			items = append(items, menuItem{label: c.name, run: func() { w.setClick(c.name, w.prefs.ClickVolume) },
				choice: true, chosen: c.name == w.prefs.ClickSound})
		}
		items = append(items, menuItem{},
			menuItem{label: fmt.Sprintf("Louder (now %d%%)", w.prefs.ClickVolume), run: func() { w.setClick(w.prefs.ClickSound, w.prefs.ClickVolume+10) }},
			menuItem{label: "Softer", run: func() { w.setClick(w.prefs.ClickSound, w.prefs.ClickVolume-10) }})
		w.popupMenu(button, x, y, items)
	})
	button.AddController(right)
}

// previewClick plays a bar's first beat and another on click-out.
func (s *session) previewClick(accent, beat int, volume float64) {
	if s.seq == nil {
		return
	}
	go func() {
		for i, p := range []int{accent, beat} {
			if i > 0 {
				time.Sleep(350 * time.Millisecond)
			}
			s.seq.NoteOn(s.clickOut, practice.ClickChannel, p, practice.ClickVelocity(i == 0, volume))
			time.Sleep(50 * time.Millisecond)
			s.seq.NoteOff(s.clickOut, practice.ClickChannel, p)
		}
	}()
}
