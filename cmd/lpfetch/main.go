// Command lpfetch downloads a varied set of keyboard pieces from the
// Mutopia Project (mutopiaproject.org), to test lilypractice on real scores.
//
//	lpfetch [-n 100] [-per 3] [-seed 1] [-dir testdata/mutopia]
//
// It reads Mutopia's piano listing, keeps the pieces for keyboard alone
// (piano, harpsichord, clavichord, organ), and picks n of them at random
// but repeatably, at most per from each composer. Each piece goes in a
// directory of its own: its .ly file, or the files of a multi-file piece.
// index.tsv lists them with their title, composer, style, licence and
// source. The listing pages are kept in dir/.listing, so a second run
// downloads only the pieces.
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"html"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const listing = "https://www.mutopiaproject.org/cgibin/make-table.cgi?Instrument=Piano&startat=%d"

type piece struct {
	id, title, composer, opus, instruments, style, licence string
	url                                                    string // the .ly file or the -lys.zip
}

func main() {
	n := flag.Int("n", 100, "how many pieces")
	per := flag.Int("per", 3, "at most this many by one composer")
	seed := flag.Uint64("seed", 1, "seed for the choice")
	dir := flag.String("dir", "testdata/mutopia", "where to put them")
	flag.Parse()
	if err := run(*n, *per, *seed, *dir); err != nil {
		fmt.Fprintln(os.Stderr, "lpfetch:", err)
		os.Exit(1)
	}
}

func run(n, per int, seed uint64, dir string) error {
	all, err := readListing(filepath.Join(dir, ".listing"))
	if err != nil {
		return err
	}
	var keyboard []piece
	for _, p := range all {
		if keyboardOnly(p.instruments) && p.url != "" {
			keyboard = append(keyboard, p)
		}
	}
	chosen := choose(keyboard, n, per, seed)
	fmt.Printf("%d pieces listed, %d for keyboard alone; fetching %d\n", len(all), len(keyboard), len(chosen))

	index := []string{"dir\tmain file\ttitle\tcomposer\topus\tinstruments\tstyle\tlicence\tsource"}
	for i, p := range chosen {
		name := strings.TrimSuffix(strings.TrimSuffix(path.Base(p.url), ".ly"), "-lys.zip")
		pieceDir := filepath.Join(dir, name)
		mainFile, err := fetchPiece(p.url, pieceDir, name)
		if err != nil {
			fmt.Printf("%3d %-40s FAILED: %v\n", i+1, name, err)
			continue
		}
		fmt.Printf("%3d %-40s %s\n", i+1, name, p.composer)
		index = append(index, strings.Join([]string{name, mainFile, p.title, p.composer, p.opus,
			p.instruments, p.style, p.licence, p.url}, "\t"))
	}
	return os.WriteFile(filepath.Join(dir, "index.tsv"), []byte(strings.Join(index, "\n")+"\n"), 0o644)
}

var (
	pieceTable = regexp.MustCompile(`(?s)<table class="table-bordered result-table">(.*?)</table>`)
	cell       = regexp.MustCompile(`(?s)<td>(.*?)</td>`)
	tag        = regexp.MustCompile(`<[^>]*>`)
	lyLink     = regexp.MustCompile(`href="(https://www\.mutopiaproject\.org/ftp/[^"]*(?:\.ly|-lys\.zip))"`)
	infoID     = regexp.MustCompile(`piece-info\.cgi\?id=(\d+)`)
)

// readListing reads every page of the listing, from the cache or the web.
func readListing(cache string) ([]piece, error) {
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return nil, err
	}
	var all []piece
	for start := 0; ; start += 10 {
		file := filepath.Join(cache, fmt.Sprintf("%05d.html", start))
		page, err := os.ReadFile(file)
		if err != nil {
			if page, err = get(fmt.Sprintf(listing, start)); err != nil {
				return nil, err
			}
			if err := os.WriteFile(file, page, 0o644); err != nil {
				return nil, err
			}
		}
		pieces := parseListing(string(page))
		if len(pieces) == 0 {
			return all, nil
		}
		all = append(all, pieces...)
	}
}

