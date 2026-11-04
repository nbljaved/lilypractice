package main

import (
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/practice"
)

// controls are the widgets around the score. The engine owns the state;
// refresh copies it into the widgets every frame, with handlers ignoring
// the changes it makes.
type controls struct {
	updating bool

	sidebar        *gtk.ToggleButton
	play           *gtk.Button
	position       *gtk.Label
	state          *gtk.Label
	modes          [3]*gtk.ToggleButton
	hands          [3]*gtk.ToggleButton
	tempo          *gtk.Label
	metronome      *gtk.ToggleButton
	loop           *gtk.MenuButton
	loopOn         *gtk.Switch
	loopFrom       *gtk.Label
	loopTo         *gtk.Label
	stats          *gtk.MenuButton
	accuracy       *gtk.Label
	statCounts     [4]*gtk.Label // perfect, good, missed, wrong
	streak         *gtk.Label
	countIn        *gtk.Label
	clickSound     *gtk.DropDown
	clickVolume    *gtk.Label
	layout         *gtk.DropDown
	pianoHeight    *gtk.Label
	textSize       *gtk.Label
	cursorStyle    *gtk.DropDown
	cursorColour   *gtk.DropDown
	guide          *gtk.Switch
	repeats        *gtk.Switch
	pedalMarks     *gtk.Switch
	anyOctave      *gtk.Switch
	keysFrom       *gtk.Label
	zoom           *gtk.Label
	instrument     *gtk.Label
	pedal          *gtk.Label
	lastPedal      bool
	scorePedal     *gtk.Label
	lastScorePedal bool
	keyboardSize   *gtk.DropDown
	keyboardRange  *gtk.Label
	detect         *gtk.Button
	commandInfo    *gtk.Label
	commandList    *gtk.Box // holds the grid of command keys
	lastHandTip    [3]string
	lastText       map[*gtk.Label]string
	lastPlayIcon   string
	lastLoopButton string
	lastChip       string
}

func (w *window) buildHeader() *gtk.HeaderBar {
	c := &w.c
	header := gtk.NewHeaderBar()
	w.header = header
	w.titleLabel = gtk.NewLabel("")
	w.titleLabel.AddCSSClass("title")
	header.SetTitleWidget(w.titleLabel)

	c.sidebar = gtk.NewToggleButton()
	c.sidebar.SetIconName("sidebar-show-symbolic")
	c.sidebar.SetTooltipText("The scores in this folder (F9)")
	c.sidebar.SetActive(!w.prefs.HideSidebar)
	c.sidebar.ConnectToggled(func() {
		if on := c.sidebar.Active(); on != w.sidebarShown() {
			w.setSidebar(on)
		}
	})
	header.PackStart(c.sidebar)
	c.stats = gtk.NewMenuButton()
	c.stats.AddCSSClass("chip")
	c.stats.SetTooltipText("Session stats")
	c.stats.SetPopover(w.buildStatsPanel())
	header.PackEnd(w.menuButton("open-menu-symbolic", "Settings", w.buildSettingsPanel()))
	header.PackEnd(w.menuButton("input-keyboard-symbolic", "Keys and MIDI command keys", w.buildKeysPanel()))
	header.PackEnd(c.stats)
	return header
}

// showScoreChoice puts a list of the file's scores in the header, if it
// has several.
func (w *window) showScoreChoice() {
	if w.scorePick != nil {
		w.header.Remove(w.scorePick)
		w.scorePick = nil
	}
	if len(w.scores) < 2 {
		return
	}
	var names []string
	for i := range w.scores {
		names = append(names, fmt.Sprintf("Score %d of %d", i+1, len(w.scores)))
	}
	w.c.updating = true // not a choice of yours
	w.scorePick = w.choice(names, w.chooseScore)
	w.scorePick.SetSelected(uint(w.score))
	w.c.updating = false
	w.scorePick.SetTooltipText("This file holds several scores (movements, or pieces of a set): the one to practise")
	w.header.PackStart(w.scorePick)
	w.turnAll(w.scorePick)
}

