// Package synth starts the default instrument: FluidSynth with a General
// MIDI soundfont, listening on an ALSA sequencer port.
//
// Any other instrument can play the notes (Pianoteq, a plugin in Carla or
// Ardour, SuperCollider); the metronome stays on FluidSynth.
package synth

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
)

// FindSoundfont looks for a .sf2 or .sf3 file in the usual places,
// preferring FluidR3 (Guix package fluid-3).
func FindSoundfont() (string, error) {
	if sf := os.Getenv("LILYPRACTICE_SOUNDFONT"); sf != "" {
		return sf, nil
	}
	home, _ := os.UserHomeDir()
	dataDirs := filepath.SplitList(os.Getenv("XDG_DATA_DIRS"))
	dataDirs = append(dataDirs,
		filepath.Join(home, ".guix-home/profile/share"),
		filepath.Join(home, ".guix-profile/share"),
		"/run/current-system/profile/share",
		"/usr/share")
	var found []string
	for _, dir := range dataDirs {
		for _, sub := range []string{"soundfonts", "sounds/sf2", "sounds/sf3"} {
			for _, ext := range []string{"*.sf2", "*.sf3"} {
				m, _ := filepath.Glob(filepath.Join(dir, sub, ext))
				found = append(found, m...)
			}
		}
	}
	if len(found) == 0 {
		return "", errors.New("no soundfont found; install fluid-3 or set LILYPRACTICE_SOUNDFONT")
	}
	sort.SliceStable(found, func(i, j int) bool {
		return strings.Contains(found[i], "FluidR3") && !strings.Contains(found[j], "FluidR3")
	})
	return found[0], nil
}

// FluidSynth is a running FluidSynth.
type FluidSynth struct {
	// Client is the ALSA sequencer client name to connect to.
	Client string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	done   chan struct{} // closed when FluidSynth exits
}

// StartFluidSynth runs FluidSynth with this soundfont. audioDriver may be
// empty for FluidSynth's default, or e.g. "pipewire", "pulseaudio", "jack".
// FluidSynth exits when Stop is called or when this program exits.
func StartFluidSynth(soundfont, audioDriver, id string) (*FluidSynth, error) {
	args := []string{
		"-m", "alsa_seq",
		"-o", "midi.alsa_seq.id=" + id,
		"-o", "synth.gain=0.7",
		"-o", "audio.period-size=128", // lower latency than the default
	}
	if audioDriver != "" {
		args = append(args, "-a", audioDriver)
	}
	args = append(args, soundfont)
	cmd := exec.Command("fluidsynth", args...)
	// FluidSynth reads commands from its shell and exits when the shell
	// closes, so it lives exactly as long as this pipe stays open.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// And should this program die without closing it (killed, or its X
	// connection cut), the kernel stops FluidSynth too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	f := &FluidSynth{Client: "FLUID Synth (" + id + ")", cmd: cmd, stdin: stdin, done: make(chan struct{})}
	started := make(chan error)
	go func() {
		// The death signal is tied to the thread that started FluidSynth,
		// so that thread must last as long as FluidSynth does.
		runtime.LockOSThread()
		err := cmd.Start()
		started <- err
		if err == nil {
			cmd.Wait()
			close(f.done)
		}
	}()
	if err := <-started; err != nil {
		return nil, fmt.Errorf("starting fluidsynth: %w", err)
	}
	return f, nil
}

// Exited reports whether FluidSynth has stopped, for example because the
// audio driver could not start.
func (f *FluidSynth) Exited() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

func (f *FluidSynth) Stop() {
	io.WriteString(f.stdin, "quit\n")
	f.stdin.Close()
	select {
	case <-f.done:
	case <-time.After(2 * time.Second):
		f.cmd.Process.Kill()
		<-f.done
	}
}