func parseListing(page string) []piece {
	var pieces []piece
	for _, t := range pieceTable.FindAllStringSubmatch(page, -1) {
		var cells []string
		for _, c := range cell.FindAllStringSubmatch(t[1], -1) {
			cells = append(cells, c[1])
		}
		if len(cells) < 12 {
			continue
		}
		text := func(i int) string {
			return strings.TrimSpace(html.UnescapeString(tag.ReplaceAllString(cells[i], "")))
		}
		p := piece{
			title:       text(0),
			composer:    strings.TrimPrefix(text(1), "by "),
			opus:        strings.Trim(text(2), "  "),
			instruments: strings.TrimPrefix(text(4), "for "),
			style:       text(6),
			licence:     text(9),
		}
		if m := infoID.FindStringSubmatch(t[1]); m != nil {
			p.id = m[1]
		}
		if m := lyLink.FindStringSubmatch(t[1]); m != nil {
			p.url = m[1]
		}
		pieces = append(pieces, p)
	}
	return pieces
}

// keyboardOnly says whether a piece is for keyboard instruments alone.
func keyboardOnly(instruments string) bool {
	keyboard := []string{"piano", "harpsichord", "clavichord", "organ", "orgue", "orgel", "keyboard", "clavier", "virginal", "spinet"}
	for _, inst := range strings.Split(instruments, ",") {
		inst = strings.ToLower(strings.TrimSpace(inst))
		if !slices.ContainsFunc(keyboard, func(k string) bool { return strings.Contains(inst, k) }) {
			return false
		}
	}
	return instruments != ""
}

// choose picks n pieces at random, at most per by one composer.
func choose(pieces []piece, n, per int, seed uint64) []piece {
	shuffled := slices.Clone(pieces)
	r := rand.New(rand.NewPCG(seed, seed))
	r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	count := map[string]int{}
	var chosen []piece
	for _, p := range shuffled {
		if len(chosen) == n {
			break
		}
		if count[p.composer] < per {
			count[p.composer]++
			chosen = append(chosen, p)
		}
	}
	slices.SortFunc(chosen, func(a, b piece) int { return strings.Compare(a.url, b.url) })
	return chosen
}

// fetchPiece downloads a piece into dir, unless it is there already, and
// returns its main .ly file, relative to dir.
func fetchPiece(url, dir, name string) (string, error) {
	mainFile := name + ".ly"
	if _, err := os.Stat(filepath.Join(dir, ".done")); err == nil {
		return findMain(dir, mainFile)
	}
	body, err := get(url)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if strings.HasSuffix(url, ".ly") {
		if err := os.WriteFile(filepath.Join(dir, mainFile), body, 0o644); err != nil {
			return "", err
		}
	} else if err := unzip(body, dir); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, ".done"), nil, 0o644); err != nil {
		return "", err
	}
	return findMain(dir, mainFile)
}

// findMain finds the piece's main file: name.ly, wherever the zip put it;
// failing that, a .ly file no other file includes, one for the piano if
// there are several (a horn and piano piece has a file for each).
func findMain(dir, main string) (string, error) {
	var lys []string
	included := map[string]bool{}
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil {
			for _, m := range includeRE.FindAllStringSubmatch(string(b), -1) {
				included[path.Base(m[1])] = true
			}
		}
		if strings.HasSuffix(p, ".ly") {
			lys = append(lys, p)
		}
		return nil
	})
	var candidates []string
	for _, p := range lys {
		if filepath.Base(p) == main {
			return filepath.Rel(dir, p)
		}
		if !included[filepath.Base(p)] {
			candidates = append(candidates, p)
		}
	}
	slices.SortStableFunc(candidates, func(a, b string) int {
		pa, pb := strings.Contains(strings.ToLower(a), "piano"), strings.Contains(strings.ToLower(b), "piano")
		switch {
		case pa && !pb:
			return -1
		case pb && !pa:
			return 1
		}
		return strings.Compare(a, b)
	})
	if len(candidates) == 0 {
		return "", fmt.Errorf("no main .ly file in the download")
	}
	return filepath.Rel(dir, candidates[0])
}

var includeRE = regexp.MustCompile(`\\include\s+"([^"]+)"`)

// unzip extracts a zip into dir, refusing paths that would leave it.
func unzip(data []byte, dir string) error {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range z.File {
		target := filepath.Join(dir, f.Name)
		if !strings.HasPrefix(target, filepath.Clean(dir)+string(os.PathSeparator)) {
			return fmt.Errorf("zip entry %q leaves the directory", f.Name)
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0o755)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		r, err := f.Open()
		if err != nil {
			return err
		}
		b, err := io.ReadAll(io.LimitReader(r, 20<<20))
		r.Close()
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

var lastGet time.Time

// get fetches a URL, waiting a little between requests to spare the site.
func get(url string) ([]byte, error) {
	if wait := 300*time.Millisecond - time.Since(lastGet); wait > 0 {
		time.Sleep(wait)
	}
	lastGet = time.Now()
	c := http.Client{Timeout: 60 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 50<<20))
}