func (w *window) buildTransport() *gtk.CenterBox {
	c := &w.c
	bar := gtk.NewCenterBox()
	bar.AddCSSClass("transport")

	// Left: restart, play, where you are.
	start := gtk.NewBox(gtk.OrientationHorizontal, 10)
	restart := gtk.NewButtonFromIconName("media-skip-backward-symbolic")
	restart.AddCSSClass("round")
	restart.SetTooltipText("Back to the start (Home)")
	restart.ConnectClicked(w.restart)
	c.play = gtk.NewButtonFromIconName("media-playback-start-symbolic")
	c.play.AddCSSClass("play-button")
	c.play.SetTooltipText("Play / stop (Space)")
	c.play.ConnectClicked(w.togglePlay)
	where := gtk.NewBox(gtk.OrientationVertical, 0)
	where.SetVAlign(gtk.AlignCenter)
	c.position = gtk.NewLabel("")
	c.position.AddCSSClass("position")
	c.position.SetXAlign(0)
	c.state = gtk.NewLabel("")
	c.state.AddCSSClass("state")
	c.state.SetXAlign(0)
	where.Append(c.position)
	where.Append(c.state)
	where.SetTooltipText("Scroll to move a bar")
	wheelSteps(where, w.moveBars)
	start.Append(restart)
	start.Append(c.play)
	start.Append(where)
	// Your pedal, and below it the score's, if it marks one.
	pedals := gtk.NewBox(gtk.OrientationVertical, 2)
	pedals.SetVAlign(gtk.AlignCenter)
	c.pedal = gtk.NewLabel("Pedal")
	c.pedal.AddCSSClass("pedal-indicator")
	c.pedal.SetTooltipText("Your sustain pedal: a MIDI keyboard's, or hold Shift")
	c.scorePedal = gtk.NewLabel("Score")
	c.scorePedal.AddCSSClass("pedal-indicator")
	c.scorePedal.AddCSSClass("score")
	c.scorePedal.SetTooltipText("The score's sustain pedal: played for you, or for you to follow with Play pedal marks off (settings)")
	c.scorePedal.SetVisible(false)
	pedals.Append(c.pedal)
	pedals.Append(c.scorePedal)
	start.Append(pedals)
	bar.SetStartWidget(start)

	// Middle: mode and hands.
	middle := gtk.NewBox(gtk.OrientationHorizontal, 18)
	modeNames := []string{"Wait", "Play", "Listen"}
	modeTips := []string{
		"Wait: the music stops until you play your notes (F2, Ctrl+1)",
		"Play: the music keeps time and scores your notes (F2, Ctrl+2)",
		"Listen: the program plays the hands you choose (F2, Ctrl+3)",
	}
	middle.Append(w.segmented(c.modes[:], modeNames, modeTips, func(i int) { w.setMode(practice.Mode(i)) }))
	handNames := []string{"Right", "Left", "Both"}
	middle.Append(w.segmented(c.hands[:], handNames, handTips, func(i int) { w.setHand(practice.Hand(i)) }))
	bar.SetCenterWidget(middle)

	// Right: tempo, loop, metronome.
	end := gtk.NewBox(gtk.OrientationHorizontal, 10)
	c.tempo = gtk.NewLabel("")
	c.tempo.AddCSSClass("tempo")
	tempo := w.stepper(c.tempo, "Tempo (↑ ↓, or scroll)", func() { w.changeTempo(-0.05) }, func() { w.changeTempo(0.05) })
	wheelSteps(tempo, func(n int) { w.changeTempo(-0.05 * float64(n)) })
	end.Append(tempo)
	c.loop = gtk.NewMenuButton()
	c.loop.SetTooltipText("Loop some bars (F6; shift-click the timeline)")
	c.loop.SetPopover(w.buildLoopPanel())
	end.Append(c.loop)
	c.metronome = gtk.NewToggleButtonWithLabel("Metronome")
	c.metronome.AddCSSClass("pill")
	c.metronome.SetTooltipText("Metronome (F3); right-click for its sound")
	c.metronome.ConnectToggled(func() {
		if !c.updating {
			w.setMetronome(c.metronome.Active())
		}
	})
	w.addMetronomeMenu(c.metronome)
	end.Append(c.metronome)
	bar.SetEndWidget(end)
	return bar
}

