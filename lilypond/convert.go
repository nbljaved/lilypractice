package lilypond

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Most scores on the web, Mutopia's among them, were written for an older
// LilyPond, whose syntax the current one no longer reads. convert-ly, which
// comes with LilyPond, updates them; Compile does so in a copy, leaving
// your files alone.

var (
	versionRE = regexp.MustCompile(`\\version\s+"(\d+)\.(\d+)(?:\.(\d+))?"`)
	includeRE = regexp.MustCompile(`\\include\s+"([^"]+)"`)
)

type version [3]int

func (v version) String() string { return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2]) }

func (v version) less(w version) bool {
	for i := range v {
		if v[i] != w[i] {
			return v[i] < w[i]
		}
	}
	return false
}

func parseVersion(m []string) version {
	var v version
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	return v
}

// ours is the version of the LilyPond that runs.
var ours = sync.OnceValue(func() version {
	out, err := exec.Command("lilypond", "--version").Output()
	if err != nil {
		return version{}
	}
	m := regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(string(out))
	if m == nil {
		return version{}
	}
	return parseVersion(m)
})

// fileVersion is the version a file says it is written for.
func fileVersion(path string) (version, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return version{}, false
	}
	m := versionRE.FindStringSubmatch(string(b))
	if m == nil {
		return version{}, false
	}
	return parseVersion(m), true
}

// needsConverting says whether a score is written for an older LilyPond
// than ours, and which.
func needsConverting(lyPath string) (string, bool) {
	v, ok := fileVersion(lyPath)
	if !ok || !v.less(ours()) {
		return "", false
	}
	return v.String(), true
}

// convert copies a score and the files it includes from its directory into
// dir, keeping their places relative to each other, and updates the copies
// with convert-ly. It returns the copy of the score, and convert-ly's
// messages. A file without a \version of its own is taken to be as old as
// the score.
func convert(ctx context.Context, lyPath, dir, from string) (string, string, error) {
	src := filepath.Dir(lyPath)
	files := []string{lyPath}
	seen := map[string]bool{lyPath: true}
	for i := 0; i < len(files); i++ {
		b, err := os.ReadFile(files[i])
		if err != nil {
			continue
		}
		for _, m := range includeRE.FindAllStringSubmatch(string(b), -1) {
			// Included files are found next to the one including them, or
			// next to the score. LilyPond's own (english.ly) are neither.
			for _, base := range []string{filepath.Dir(files[i]), src} {
				p := filepath.Clean(filepath.Join(base, m[1]))
				if info, err := os.Stat(p); err == nil && !info.IsDir() && inside(src, p) {
					if !seen[p] {
						seen[p] = true
						files = append(files, p)
					}
					break
				}
			}
		}
	}

	if err := os.RemoveAll(dir); err != nil {
		return "", "", err
	}
	var log strings.Builder
	for _, f := range files {
		rel, _ := filepath.Rel(src, f)
		copy := filepath.Join(dir, rel)
		b, err := os.ReadFile(f)
		if err != nil {
			return "", log.String(), err
		}
		if err := os.MkdirAll(filepath.Dir(copy), 0o755); err != nil {
			return "", log.String(), err
		}
		if err := os.WriteFile(copy, b, 0o644); err != nil {
			return "", log.String(), err
		}
		args := []string{"--edit"}
		if _, ok := fileVersion(f); !ok {
			args = append(args, "--from="+from)
		}
		cmd := exec.CommandContext(ctx, "convert-ly", append(args, copy)...)
		out, err := cmd.CombinedOutput()
		log.Write(out)
		if err != nil {
			return "", log.String(), fmt.Errorf("convert-ly %s: %w", rel, err)
		}
		if err := fixUp(copy); err != nil {
			return "", log.String(), err
		}
	}
	main, _ := filepath.Rel(src, lyPath)
	return filepath.Join(dir, main), log.String(), nil
}

// fixUps are old idioms convert-ly leaves alone, with what replaces them.
var fixUps = []struct {
	old *regexp.Regexp
	new string
}{
	// unfold-repeats has taken a second argument since 2.23.
	{regexp.MustCompile(`\\applyMusic\s*#unfold-repeats\b`), `\unfoldRepeats`},
}

func fixUp(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fixed := b
	for _, f := range fixUps {
		fixed = f.old.ReplaceAll(fixed, []byte(f.new))
	}
	if string(fixed) == string(b) {
		return nil
	}
	return os.WriteFile(path, fixed, 0o644)
}

// inside says whether path p is in directory dir or below it.
func inside(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}
