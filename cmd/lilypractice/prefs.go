package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// prefs are kept between sessions, in $XDG_CONFIG_HOME/lilypractice/prefs.json
// (~/.config/... if XDG_CONFIG_HOME isn't set).
type prefs struct {
	KeyLow, KeyHigh int // your keyboard's range
	ComputerKeys    int // note of the z key
	CountIn         int // bars, 0 for none
	Zoom            float64
	Columns         int    // pages side by side: 1 or 2
	PianoHeight     int    // pixels
	CursorStyle     string // an id from cursorStyles
	CursorColour    string // a name from cursorColours
	ClickSound      string // the metronome's: a name from clickSounds
	ClickVolume     int    // the metronome's, in percent
	IgnoreRepeats   bool   // play straight through, as written
	PedalYourself   bool   // the score's pedal marks aren't played
	HideSidebar     bool   // the list of scores
	SidebarWidth    int    // pixels
	TextSize        int    // of everything but the score and piano, in percent
}

// keyboard is a common keyboard size.
type keyboard struct {
	name      string
	low, high int
}

var keyboards = []keyboard{
	{"88 keys · A0–C8", 21, 108},
	{"76 keys · E1–G7", 28, 103},
	{"61 keys · C2–C7", 36, 96},
	{"49 keys · C2–C6", 36, 84},
	{"37 keys · C2–C5", 36, 72},
	{"25 keys · C3–C5", 48, 72},
}

func prefsPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "lilypractice", "prefs.json")
}

func loadPrefs() prefs {
	p := prefs{KeyLow: 21, KeyHigh: 108, ComputerKeys: 48, CountIn: 1, Zoom: 1.5, Columns: 1,
		PianoHeight: 140, CursorStyle: "column", CursorColour: "Violet",
		ClickSound: clickSounds[0].name, ClickVolume: 100, SidebarWidth: 240, TextSize: 100}
	if b, err := os.ReadFile(prefsPath()); err == nil {
		json.Unmarshal(b, &p)
	}
	p.ClickSound = clickSoundNamed(p.ClickSound).name
	p.ClickVolume = min(max(p.ClickVolume, 10), 140)
	p.SidebarWidth = min(max(p.SidebarWidth, 120), 600)
	p.TextSize = min(max(p.TextSize, minTextSize), maxTextSize)
	if p.KeyLow < 0 || p.KeyHigh > 127 || p.KeyHigh-p.KeyLow < 11 {
		p.KeyLow, p.KeyHigh = 21, 108
	}
	return p
}

func (p prefs) save() {
	path := prefsPath()
	if path == "" {
		return
	}
	os.MkdirAll(filepath.Dir(path), 0o755)
	if b, err := json.MarshalIndent(p, "", "  "); err == nil {
		os.WriteFile(path, b, 0o644)
	}
}