var (
	handTips   = []string{"You play the right hand (Tab, Ctrl+R)", "You play the left hand (Tab, Ctrl+L)", "You play both hands (Tab, Ctrl+B)"}
	listenTips = []string{"Hear only the right hand (Tab, Ctrl+R)", "Hear only the left hand (Tab, Ctrl+L)", "Hear both hands (Tab, Ctrl+B)"}
)

// segmented makes a row of joined buttons of which one is chosen.
func (w *window) segmented(buttons []*gtk.ToggleButton, names, tips []string, choose func(int)) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.AddCSSClass("linked")
	box.AddCSSClass("segmented")
	for i, name := range names {
		b := gtk.NewToggleButtonWithLabel(name)
		b.SetTooltipText(tips[i])
		if i > 0 {
			b.SetGroup(buttons[0])
		}
		b.ConnectToggled(func() {
			if !w.c.updating && b.Active() {
				choose(i)
			}
		})
		buttons[i] = b
		box.Append(b)
	}
	return box
}

// stepper is a value with − and + buttons.
func (w *window) stepper(value *gtk.Label, tip string, down, up func()) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 0)
	box.AddCSSClass("linked")
	minus := gtk.NewButtonFromIconName("list-remove-symbolic")
	minus.ConnectClicked(down)
	plus := gtk.NewButtonFromIconName("list-add-symbolic")
	plus.ConnectClicked(up)
	value.SetTooltipText(tip)
	box.Append(minus)
	box.Append(value)
	box.Append(plus)
	return box
}

func (w *window) menuButton(icon, tip string, popover *gtk.Popover) *gtk.MenuButton {
	b := gtk.NewMenuButton()
	b.SetIconName(icon)
	b.SetTooltipText(tip)
	b.SetPopover(popover)
	return b
}

// row is one setting: a title and explanation on the left, a control on
// the right.
func row(title, subtitle string, control gtk.Widgetter) *gtk.Box {
	box := gtk.NewBox(gtk.OrientationHorizontal, 16)
	text := gtk.NewBox(gtk.OrientationVertical, 1)
	text.SetHExpand(true)
	t := gtk.NewLabel(title)
	t.SetXAlign(0)
	text.Append(t)
	if subtitle != "" {
		s := gtk.NewLabel(subtitle)
		s.SetXAlign(0)
		s.AddCSSClass("subtitle")
		s.SetWrap(true)
		s.SetMaxWidthChars(40)
		text.Append(s)
	}
	box.Append(text)
	if sw, ok := control.(*gtk.Switch); ok {
		sw.SetVAlign(gtk.AlignCenter)
	}
	box.Append(control)
	return box
}

func sectionTitle(text string) *gtk.Label {
	l := gtk.NewLabel(text)
	l.SetXAlign(0)
	l.AddCSSClass("section-title")
	return l
}

// toggle makes a switch that calls set when you flip it.
func (w *window) toggle(set func(bool)) *gtk.Switch {
	sw := gtk.NewSwitch()
	sw.ConnectStateSet(func(on bool) bool {
		if !w.c.updating {
			set(on)
		}
		return false
	})
	return sw
}

// panel makes a pop-up panel of settings; Esc or Ctrl+G closes it. It
// scrolls if it is taller than the window.
func (w *window) panel(children ...gtk.Widgetter) *gtk.Popover {
	box := gtk.NewBox(gtk.OrientationVertical, 8)
	box.AddCSSClass("panel")
	for _, ch := range children {
		box.Append(ch)
	}
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetPropagateNaturalWidth(true)
	scroll.SetPropagateNaturalHeight(true)
	scroll.SetChild(box)
	p := gtk.NewPopover()
	p.SetChild(scroll)
	p.ConnectShow(func() { scroll.SetMaxContentHeight(max(240, w.shownHeight()-150)) })
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		if keyval == gdk.KEY_Escape || keyval == gdk.KEY_g && state&gdk.ControlMask != 0 {
			p.Popdown()
			return true
		}
		return false
	})
	p.AddController(keys)
	w.popovers = append(w.popovers, p)
	return p
}

