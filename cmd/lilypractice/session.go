package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"lilypractice/alsa"
	"lilypractice/midikeys"
	"lilypractice/practice"
	"lilypractice/synth"
)

// session ties the practice engine to the clock, the MIDI ports and the
// instrument. The GTK thread, the MIDI input goroutine and the timing
// goroutine all reach the engine through do, which holds the lock.
type session struct {
	mu    sync.Mutex
	eng   *practice.Engine
	start time.Time
	wake  chan struct{}

	seq        *alsa.Seq // nil if the ALSA sequencer is unavailable
	keyboardIn alsa.Port
	scoreOut   alsa.Port // notes played from the score
	clickOut   alsa.Port // metronome and count-in
	keysOut    alsa.Port // notes from the computer keys and the screen piano, so you hear them
	inChannel  int       // 0-15, or -1 for any
	thru       bool
	name       string // our ALSA client name

	// Under mu:
	held        map[int]heldKey // keys down now and how they were judged
	pedal       bool            // your sustain pedal is down
	detect      detection
	commandKeys *midikeys.Map     // keys of the MIDI keyboard that are commands
	onCommand   func(name string) // runs a command, from any goroutine
	midiPlayed  bool              // a MIDI keyboard has played a note

	fluid       *synth.FluidSynth
	statusMu    sync.Mutex
	synthStatus string
}

func newSession(eng *practice.Engine, o options) *session {
	s := &session{
		eng:       eng,
		start:     time.Now(),
		wake:      make(chan struct{}, 1),
		inChannel: o.channel - 1,
		name:      o.name,
		thru:      o.thru,
		held:      make(map[int]heldKey),
	}
	seq, err := openPorts(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lilypractice: no MIDI:", err)
		s.setSynthStatus("no MIDI: " + err.Error())
		return s
	}
	s.seq = seq
	return s
}

func openPorts(s *session) (*alsa.Seq, error) {
	seq, err := alsa.Open(s.name)
	if err != nil {
		return nil, err
	}
	for _, p := range []struct {
		port  *alsa.Port
		name  string
		input bool
	}{
		{&s.keyboardIn, "keyboard-in", true},
		{&s.scoreOut, "score-out", false},
		{&s.clickOut, "click-out", false},
		{&s.keysOut, "keys-out", false},
	} {
		if p.input {
			*p.port, err = seq.InputPort(p.name)
		} else {
			*p.port, err = seq.OutputPort(p.name)
		}
		if err != nil {
			seq.Close()
			return nil, err
		}
	}
	return seq, nil
}

func (s *session) now() time.Duration { return time.Since(s.start) }

// do runs f on the engine and sends the notes it returns.
func (s *session) do(f func(now time.Duration) []practice.Out) {
	s.mu.Lock()
	s.send(f(s.now()))
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *session) view() practice.View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng.View()
}

func (s *session) settings() practice.Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng.Settings()
}

func (s *session) send(out []practice.Out) {
	if s.seq == nil {
		return
	}
	for _, o := range out {
		port := s.scoreOut
		if o.Port == practice.ClickOut {
			port = s.clickOut
		}
		switch {
		case o.Controller != 0:
			s.seq.Controller(port, o.Channel, o.Controller, o.Value)
		case o.On:
			s.seq.NoteOn(port, o.Channel, o.Pitch, o.Velocity)
		default:
			s.seq.NoteOff(port, o.Channel, o.Pitch)
		}
	}
}

// computerPedal is the sustain pedal on the computer keyboard (Shift):
// it holds the notes of the computer keys.
func (s *session) computerPedal(down bool) {
	if s.seq != nil {
		value := 0
		if down {
			value = 127
		}
		s.seq.Controller(s.keysOut, 0, practice.SustainPedal, value)
	}
	s.mu.Lock()
	s.pedal = down
	s.mu.Unlock()
}

func (s *session) pedalDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pedal
}

// detection finds the range of a MIDI keyboard from its lowest and highest
// key, pressed one after the other.
type detection struct {
	step      int // 0 off, 1 waiting for the lowest key, 2 for the highest
	low, high int
	done      bool
}

func (s *session) startDetect() {
	s.mu.Lock()
	s.detect = detection{step: 1}
	s.mu.Unlock()
}

func (s *session) cancelDetect() {
	s.mu.Lock()
	s.detect = detection{}
	s.mu.Unlock()
}

