// Command lprender compiles a .ly file and writes one page as a PNG, with
// the notes of some onsets coloured and a cursor on the first of them.
// It is a test for the drawing code, without a window.
//
//	lprender [-zoom 2] [-o page.png] piece.ly onset...
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/diamondburned/gotk4/pkg/cairo"

	"lilypractice/lilypond"
	"lilypractice/score"
	"lilypractice/view"
)

func main() {
	zoom := flag.Float64("zoom", 2, "zoom factor")
	png := flag.String("o", "page.png", "PNG to write")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "usage: lprender [-zoom 2] [-o page.png] piece.ly onset...")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), flag.Args()[1:], *zoom, *png); err != nil {
		fmt.Fprintln(os.Stderr, "lprender:", err)
		os.Exit(1)
	}
}

func run(lyPath string, onsetArgs []string, zoom float64, png string) error {
	name := strings.TrimSuffix(filepath.Base(lyPath), filepath.Ext(lyPath))
	out, err := lilypond.Compile(context.Background(), lyPath, filepath.Join("out", name), false)
	if err != nil {
		return err
	}
	s, err := score.Load(out.Notes)
	if err != nil {
		return err
	}
	pages, err := view.Open(out)
	if err != nil {
		return err
	}
	defer pages.Close()

	onsets := s.Onsets()
	highlight := map[string]view.Color{}
	var cursor []string
	colors := []view.Color{view.Blue, view.Green, view.Red}
	for k, arg := range onsetArgs {
		i, err := strconv.Atoi(arg)
		if err != nil || i < 0 || i >= len(onsets) {
			return fmt.Errorf("onset %q: want a number from 0 to %d", arg, len(onsets)-1)
		}
		for _, n := range onsets[i].Notes {
			highlight[n.ID] = colors[k%len(colors)]
			if k == 0 {
				cursor = append(cursor, n.ID)
			}
		}
	}

	// Write the page holding the first highlighted note (or the first page).
	page := 0
	if len(cursor) > 0 {
		page = out.NotePage[cursor[0]]
	}
	w, _ := pages.Size()
	top, h := pages.PageSpan(page)
	surf := cairo.CreateImageSurface(cairo.FormatARGB32, int(math.Ceil(w*zoom)), int(math.Ceil(h*zoom)))
	cr := cairo.Create(surf)
	cr.SetSourceRGB(1, 1, 1)
	cr.Paint()
	cr.Translate(0, -top*zoom)
	pages.Draw(cr, zoom, highlight)
	if box, ok := pages.Union(cursor); ok {
		view.DrawCursor(cr, zoom, box, view.Blue)
	}
	cr.Close()
	if err := surf.WriteToPNG(png); err != nil {
		return err
	}
	fmt.Printf("wrote %s (page %d of %d, %d onsets in the score)\n", png, page+1, len(out.Pages), len(onsets))
	return nil
}