// choice is a drop-down list that calls choose with the index picked.
func (w *window) choice(names []string, choose func(int)) *gtk.DropDown {
	d := gtk.NewDropDownFromStrings(names)
	d.SetVAlign(gtk.AlignCenter)
	d.NotifyProperty("selected", func() {
		if i := d.Selected(); !w.c.updating && int(i) < len(names) {
			choose(int(i))
		}
	})
	return d
}

func (w *window) buildLoopPanel() *gtk.Popover {
	c := &w.c
	c.loopOn = w.toggle(w.switchLoop)
	c.loopFrom = gtk.NewLabel("")
	c.loopTo = gtk.NewLabel("")
	c.loopFrom.SetWidthChars(4)
	c.loopTo.SetWidthChars(4)
	move := func(dFrom, dTo int) func() {
		return func() {
			set := w.s.settings()
			w.setLoop(true, set.LoopFrom+dFrom, max(set.LoopTo+dTo, set.LoopFrom+dFrom))
		}
	}
	here := gtk.NewButtonWithLabel("Start the loop at the current bar")
	here.ConnectClicked(func() {
		set := w.s.settings()
		bar := w.s.view().Bar
		w.setLoop(true, bar, max(set.LoopTo, bar))
	})
	return w.panel(
		row("Loop", "Repeat these bars until you stop", c.loopOn),
		row("From bar", "", w.stepper(c.loopFrom, "First bar of the loop", move(-1, 0), move(1, 0))),
		row("To bar", "", w.stepper(c.loopTo, "Last bar of the loop", move(0, -1), move(0, 1))),
		here,
	)
}

// buildKeyboardChooser picks your keyboard's size from the common ones, or
// detects it from its lowest and highest keys.
func (w *window) buildKeyboardChooser() *gtk.Box {
	c := &w.c
	var names []string
	for _, k := range keyboards {
		names = append(names, k.name)
	}
	c.keyboardSize = gtk.NewDropDownFromStrings(names)
	c.keyboardSize.SetVAlign(gtk.AlignCenter)
	c.keyboardSize.NotifyProperty("selected", func() {
		if i := c.keyboardSize.Selected(); !c.updating && int(i) < len(keyboards) {
			w.setKeyboard(keyboards[i].low, keyboards[i].high)
		}
	})
	c.detect = gtk.NewButtonWithLabel("Detect")
	c.detect.SetVAlign(gtk.AlignCenter)
	c.detect.SetTooltipText("Press the lowest, then the highest key of your MIDI keyboard")
	c.detect.ConnectClicked(func() {
		if w.s.detectStep() != 0 {
			w.s.cancelDetect()
		} else {
			w.s.startDetect()
		}
	})
	c.keyboardRange = gtk.NewLabel("")
	c.keyboardRange.SetXAlign(0)
	c.keyboardRange.AddCSSClass("subtitle")
	box := gtk.NewBox(gtk.OrientationHorizontal, 6)
	box.Append(c.keyboardSize)
	box.Append(c.detect)
	return box
}

