// Package lilypond compiles .ly files with the lilypractice plugin
// (lilypractice.ly), which gives every note head an SVG id and writes the
// notes to a JSON file.
package lilypond

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed lilypractice.ly
var plugin []byte

// Output lists the files produced by one compile.
type Output struct {
	Pages      []string       // SVG pages, in order
	Notes      string         // JSON written by the plugin: the first score's
	Scores     []string       // the JSON of every score in the file, in order
	MIDI       string         // empty if the score has no \midi block
	NotePage   map[string]int // note id -> index into Pages
	NoteSystem map[string]int // note id -> line of music (system) it is on
	Staves     []Staff        // the staves of every line of music
	Log        string         // LilyPond's messages
	// Converted is the LilyPond version the score was written for, when
	// it was older than ours and so was updated with convert-ly first.
	Converted string
	// Errors are the errors LilyPond reported while still producing the
	// score, as it does for a setting it doesn't know: parts of the score
	// may be missing or look different.
	Errors []string
}

// Staff is one staff of one line of music.
type Staff struct {
	ID     string
	Page   int // index into Pages
	System int
}

// Compile runs LilyPond on lyPath and writes the results to outDir.
// Unless force is set, it reuses an earlier compile when the .ly file and
// the plugin are unchanged; files the score \includes are not checked.
// A score written for an older LilyPond is updated first (see convert).
func Compile(ctx context.Context, lyPath, outDir string, force bool) (*Output, error) {
	lyPath, err := filepath.Abs(lyPath)
	if err != nil {
		return nil, err
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	info, err := os.Stat(lyPath)
	if err != nil {
		return nil, err
	}
	stem := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(lyPath), filepath.Ext(lyPath)))
	notes := stem + ".notes.json"
	stampPath := stem + ".stamp"
	stamp := fmt.Sprintf("%s\n%d %d\n%x\n", lyPath, info.ModTime().UnixNano(), info.Size(), sha256.Sum256(plugin))
	if old, err := os.ReadFile(stampPath); err == nil && !force && string(old) == stamp {
		if _, err := os.Stat(notes); err == nil {
			return collect(&Output{Notes: notes, Pages: pagesOf(stem)}, stem)
		}
	}
	os.Remove(stampPath)

	pluginPath := filepath.Join(outDir, ".lilypractice-plugin.ly")
	if err := os.WriteFile(pluginPath, plugin, 0o644); err != nil {
		return nil, err
	}
	// Nothing from an earlier compile may be taken for this one's: it may
	// have had more pages, or this one may fail before writing the notes.
	for _, old := range append(append(pagesOf(stem), scoresOf(notes)...), notes, stem+".midi", stem+".mid") {
		os.Remove(old)
	}

	source, converted, convertLog := lyPath, "", ""
	if from, old := needsConverting(lyPath); old {
		var err error
		if source, convertLog, err = convert(ctx, lyPath, filepath.Join(outDir, "converted"), from); err != nil {
			source, convertLog = lyPath, fmt.Sprintf("convert-ly failed, so the score is compiled as it is: %v\n%s", err, convertLog)
		} else {
			converted = from
		}
	}

	cmd := exec.CommandContext(ctx, "lilypond",
		"-dbackend=svg",
		"-dno-point-and-click",
		"-dinclude-settings="+pluginPath,
		"-I", filepath.Dir(lyPath), // for files a converted score includes but convert left alone
		"-o", stem,
		source)
	cmd.Env = append(os.Environ(), "LILYPRACTICE_JSON="+notes)
	log, err := cmd.CombinedOutput()
	log = append([]byte(convertLog), log...)
	out := &Output{Log: string(log), Notes: notes, Pages: pagesOf(stem), Converted: converted}
	if err != nil {
		// LilyPond carries on after most errors and says so at the end.
		// What it made is worth showing, as long as it made the notes.
		if _, statErr := os.Stat(notes); statErr != nil || len(out.Pages) == 0 || ctx.Err() != nil {
			return out, fmt.Errorf("lilypond failed: %w\n%s", err, log)
		}
		out.Errors = errorLines(string(log))
		if len(out.Errors) == 0 {
			out.Errors = []string{"lilypond: " + err.Error()}
		}
	}
	if len(out.Pages) == 0 {
		return out, fmt.Errorf("LilyPond printed no music: the file has no score, only (say) music for other files to include\n%s", log)
	}
	if out, err := collect(out, stem); err != nil {
		return out, err
	}
	return out, os.WriteFile(stampPath, []byte(stamp), 0o644)
}

