package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	glib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"lilypractice/lilypond"
	"lilypractice/practice"
	"lilypractice/score"
	"lilypractice/view"
)

// Scores are loaded in the background: LilyPond may take a while, and the
// window stays usable meanwhile. Choosing another score stops the one
// loading; only one LilyPond runs at a time.

type loader struct {
	gen    int // counts loads, so that a stopped one's result is dropped
	cancel context.CancelFunc
	done   chan struct{} // closed when the last load has finished
}

// loaded is what a load makes.
type loaded struct {
	out    *lilypond.Output
	sc     *score.Score
	pages  *view.Pages
	chosen int // which score of the file
	err    error
}

// choose practises a score of the sidebar: another file, or the same one
// again if it was changed since it was loaded.
func (w *window) choose(path string) {
	if path == w.pending {
		return
	}
	if path == w.file && w.pending == "" {
		if info, err := os.Stat(path); err == nil && info.ModTime().Equal(w.fileTime) {
			return
		}
	}
	w.open(path, 0)
}

// open loads a score file and practises score chosen of it.
func (w *window) open(path string, chosen int) {
	w.unload()
	if w.load.cancel != nil {
		w.load.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	previous, done := w.load.done, make(chan struct{})
	w.load.gen++
	gen := w.load.gen
	w.load.cancel, w.load.done = cancel, done
	w.pending = path
	w.markChosen()
	name := scoreName(path)
	w.showMessage(true, "Engraving "+name+"…",
		"LilyPond is engraving the score. The first time takes a few seconds; after that it is quick, until the file changes.")

	force := w.recompile
	go func() {
		defer close(done)
		if previous != nil {
			<-previous
		}
		res := loadScore(ctx, path, chosen, force)
		glib.IdleAdd(func() {
			if gen != w.load.gen { // another was chosen meanwhile
				if res.pages != nil {
					res.pages.Close()
				}
				return
			}
			w.load.cancel = nil
			w.pending = ""
			w.practise(path, res)
		})
	}()
}

// stopLoading stops LilyPond, if it is running, and waits a moment for it:
// it isn't to go on after the program.
func (w *window) stopLoading() {
	if w.load.cancel == nil {
		return
	}
	w.load.cancel()
	select {
	case <-w.load.done:
	case <-time.After(2 * time.Second):
	}
}

// loadScore runs LilyPond on a file and reads what it made.
func loadScore(ctx context.Context, path string, chosen int, force bool) loaded {
	cache, err := os.UserCacheDir()
	if err != nil {
		return loaded{err: err}
	}
	// Named for the file, and kept apart from files of the same name elsewhere.
	dir := fmt.Sprintf("%s-%.4x", scoreName(path), sha256.Sum256([]byte(path)))
	out, err := lilypond.Compile(ctx, path, filepath.Join(cache, "lilypractice", dir), force)
	if err != nil {
		return loaded{err: err}
	}
	if out.Converted != "" {
		fmt.Fprintf(os.Stderr, "%s is written for LilyPond %s: updated it with convert-ly (your file is unchanged)\n", path, out.Converted)
	}
	if len(out.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "LilyPond went on after %d error(s), so parts of %s may be missing:\n  %s\n",
			len(out.Errors), path, strings.Join(out.Errors, "\n  "))
	}
	if chosen < 0 || chosen >= len(out.Scores) {
		fmt.Fprintf(os.Stderr, "lilypractice: -score %d: %s has %d score(s); practising the first\n", chosen+1, path, len(out.Scores))
		chosen = 0
	}
	sc, err := score.Load(out.Scores[chosen])
	if err != nil {
		return loaded{err: err}
	}
	if len(sc.Notes) == 0 {
		return loaded{err: fmt.Errorf("there are no notes in it")}
	}
	pages, err := view.Open(out)
	if err != nil {
		return loaded{err: err}
	}
	return loaded{out: out, sc: sc, pages: pages, chosen: chosen}
}