func (w *window) buildSettingsPanel() *gtk.Popover {
	c := &w.c
	c.countIn = gtk.NewLabel("")
	c.countIn.SetWidthChars(7)
	var sounds []string
	for _, cs := range clickSounds {
		sounds = append(sounds, cs.name)
	}
	c.clickSound = w.choice(sounds, func(i int) { w.setClick(clickSounds[i].name, w.prefs.ClickVolume) })
	c.clickVolume = gtk.NewLabel("")
	c.clickVolume.SetWidthChars(5)
	c.pianoHeight = gtk.NewLabel("")
	c.pianoHeight.SetWidthChars(6)
	c.layout = w.choice([]string{"One page", "Two pages"}, func(i int) { w.setColumns(i + 1) })
	var styles, colours []string
	for _, s := range cursorStyles {
		styles = append(styles, s.name)
	}
	for _, c := range cursorColours {
		colours = append(colours, c.name)
	}
	c.cursorStyle = w.choice(styles, func(i int) { w.setCursor(cursorStyles[i].id, w.prefs.CursorColour) })
	c.cursorColour = w.choice(colours, func(i int) { w.setCursor(w.prefs.CursorStyle, cursorColours[i].name) })
	c.guide = w.toggle(w.setGuide)
	c.repeats = w.toggle(w.setFollowRepeats)
	c.pedalMarks = w.toggle(w.setPedalMarks)
	c.anyOctave = w.toggle(w.setAnyOctave)
	c.keysFrom = gtk.NewLabel("")
	c.keysFrom.SetWidthChars(4)
	c.zoom = gtk.NewLabel("")
	c.zoom.SetWidthChars(5)
	c.textSize = gtk.NewLabel("")
	c.textSize.SetWidthChars(5)
	c.instrument = gtk.NewLabel("")
	c.instrument.SetXAlign(0)
	c.instrument.SetWrap(true)
	c.instrument.SetMaxWidthChars(48)
	c.instrument.AddCSSClass("subtitle")

	midi := gtk.NewLabel("Connect a MIDI keyboard to " + w.s.name + ":keyboard-in in qpwgraph, or start with -in NAME.")
	midi.SetXAlign(0)
	midi.SetWrap(true)
	midi.SetMaxWidthChars(48)
	midi.AddCSSClass("subtitle")

	return w.panel(
		sectionTitle("Playing"),
		row("Count-in", "Clicks before the music starts (F5)", w.stepper(c.countIn, "Bars of count-in",
			func() { w.setCountIn(w.prefs.CountIn - 1) }, func() { w.setCountIn(w.prefs.CountIn + 1) })),
		row("Guide", "In play mode, also hear your part, quietly", c.guide),
		row("Follow repeats", "Go back at repeat signs and take the endings in turn, and D.S. al Fine; loops play their bars as written", c.repeats),
		row("Play pedal marks", "The score's pedals; off to pedal yourself, following the Score light", c.pedalMarks),
		row("Metronome sound", "Also for the count-in; F3 turns it on", c.clickSound),
		row("Metronome volume", "", w.stepper(c.clickVolume, "Metronome volume",
			func() { w.setClick(w.prefs.ClickSound, w.prefs.ClickVolume-10) },
			func() { w.setClick(w.prefs.ClickSound, w.prefs.ClickVolume+10) })),
		sectionTitle("Your keyboard"),
		row("Size", "The piano below shows these keys; in wait and play mode, notes outside them are played for you", w.buildKeyboardChooser()),
		c.keyboardRange,
		sectionTitle("Computer keyboard"),
		row("Any octave counts", "Two rows can't reach the whole piano", c.anyOctave),
		row("Lowest key (z)", "PgUp / PgDn", w.stepper(c.keysFrom, "Note of the z key",
			func() { w.setKeyBase(w.keyBase - 12) }, func() { w.setKeyBase(w.keyBase + 12) })),
		sectionTitle("View"),
		row("Pages", "One above another, or two side by side (F8)", c.layout),
		row("Cursor", "How the place in the music is shown", c.cursorStyle),
		row("Cursor colour", "", c.cursorColour),
		row("Piano height", "Or drag the line above the piano", w.stepper(c.pianoHeight, "Piano height",
			func() { w.setPianoHeight(w.prefs.PianoHeight - 20) }, func() { w.setPianoHeight(w.prefs.PianoHeight + 20) })),
		row("Zoom", "Ctrl + / Ctrl −", w.stepper(c.zoom, "Zoom",
			func() { w.setZoom(w.zoom / 1.2) }, func() { w.setZoom(w.zoom * 1.2) })),
		row("Text size", "Of the list of scores, panels, menus and buttons", w.stepper(c.textSize, "Text size",
			func() { w.setTextSize(w.prefs.TextSize - 10) }, func() { w.setTextSize(w.prefs.TextSize + 10) })),
		sectionTitle("Sound"),
		c.instrument,
		midi,
	)
}

// keyGrid lays out pairs of a key and what it does, in two columns.
func keyGrid(pairs [][2]string) *gtk.Grid {
	g := gtk.NewGrid()
	g.SetColumnSpacing(14)
	g.SetRowSpacing(3)
	for i, k := range pairs {
		name := gtk.NewLabel(k[0])
		name.SetXAlign(1)
		name.AddCSSClass("key-name")
		what := gtk.NewLabel(k[1])
		what.SetXAlign(0)
		what.AddCSSClass("subtitle")
		g.Attach(name, (i%2)*2, i/2, 1, 1)
		g.Attach(what, (i%2)*2+1, i/2, 1, 1)
	}
	return g
}