// detected reports the detection's progress; once done, it hands over the
// range and forgets it.
func (s *session) detected() detection {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.detect
	if d.done {
		s.detect = detection{}
	}
	return d
}

// detectStep says what the detection waits for: 0 nothing, 1 the lowest
// key, 2 the highest.
func (s *session) detectStep() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.detect.step
}

// detectKey takes a key for the detection, if one is running.
func (s *session) detectKey(pitch int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch s.detect.step {
	case 1:
		s.detect.low, s.detect.step = pitch, 2
	case 2:
		s.detect.high, s.detect.step, s.detect.done = pitch, 0, true
		if s.detect.high < s.detect.low {
			s.detect.low, s.detect.high = s.detect.high, s.detect.low
		}
	default:
		return false
	}
	return true
}

// replaceEngine practises another score: the old one is stopped first.
func (s *session) replaceEngine(eng *practice.Engine) {
	s.mu.Lock()
	s.send(s.eng.Stop())
	s.eng = eng
	clear(s.held)
	s.mu.Unlock()
}

// setCommandKeys sets which MIDI keys are commands, and what runs them.
func (s *session) setCommandKeys(m *midikeys.Map, run func(name string)) {
	s.mu.Lock()
	s.commandKeys, s.onCommand = m, run
	s.mu.Unlock()
}

// commandKey runs the command of a MIDI key, if it is a command key, and
// reports whether it was one.
func (s *session) commandKey(channel, pitch int, down bool) bool {
	s.mu.Lock()
	m, run := s.commandKeys, s.onCommand
	var command string
	owned := false
	if m != nil {
		if down {
			command, owned = m.Press(channel, pitch)
		} else {
			command, owned = m.Release(channel, pitch)
		}
	}
	s.mu.Unlock()
	if command != "" && run != nil {
		run(command)
	}
	return owned
}

// playedMIDI reports whether a MIDI keyboard has played a note.
func (s *session) playedMIDI() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.midiPlayed
}

// computerKey sounds a note played on the computer keyboard and, when it
// goes down, gives it to the engine.
func (s *session) computerKey(pitch int, down bool) {
	s.sound(pitch, 90, down, practice.ComputerKeys)
}

// sound plays a key of the computer keyboard or of the piano on the
// screen on keys-out, and gives it to the engine.
func (s *session) sound(pitch, velocity int, down bool, src practice.Source) {
	if s.seq != nil {
		if down {
			s.seq.NoteOn(s.keysOut, 0, pitch, velocity)
		} else {
			s.seq.NoteOff(s.keysOut, 0, pitch)
		}
	}
	s.key(pitch, down, src)
}

// key gives a key you pressed to the engine and remembers how it was
// judged while it is held.
func (s *session) key(pitch int, down bool, src practice.Source) {
	s.do(func(now time.Duration) []practice.Out {
		if !down {
			delete(s.held, pitch)
			return nil
		}
		out, result := s.eng.NoteOn(now, pitch, src)
		s.held[pitch] = heldKey{result, s.eng.LastHand()}
		return out
	})
}

// playable reports whether practising with this hand makes sense.
func (s *session) playable(h practice.Hand) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng.Playable(h)
}

func (s *session) loopSpan() (from, to float64, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.eng.LoopSpan()
}

// heldKeys returns the keys down now and how they were judged.
func (s *session) heldKeys() map[int]heldKey {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.held)
}

// runTiming keeps the music moving: it advances the engine when the next
// note is due, and at least every 20 ms. Stopped, or waiting for you, it
// sleeps until do wakes it.
func (s *session) runTiming() {
	timer := time.NewTimer(0)
	for {
		s.mu.Lock()
		s.send(s.eng.Advance(s.now()))
		d := s.eng.UntilNext()
		s.mu.Unlock()
		if d < 0 {
			<-s.wake
			continue
		}
		timer.Reset(min(d, 20*time.Millisecond))
		select {
		case <-timer.C:
		case <-s.wake:
		}
	}
}