// practise shows a loaded score and practises it, with the settings of the
// last one but no loop; or says why it couldn't be loaded.
func (w *window) practise(path string, res loaded) {
	if res.err != nil {
		fmt.Fprintf(os.Stderr, "lilypractice: %s: %v\n", path, res.err)
		w.showMessage(false, "Couldn't open "+scoreName(path), briefly(res.err))
		return
	}
	if info, err := os.Stat(path); err == nil {
		w.fileTime = info.ModTime()
	}
	w.setPages(res.pages)
	w.file, w.scores, w.score = path, res.out.Scores, res.chosen
	w.replaceEngine(practice.New(res.sc, w.blankSettings()))
	w.keepPlayableHand()
	w.showScoreChoice()
	w.setTitle(scoreName(path))
	w.markChosen()
	w.stack.SetVisibleChildName("practice")
	w.scrollTo(0, 0)
	w.placeView = true
}

// unload leaves nothing to practise: the music stops, and an empty score
// keeps the settings.
func (w *window) unload() {
	w.replaceEngine(practice.New(&score.Score{}, w.blankSettings()))
	w.setPages(emptyPages())
	w.file, w.scores, w.score = "", nil, 0
	w.showScoreChoice()
	w.setTitle(filepath.Base(w.side.dir))
}

// blankSettings are the settings of the score being practised, for the next
// one: the loop is of the old score's bars.
func (w *window) blankSettings() practice.Settings {
	set := w.s.settings()
	set.Loop, set.LoopFrom, set.LoopTo = false, 0, 0
	return set
}

func (w *window) replaceEngine(eng *practice.Engine) {
	w.s.replaceEngine(eng)
	w.takeEngine()
	w.drag = loopDrag{}
	w.loopSet = false
	w.wrongSeen = 0
	w.manualAt = time.Time{}
	w.dirty = true
}

func (w *window) setPages(p *view.Pages) {
	if w.pages != nil {
		w.pages.Close()
	}
	w.pages = p
	w.pages.SetColumns(w.prefs.Columns)
	w.barAreaCache, w.systemBoxes = nil, nil
}

func emptyPages() *view.Pages {
	p, _ := view.Open(&lilypond.Output{})
	return p
}

// loaded reports whether there is a score to practise.
func (w *window) loaded() bool { return w.file != "" }

func (w *window) setTitle(title string) {
	w.titleLabel.SetText(title)
	w.win.SetTitle(title + " — lilypractice")
}

// The page shown instead of the score: while one loads, when none is
// chosen, or when one couldn't be loaded.

type message struct {
	spinner *gtk.Spinner
	title   *gtk.Label
	detail  *gtk.Label
	idle    bool // it asks you to choose a score
}

func (w *window) buildMessage() *gtk.Box {
	m := &w.msg
	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.AddCSSClass("message")
	box.SetVAlign(gtk.AlignCenter)
	box.SetHAlign(gtk.AlignCenter)
	m.spinner = gtk.NewSpinner()
	m.spinner.SetSizeRequest(32, 32)
	m.title = gtk.NewLabel("")
	m.title.AddCSSClass("message-title")
	m.title.SetWrap(true)
	m.detail = gtk.NewLabel("")
	m.detail.AddCSSClass("subtitle")
	m.detail.SetWrap(true)
	m.detail.SetMaxWidthChars(70)
	m.detail.SetSelectable(true)
	m.detail.SetJustify(gtk.JustifyCenter)
	box.Append(m.spinner)
	box.Append(m.title)
	box.Append(m.detail)
	return box
}

func (w *window) showMessage(busy bool, title, detail string) {
	m := &w.msg
	m.spinner.SetVisible(busy)
	m.spinner.SetSpinning(busy)
	m.title.SetText(title)
	m.detail.SetText(detail)
	m.idle = false
	w.stack.SetVisibleChildName("message")
}

func (w *window) showNothingChosen() {
	if len(w.side.files) == 0 {
		w.showMessage(false, "No scores here", "There are no LilyPond files (.ly) in "+w.side.dir+". Ctrl+O opens another folder.")
	} else {
		where := "in the list on the left"
		if !w.sidebarShown() {
			where = "in the list of scores (F9 shows it)"
		}
		w.showMessage(false, "Choose a score", "Click a LilyPond file "+where+" to practise it.")
	}
	w.msg.idle = true
}

func scoreName(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// briefly gives the gist of a load's error: its first line and the
// errors in LilyPond's log, not the whole log.
func briefly(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	gist := []string{lines[0]}
	for _, l := range lines[1:] {
		if strings.Contains(l, "error:") && len(gist) <= 6 {
			gist = append(gist, l)
		}
	}
	return strings.Join(gist, "\n")
}