// buildKeysPanel lists the computer keys, and the keys of a MIDI keyboard
// that are commands.
func (w *window) buildKeysPanel() *gtk.Popover {
	c := &w.c
	keys := keyGrid([][2]string{
		{"Space", "play / stop"}, {"Home", "back to the start"},
		{"Ctrl+← →", "back / forward a bar"}, {"← →", "back / forward a note"},
		{"F2", "wait / play / listen"}, {"Ctrl+1 2 3", "wait, play, listen"},
		{"Tab", "right / left / both hands"}, {"Ctrl+R L B", "right, left, both hands"},
		{"↑ ↓", "tempo"}, {"F3", "metronome"},
		{"F5", "count-in on / off"}, {"F6", "loop"},
		{"F7", "guide"}, {"F4", "any octave"},
		{"F8", "one or two pages"}, {"Esc, Ctrl+G", "close this panel"},
		{"PgUp PgDn", "computer keys octave"}, {"Ctrl + −", "zoom"},
		{"z s x … /", "piano, lower row"}, {"q 2 w … ]", "piano, upper row"},
		{"Shift", "sustain pedal"}, {"F9", "list of scores"},
		{"Ctrl+O", "open another folder"}, {"Ctrl+Q", "quit"},
	})
	mouse := keyGrid([][2]string{
		{"click a note", "start there"}, {"double-click", "play from there"},
		{"right-click", "play, loop and view menu"}, {"drag over bars", "loop them"},
		{"shift-click", "loop to that bar"}, {"drag loop edge", "change the loop (timeline)"},
		{"drag empty space", "move the view"}, {"middle-drag", "move the view"},
		{"Ctrl+wheel", "zoom"}, {"Shift+wheel", "scroll sideways"},
		{"click piano keys", "play them"}, {"right-click a key", "computer keys, your keyboard"},
		{"wheel on tempo", "faster / slower"}, {"wheel on bar", "back / forward a bar"},
		{"back / forward buttons", "back / forward a bar"}, {"right-click Metronome", "its sound"},
	})
	c.commandInfo = gtk.NewLabel("")
	c.commandInfo.SetXAlign(0)
	c.commandInfo.SetWrap(true)
	c.commandInfo.SetMaxWidthChars(56)
	c.commandInfo.AddCSSClass("subtitle")
	c.commandList = gtk.NewBox(gtk.OrientationVertical, 0)
	return w.panel(sectionTitle("Computer keys"), keys,
		sectionTitle("Mouse"), mouse,
		sectionTitle("MIDI command keys"), c.commandInfo, c.commandList)
}

// showCommandKeys fills the keys panel's list of MIDI command keys.
func (w *window) showCommandKeys() {
	c, k := &w.c, &w.commandKeys
	path := k.path
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home+"/") {
		path = "~" + path[len(home):]
	}
	var info string
	if k.m == nil || len(k.m.Bindings) == 0 {
		info = "None. "
	}
	info += "A0+A#0 C4 means A0 and A#0 together, then C4. The first step, keys you never " +
		"play together, takes command mode on: then keys are commands, not music. A command " +
		"ends it, except those marked ↻; so do the first step again and any key that does " +
		"nothing. Edit them in " + path + "; changes apply at once."
	if k.err != nil {
		info += "\n\nProblems: " + k.err.Error()
	}
	c.commandInfo.SetText(info)

	for child := c.commandList.FirstChild(); child != nil; child = c.commandList.FirstChild() {
		c.commandList.Remove(child)
	}
	if k.m == nil {
		return
	}
	var pairs [][2]string
	for _, b := range k.m.Bindings {
		pairs = append(pairs, [2]string{b.Keys, commandWhat(b)})
	}
	c.commandList.Append(keyGrid(pairs))
}

