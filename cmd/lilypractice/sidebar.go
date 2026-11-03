package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"github.com/diamondburned/gotk4/pkg/pango"
)

// The sidebar lists the LilyPond files of a folder: the folder of the
// score you opened, or the one you named or are in. Click one to practise
// it. The list follows the folder as files come and go.

type scoreFile struct {
	path, name string
	about      string // title and composer, from the file's \header
	modTime    time.Time
}

type sidebar struct {
	dir     string
	box     *gtk.Box
	list    *gtk.ListBox
	rows    []*gtk.ListBoxRow
	files   []scoreFile
	checked time.Time
}

func (w *window) buildSidebar(dir string) *gtk.Box {
	sb := &w.side
	sb.dir = dir
	sb.box = gtk.NewBox(gtk.OrientationVertical, 0)
	sb.box.AddCSSClass("sidebar")

	head := gtk.NewBox(gtk.OrientationHorizontal, 6)
	head.AddCSSClass("sidebar-head")
	head.SetTooltipText(dir)
	head.Append(gtk.NewImageFromIconName("folder-symbolic"))
	name := gtk.NewLabel(filepath.Base(dir))
	name.SetEllipsize(pango.EllipsizeMiddle)
	name.SetXAlign(0)
	head.Append(name)
	sb.box.Append(head)

	sb.list = gtk.NewListBox()
	// The score practised is marked by a style of its own: GTK's selection
	// would follow the keyboard focus.
	sb.list.SetSelectionMode(gtk.SelectionNone)
	empty := gtk.NewLabel("No LilyPond files (.ly) here")
	empty.AddCSSClass("subtitle")
	empty.SetWrap(true)
	empty.SetMarginTop(12)
	sb.list.SetPlaceholder(empty)
	sb.list.ConnectRowActivated(func(row *gtk.ListBoxRow) {
		if i := row.Index(); i >= 0 && i < len(sb.files) {
			w.choose(sb.files[i].path)
		}
	})
	scroll := gtk.NewScrolledWindow()
	scroll.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scroll.SetVExpand(true)
	scroll.SetChild(sb.list)
	sb.box.Append(scroll)

	sb.box.SetVisible(!w.prefs.HideSidebar)
	w.sidebarLeast()
	w.checkFolder()
	return sb.box
}

// sidebarLeast keeps the list from being squeezed to nothing when the
// window is too narrow for everything, as with large text.
func (w *window) sidebarLeast() { w.side.box.SetSizeRequest(int(120*w.textScale()), -1) }

// checkFolder lists the folder again, every two seconds, and redoes the
// rows if anything changed.
func (w *window) checkFolder() {
	sb := &w.side
	if time.Since(sb.checked) < 2*time.Second {
		return
	}
	sb.checked = time.Now()
	files := listScores(sb.dir, sb.files)
	if slices.EqualFunc(files, sb.files, func(a, b scoreFile) bool {
		return a.path == b.path && a.modTime.Equal(b.modTime)
	}) {
		return
	}
	sb.files = files
	for _, r := range sb.rows {
		sb.list.Remove(r)
	}
	sb.rows = sb.rows[:0]
	for _, f := range files {
		text := gtk.NewBox(gtk.OrientationVertical, 1)
		name := gtk.NewLabel(f.name)
		name.SetXAlign(0)
		name.SetEllipsize(pango.EllipsizeEnd)
		text.Append(name)
		if f.about != "" {
			about := gtk.NewLabel(f.about)
			about.SetXAlign(0)
			about.SetEllipsize(pango.EllipsizeEnd)
			about.AddCSSClass("subtitle")
			text.Append(about)
		}
		row := gtk.NewListBoxRow()
		row.SetChild(text)
		row.SetTooltipText(filepath.Base(f.path))
		sb.list.Append(row)
		sb.rows = append(sb.rows, row)
	}
	w.markChosen()
	if w.msg.idle {
		w.showNothingChosen()
	}
}

// markChosen marks the row of the score being practised or loaded.
func (w *window) markChosen() {
	sb := &w.side
	path := w.pending
	if path == "" {
		path = w.file
	}
	for i, f := range sb.files {
		if f.path == path {
			sb.rows[i].AddCSSClass("chosen")
		} else {
			sb.rows[i].RemoveCSSClass("chosen")
		}
	}
}

func (w *window) sidebarShown() bool { return w.side.box.Visible() }

func (w *window) setSidebar(show bool) {
	w.side.box.SetVisible(show)
	if w.c.sidebar.Active() != show {
		w.c.sidebar.SetActive(show)
	}
	w.prefs.HideSidebar = !show
	w.prefsChanged = true
	if w.msg.idle {
		w.showNothingChosen()
	}
}

// listScores finds the .ly files in dir, by name. Titles are read again
// only from files that changed since known.
func listScores(dir string, known []scoreFile) []scoreFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var files []scoreFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".ly") || strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		info, err := os.Stat(path) // following links
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		f := scoreFile{path: path, name: strings.TrimSuffix(name, ".ly"), modTime: info.ModTime()}
		if i := slices.IndexFunc(known, func(k scoreFile) bool { return k.path == path }); i >= 0 && known[i].modTime.Equal(f.modTime) {
			f.about = known[i].about
		} else {
			f.about = aboutScore(path)
		}
		files = append(files, f)
	}
	slices.SortFunc(files, func(a, b scoreFile) int {
		return strings.Compare(strings.ToLower(a.name), strings.ToLower(b.name))
	})
	return files
}

// Fields of a \header, not subtitle or mutopiatitle.
var (
	titleField    = regexp.MustCompile(`(?m)(?:^|[\s{])title\s*=\s*"((?:[^"\\]|\\.)*)"`)
	composerField = regexp.MustCompile(`(?m)(?:^|[\s{])composer\s*=\s*"((?:[^"\\]|\\.)*)"`)
)

// aboutScore gives "title · composer" from a score's \header, as far as
// they are plain strings.
func aboutScore(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head, _ := io.ReadAll(io.LimitReader(f, 64<<10))
	var parts []string
	for _, field := range []*regexp.Regexp{titleField, composerField} {
		if m := field.FindSubmatch(head); m != nil {
			if s := strings.TrimSpace(strings.ReplaceAll(string(m[1]), `\"`, `"`)); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, " · ")
}