// runMIDIInput handles notes arriving at keyboard-in: the keys of a
// keyboard detection, command keys, and notes you play.
func (s *session) runMIDIInput() {
	for {
		ev, err := s.seq.Receive()
		if err != nil {
			s.setSynthStatus("MIDI input stopped: " + err.Error())
			return
		}
		if ev.Port != s.keyboardIn {
			continue
		}
		switch ev.Kind {
		case alsa.NoteOn:
			if s.detectKey(ev.A) || s.commandKey(ev.Channel, ev.A, true) {
				continue
			}
		case alsa.NoteOff:
			if s.commandKey(ev.Channel, ev.A, false) {
				continue
			}
		}
		if s.inChannel >= 0 && ev.Channel != s.inChannel {
			continue
		}
		switch ev.Kind {
		case alsa.NoteOn:
			if s.thru {
				s.seq.NoteOn(s.keysOut, ev.Channel, ev.A, ev.B)
			}
			s.mu.Lock()
			s.midiPlayed = true
			s.mu.Unlock()
			s.key(ev.A, true, practice.MIDIKeyboard)
		case alsa.NoteOff:
			if s.thru {
				s.seq.NoteOff(s.keysOut, ev.Channel, ev.A)
			}
			s.key(ev.A, false, practice.MIDIKeyboard)
		case alsa.Controller: // sustain pedal and the like
			if s.thru {
				s.seq.Controller(s.keysOut, ev.Channel, ev.A, ev.B)
			}
			if ev.A == practice.SustainPedal {
				s.mu.Lock()
				s.pedal = ev.B >= 64
				s.mu.Unlock()
			}
		}
	}
}

// setUpPorts starts or finds the instrument, connects the outputs to it,
// and connects the MIDI keyboard if one was named. The metronome and
// count-in always sound on FluidSynth, whatever plays the notes: a piano
// such as Pianoteq would play their wood blocks as piano notes.
func (s *session) setUpPorts(o options) {
	if o.in != "" {
		if err := s.seq.ConnectFrom(s.keyboardIn, o.in); err != nil {
			fmt.Fprintln(os.Stderr, "lilypractice:", err)
		}
	}
	if o.synth == "auto" {
		s.setSynthStatus("starting FluidSynth...")
		s.setSynthStatus(s.startFluidSynth(o.audio, s.scoreOut, s.clickOut, s.keysOut))
		return
	}
	notes := "none; connect score-out and keys-out yourself, e.g. in qpwgraph"
	if o.synth != "none" {
		notes = o.synth
		if err := s.connectOutputs(o.synth, s.scoreOut, s.keysOut); err != nil {
			notes = err.Error()
		}
	}
	s.setSynthStatus(notes + "; metronome: starting FluidSynth...")
	s.setSynthStatus(notes + "; metronome: " + s.startFluidSynth(o.audio, s.clickOut))
}

// startFluidSynth starts FluidSynth and connects these outputs to it. It
// returns how the status shows it, or what went wrong.
func (s *session) startFluidSynth(audio string, outputs ...alsa.Port) string {
	sf, err := synth.FindSoundfont()
	if err != nil {
		return err.Error()
	}
	// A name of its own, so that two lilypractices never share a synth.
	fluid, err := synth.StartFluidSynth(sf, audio, fmt.Sprintf("%s-%d", s.name, os.Getpid()))
	if err != nil {
		return err.Error()
	}
	s.statusMu.Lock()
	s.fluid = fluid
	s.statusMu.Unlock()
	for i := 0; !slices.Contains(s.seq.Clients(), fluid.Client); i++ {
		if fluid.Exited() || i == 50 {
			return fmt.Sprintf("FluidSynth did not start; try another -audio driver than %q", audio)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := s.connectOutputs(fluid.Client, outputs...); err != nil {
		return err.Error()
	}
	return "FluidSynth with " + filepath.Base(sf)
}

// connectOutputs connects our outputs to an ALSA client.
func (s *session) connectOutputs(client string, outputs ...alsa.Port) error {
	for _, p := range outputs {
		if err := s.seq.ConnectTo(p, client); err != nil {
			return err
		}
	}
	return nil
}

func (s *session) setSynthStatus(status string) {
	s.statusMu.Lock()
	s.synthStatus = status
	s.statusMu.Unlock()
}

func (s *session) instrument() string {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	return s.synthStatus
}

// close silences everything and stops FluidSynth.
func (s *session) close() {
	s.mu.Lock()
	s.send(s.eng.Stop())
	s.mu.Unlock()
	s.statusMu.Lock()
	fluid := s.fluid
	s.fluid = nil
	s.statusMu.Unlock()
	if fluid != nil {
		fluid.Stop()
	}
}

// heldKey is a key you hold down: how it was judged and, when right, which
// hand's note it played.
type heldKey struct {
	result practice.Result
	hand   practice.Hand
}