func (w *window) buildStatsPanel() *gtk.Popover {
	c := &w.c
	c.accuracy = gtk.NewLabel("")
	c.accuracy.AddCSSClass("big-number")
	c.streak = gtk.NewLabel("")
	c.streak.AddCSSClass("subtitle")
	tiles := gtk.NewBox(gtk.OrientationHorizontal, 8)
	for i, name := range []string{"PERFECT", "GOOD", "MISSED", "WRONG"} {
		class := []string{"perfect", "good", "missed", "wrong"}[i]
		tile := gtk.NewBox(gtk.OrientationVertical, 2)
		tile.AddCSSClass("stat-tile")
		n := gtk.NewLabel("0")
		n.AddCSSClass("stat-number")
		n.AddCSSClass(class)
		l := gtk.NewLabel(name)
		l.AddCSSClass("stat-name")
		l.AddCSSClass(class)
		tile.Append(n)
		tile.Append(l)
		tiles.Append(tile)
		c.statCounts[i] = n
	}
	explain := gtk.NewLabel("Perfect: within 50 ms. Good: within 300 ms (in wait mode: the music waited longer than 50 ms). Missed: not played in time. Wrong: a key that wasn't due.")
	explain.SetWrap(true)
	explain.SetMaxWidthChars(44)
	explain.SetXAlign(0)
	explain.AddCSSClass("subtitle")
	reset := gtk.NewButtonWithLabel("Reset")
	reset.ConnectClicked(w.resetStats)
	return w.panel(sectionTitle("Session"), c.accuracy, c.streak, tiles, explain, reset)
}

// ordinal writes 2 as "2nd", 3 as "3rd" and so on.
func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