// collect finds the MIDI file and which page each note is on.
// errorLines picks LilyPond's error messages out of its log.
func errorLines(log string) []string {
	var errs []string
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, "error: ") && !strings.Contains(line, "fatal error: failed files") {
			errs = append(errs, strings.TrimSpace(line))
		}
	}
	return errs
}

// scoresOf lists the JSON files of the scores after the first: the plugin
// writes piece.notes-2.json for the second, and so on.
func scoresOf(notes string) []string {
	var more []string
	ext := filepath.Ext(notes)
	for i := 2; ; i++ {
		p := strings.TrimSuffix(notes, ext) + "-" + strconv.Itoa(i) + ext
		if _, err := os.Stat(p); err != nil {
			return more
		}
		more = append(more, p)
	}
}

func collect(out *Output, stem string) (*Output, error) {
	if len(out.Pages) == 0 {
		return out, fmt.Errorf("no SVG pages for %s", stem)
	}
	out.Scores = append([]string{out.Notes}, scoresOf(out.Notes)...)
	for _, ext := range []string{".midi", ".mid"} {
		if _, err := os.Stat(stem + ext); err == nil {
			out.MIDI = stem + ext
			break
		}
	}
	out.NotePage = make(map[string]int)
	out.NoteSystem = make(map[string]int)
	for i, page := range out.Pages {
		notes, staves, err := Scan(page)
		if err != nil {
			return out, err
		}
		for _, n := range notes {
			out.NotePage[n.ID] = i
			out.NoteSystem[n.ID] = n.System
		}
		for _, s := range staves {
			out.Staves = append(out.Staves, Staff{ID: s.ID, Page: i, System: s.System})
		}
	}
	return out, nil
}

var pageNumber = regexp.MustCompile(`-(\d+)\.svg$`)

// pagesOf returns stem.svg, or stem-1.svg, stem-2.svg, ... for several pages.
func pagesOf(stem string) []string {
	if _, err := os.Stat(stem + ".svg"); err == nil {
		return []string{stem + ".svg"}
	}
	matches, _ := filepath.Glob(stem + "-*.svg")
	var pages []string
	for _, m := range matches {
		if pageNumber.MatchString(strings.TrimPrefix(m, stem)) {
			pages = append(pages, m)
		}
	}
	number := func(p string) int {
		n, _ := strconv.Atoi(pageNumber.FindStringSubmatch(p)[1])
		return n
	}
	sort.Slice(pages, func(i, j int) bool { return number(pages[i]) < number(pages[j]) })
	return pages
}

// Tagged is an element the plugin tagged: a note head or a piece of staff,
// with the number of the line of music (system) it is on, or -1.
type Tagged struct {
	ID     string
	System int
}

// Scan returns the note heads and staves in an SVG page, in document order.
func Scan(svgPath string) (notes, staves []Tagged, err error) {
	f, err := os.Open(svgPath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	dec := xml.NewDecoder(f)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return notes, staves, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", svgPath, err)
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var class string
		t := Tagged{System: -1}
		for _, a := range el.Attr {
			switch a.Name.Local {
			case "id":
				t.ID = a.Value
			case "class":
				class = a.Value
			case "data-system":
				if n, err := strconv.Atoi(a.Value); err == nil {
					t.System = n
				}
			}
		}
		if t.ID == "" {
			continue
		}
		switch class {
		case "lp-note":
			notes = append(notes, t)
		case "lp-staff":
			staves = append(staves, t)
		}
	}
}
