// Command lilypractice is a piano practice program for LilyPond scores.
//
//	lilypractice [flags] [piece.ly | folder]
//
// It shows the engraved score with a cursor, plays the hand you are not
// practising, and in wait mode stops until you play your notes. Play on a
// MIDI keyboard connected to its keyboard-in port, or on the computer
// keyboard. A list on the left has the scores of the piece's folder, of the
// folder named, or of the current one. Run with -h for the flags; the keys
// are listed in the window.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	glib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/practice"
	"lilypractice/score"
)

type options struct {
	synth, audio, in string
	name             string
	channel          int
	hand, mode       string
	tempo            float64
	thru, recompile  bool
	score            int
}

func main() {
	var o options
	flag.StringVar(&o.synth, "synth", "auto", `instrument: "auto" starts FluidSynth, "none" leaves the ports for you to connect, anything else is an ALSA client to connect to, e.g. "Pianoteq"`)
	flag.StringVar(&o.audio, "audio", "pipewire", "FluidSynth's audio driver: pipewire, jack or alsa")
	flag.StringVar(&o.in, "in", "", `MIDI keyboard to connect to keyboard-in, e.g. "SL88"`)
	flag.StringVar(&o.name, "name", "lilypractice", "ALSA client name, to tell several lilypractices apart")
	flag.IntVar(&o.channel, "channel", 0, "only listen to this MIDI channel (1-16); 0 means all")
	flag.StringVar(&o.hand, "hand", "", "the hand you practise (default right), or in listen mode the hand you hear (default both): right, left or both")
	flag.StringVar(&o.mode, "mode", "wait", "wait: stop until you play your notes; play: keep time and score you; listen: the program plays everything")
	flag.Float64Var(&o.tempo, "tempo", 100, "tempo, in percent of the written tempo")
	flag.BoolVar(&o.recompile, "recompile", false, "run LilyPond even if the .ly file is unchanged since last time (e.g. after editing a file it includes)")
	flag.BoolVar(&o.thru, "thru", false, "pass notes from keyboard-in on to keys-out, for a keyboard connected only to lilypractice")
	flag.IntVar(&o.score, "score", 1, "in a file of several scores (movements, pieces of a set), the one to practise; the header lets you change it")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: lilypractice [flags] [piece.ly | folder]")
		fmt.Fprintln(os.Stderr, "The list of scores shows the piece's folder, the folder named, or the current one.")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), o); err != nil {
		fmt.Fprintln(os.Stderr, "lilypractice:", err)
		os.Exit(1)
	}
}

func (o options) settings() (practice.Settings, error) {
	set := practice.Settings{Tempo: o.tempo / 100, CountIn: 1, AnyOctave: true}
	hand := o.hand
	if hand == "" {
		hand = "right"
		if o.mode == "listen" {
			hand = "both"
		}
	}
	switch hand {
	case "right":
		set.Hand = practice.Right
	case "left":
		set.Hand = practice.Left
	case "both":
		set.Hand = practice.Both
	default:
		return set, fmt.Errorf("-hand %q: want right, left or both", o.hand)
	}
	switch o.mode {
	case "wait":
		set.Mode = practice.Wait
	case "play":
		set.Mode = practice.Play
	case "listen":
		set.Mode = practice.Listen
	default:
		return set, fmt.Errorf("-mode %q: want wait, play or listen", o.mode)
	}
	if o.channel < 0 || o.channel > 16 {
		return set, fmt.Errorf("-channel %d: want 1 to 16, or 0 for all", o.channel)
	}
	return set, nil
}

// run practises arg, a score file or a folder of them ("" for the current
// folder).
func run(arg string, o options) error {
	set, err := o.settings()
	if err != nil {
		return err
	}
	start := launch{score: o.score - 1, recompile: o.recompile}
	if arg == "" {
		arg = "."
	}
	path, err := filepath.Abs(arg)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		start.dir = path
	} else {
		start.dir, start.file = filepath.Dir(path), path
	}

	p := loadPrefs()
	set.KeyLow, set.KeyHigh = p.KeyLow, p.KeyHigh
	set.CountIn = p.CountIn
	set.IgnoreRepeats = p.IgnoreRepeats
	click := clickSoundNamed(p.ClickSound)
	set.ClickAccent, set.ClickBeat, set.ClickVolume = click.accent, click.beat, float64(p.ClickVolume)/100
	// Nothing to practise until a score is loaded.
	s := newSession(practice.New(&score.Score{}, set), o)
	defer s.close()
	go s.runTiming()
	if s.seq != nil {
		go s.runMIDIInput()
		go s.setUpPorts(o)
	}

	app := gtk.NewApplication("org.lilypractice.Practice", gio.ApplicationNonUnique)
	app.ConnectActivate(func() { newWindow(app, s, p, start) })
	quitOnSignal(app, s)
	// GTK would take the .ly argument as a file to open, so keep it out.
	if code := app.Run(os.Args[:1]); code != 0 {
		return fmt.Errorf("GTK exited with status %d", code)
	}
	return nil
}

// quitOnSignal ends the program cleanly when it is asked to stop (SIGTERM,
// SIGHUP, SIGINT), as a window manager or terminal may do. Should GTK not
// manage to quit, for example because its display is gone, it exits anyway.
func quitOnSignal(app *gtk.Application, s *session) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	go func() {
		<-signals
		glib.IdleAdd(app.Quit)
		time.Sleep(3 * time.Second)
		s.close()
		os.Exit(1)
	}()
}