// refresh copies the engine's state into the widgets.
func (w *window) refresh(v practice.View, set practice.Settings) {
	c := &w.c
	c.updating = true
	defer func() { c.updating = false }()
	if c.lastText == nil {
		c.lastText = make(map[*gtk.Label]string)
	}
	text := func(l *gtk.Label, s string) {
		if c.lastText[l] != s {
			l.SetText(s)
			c.lastText[l] = s
		}
	}

	icon := "media-playback-start-symbolic"
	if v.Playing {
		icon = "media-playback-pause-symbolic"
	}
	if icon != c.lastPlayIcon {
		c.play.SetIconName(icon)
		c.lastPlayIcon = icon
	}
	text(c.position, fmt.Sprintf("Bar %d / %d", v.Bar, w.bars[len(w.bars)-1].Number))
	state := "Stopped"
	switch {
	case v.CountingIn:
		state = "Count-in…"
	case v.Waiting:
		state = "Waiting for you"
	case v.Playing:
		state = "Playing"
	case v.Finished:
		state = "Finished"
	}
	if v.Pass >= 2 && (v.Playing || v.Waiting) {
		state += " · " + ordinal(v.Pass) + " time"
	}
	text(c.state, state)

	if !c.modes[set.Mode].Active() {
		c.modes[set.Mode].SetActive(true)
	}
	if !c.hands[set.Hand].Active() {
		c.hands[set.Hand].SetActive(true)
	}
	for i, b := range c.hands {
		playable := w.s.playable(practice.Hand(i))
		b.SetSensitive(playable)
		tip := handTips[i]
		switch {
		case set.Mode == practice.Listen && playable:
			tip = listenTips[i]
		case set.Mode == practice.Listen:
			tip = []string{"No right-hand notes", "No left-hand notes", "Only one hand has notes"}[i]
		case !playable:
			tip = []string{"No right-hand notes on your keyboard", "No left-hand notes on your keyboard",
				"Only one hand has notes on your keyboard"}[i]
		}
		if tip != c.lastHandTip[i] {
			b.SetTooltipText(tip)
			c.lastHandTip[i] = tip
		}
	}
	if w.commandKeys.changed {
		w.showCommandKeys()
		w.commandKeys.changed = false
	}
	text(c.tempo, fmt.Sprintf("♩ %.0f · %.0f%%", v.QuarterBPM, set.Tempo*100))
	if c.metronome.Active() != set.Metronome {
		c.metronome.SetActive(set.Metronome)
	}

	loopLabel := "Loop"
	if set.Loop {
		loopLabel = fmt.Sprintf("Loop %d–%d", set.LoopFrom, set.LoopTo)
	}
	if loopLabel != c.lastLoopButton {
		c.loop.SetLabel(loopLabel)
		if set.Loop {
			c.loop.AddCSSClass("on")
		} else {
			c.loop.RemoveCSSClass("on")
		}
		c.lastLoopButton = loopLabel
	}
	if c.loopOn.Active() != set.Loop {
		c.loopOn.SetActive(set.Loop)
	}
	text(c.loopFrom, fmt.Sprint(set.LoopFrom))
	text(c.loopTo, fmt.Sprint(set.LoopTo))

	st := v.Stats
	acc := "—"
	if a := st.Accuracy(); a >= 0 {
		acc = fmt.Sprintf("%.0f%%", math.Floor(a))
	}
	chip := acc
	if st.Streak > 1 {
		chip += fmt.Sprintf(" · streak %d", st.Streak)
	}
	if chip != c.lastChip {
		c.stats.SetLabel(chip)
		c.lastChip = chip
	}
	text(c.accuracy, acc+" accuracy")
	text(c.streak, fmt.Sprintf("Streak %d · best %d", st.Streak, st.BestStreak))
	for i, n := range []int{st.Perfect, st.Good, st.Missed, st.Wrong} {
		text(c.statCounts[i], fmt.Sprint(n))
	}

	for _, s := range [...]struct {
		sw *gtk.Switch
		on bool
	}{{c.guide, set.Guide}, {c.anyOctave, set.AnyOctave}, {c.repeats, !set.IgnoreRepeats}, {c.pedalMarks, !set.PedalYourself}} {
		if s.sw.Active() != s.on {
			s.sw.SetActive(s.on)
		}
	}
	countIn := "Off"
	switch {
	case set.CountIn == 1:
		countIn = "1 bar"
	case set.CountIn > 1:
		countIn = fmt.Sprintf("%d bars", set.CountIn)
	}
	text(c.countIn, countIn)
	text(c.pianoHeight, fmt.Sprintf("%d px", w.prefs.PianoHeight))
	select1 := func(d *gtk.DropDown, i int) {
		if i >= 0 && d.Selected() != uint(i) {
			d.SetSelected(uint(i))
		}
	}
	select1(c.layout, w.prefs.Columns-1)
	for i, cs := range clickSounds {
		if cs.name == w.prefs.ClickSound {
			select1(c.clickSound, i)
		}
	}
	text(c.clickVolume, fmt.Sprintf("%d%%", w.prefs.ClickVolume))
	for i, s := range cursorStyles {
		if s.id == w.prefs.CursorStyle {
			select1(c.cursorStyle, i)
		}
	}
	for i, cc := range cursorColours {
		if cc.name == w.prefs.CursorColour {
			select1(c.cursorColour, i)
		}
	}
	text(c.keysFrom, noteName(w.keyBase))
	text(c.zoom, fmt.Sprintf("%.0f%%", w.zoom/1.5*100))
	text(c.textSize, fmt.Sprintf("%d%%", w.prefs.TextSize))
	text(c.instrument, "Instrument: "+w.s.instrument())

	light := func(l *gtk.Label, on bool, last *bool) {
		if on != *last {
			if on {
				l.AddCSSClass("on")
			} else {
				l.RemoveCSSClass("on")
			}
			*last = on
		}
	}
	light(c.pedal, w.s.pedalDown(), &c.lastPedal)
	if c.scorePedal.Visible() != v.PedalMarks {
		c.scorePedal.SetVisible(v.PedalMarks)
	}
	light(c.scorePedal, v.ScorePedal, &c.lastScorePedal)

	low, high := w.prefs.KeyLow, w.prefs.KeyHigh
	selected := uint(gtk.INVALID_LIST_POSITION)
	for i, k := range keyboards {
		if k.low == low && k.high == high {
			selected = uint(i)
		}
	}
	if c.keyboardSize.Selected() != selected {
		c.keyboardSize.SetSelected(selected)
	}
	rangeText := fmt.Sprintf("%d keys, %s to %s", high-low+1, noteName(low), noteName(high))
	detectLabel := "Detect"
	switch w.s.detectStep() {
	case 1:
		rangeText, detectLabel = "Press the lowest key on your MIDI keyboard…", "Cancel"
	case 2:
		rangeText, detectLabel = "…now the highest key", "Cancel"
	}
	text(c.keyboardRange, rangeText)
	if c.detect.Label() != detectLabel {
		c.detect.SetLabel(detectLabel)
	}
}
