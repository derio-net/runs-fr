// Package pins guards the version pins that must agree across the repo's build
// files: go.mod, the Dockerfile's Go build stage, and CI's setup-go steps. A
// drifting Renovate PR fails here with a message naming the file, the line and
// both versions.
package pins

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	goModPath      = "../../go.mod"
	dockerfilePath = "../../Dockerfile"
	ciPath         = "../../.github/workflows/ci.yml"
)

// minGoMinor is the oldest Go minor the repo may declare (a supported release
// at the time of writing; see the deps-gate spec, R1).
const minGoMinor = 27

// pin is one version found in one file.
type pin struct {
	file string
	line int
	text string // the matched line, trimmed
	ver  string // the extracted version (e.g. "1.27")
}

func (p pin) String() string { return fmt.Sprintf("%s:%d %q", p.file, p.line, p.text) }

// readLines returns the file's lines; it fails the test when the file is unreadable.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer func() { _ = f.Close() }() // read-only; a close error loses nothing
	var lines []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		lines = append(lines, s.Text())
	}
	if err := s.Err(); err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return lines
}

// findFirstIn returns the first of lines matching re, with its first submatch
// as ver (path only labels the pin).
func findFirstIn(path string, lines []string, re *regexp.Regexp) (pin, bool) {
	for i, l := range lines {
		if m := re.FindStringSubmatch(l); m != nil {
			return pin{file: path, line: i + 1, text: strings.TrimSpace(l), ver: m[1]}, true
		}
	}
	return pin{}, false
}

// minorOf reduces "1.27.0" or "1.27" to "1.27".
func minorOf(v string) string {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) < 2 {
		return v
	}
	return parts[0] + "." + parts[1]
}

var (
	reGoDirective = regexp.MustCompile(`^go\s+(\d+\.\d+(?:\.\d+)?)\s*$`)
	reToolchain   = regexp.MustCompile(`^toolchain\s+go(\d+\.\d+(?:\.\d+)?)\s*$`)
	// An optional @sha256 digest: Renovate's dockerfile manager writes one
	// when digest pinning is on.
	reBuildStage = regexp.MustCompile(`^FROM\s+golang:(\d+\.\d+(?:\.\d+)?)-alpine(?:@sha256:[0-9a-f]+)?\s+AS\s+build\b`)
)

// parseGoMod returns go.mod's go directive and its toolchain. cmd/go omits a
// `toolchain` line equal to the go directive (and `go mod tidy` removes one),
// so a missing line means "the go directive's version", never an error.
func parseGoMod(path string, lines []string) (goDir, toolchain pin, err error) {
	goDir, ok := findFirstIn(path, lines, reGoDirective)
	if !ok {
		return pin{}, pin{}, fmt.Errorf("%s: no `go X.Y[.Z]` directive", path)
	}
	toolchain, ok = findFirstIn(path, lines, reToolchain)
	if !ok {
		toolchain = goDir
		toolchain.text += " (no toolchain line: it equals the go directive)"
	}
	return goDir, toolchain, nil
}

// parseBuildStage returns the Dockerfile's `FROM golang:<v>-alpine AS build`.
func parseBuildStage(path string, lines []string) (pin, error) {
	p, ok := findFirstIn(path, lines, reBuildStage)
	if !ok {
		return pin{}, fmt.Errorf("%s: no `FROM golang:<minor>-alpine[@sha256:…] AS build` stage", path)
	}
	return p, nil
}

// goModPins parses the repo's go.mod; see parseGoMod.
func goModPins(t *testing.T) (goDir, toolchain pin) {
	t.Helper()
	goDir, toolchain, err := parseGoMod(goModPath, readLines(t, goModPath))
	if err != nil {
		t.Fatal(err)
	}
	return goDir, toolchain
}

// dockerBuildStage parses the repo's Dockerfile; see parseBuildStage.
func dockerBuildStage(t *testing.T) pin {
	t.Helper()
	p, err := parseBuildStage(dockerfilePath, readLines(t, dockerfilePath))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// setupGoStep is one actions/setup-go step in a workflow and its `with:` keys.
type setupGoStep struct {
	line int
	with map[string]pin
}

var reIndent = regexp.MustCompile(`^(\s*)`)

func indentOf(l string) int { return len(reIndent.FindString(l)) }

// setupGoSteps finds every `uses: actions/setup-go@…` step in a workflow and
// collects the scalar keys of its `with:` block (a line-oriented parse; the
// workflow files are plain block YAML).
func setupGoSteps(t *testing.T, path string) []setupGoStep {
	t.Helper()
	lines := readLines(t, path)
	var steps []setupGoStep
	for i, l := range lines {
		if !strings.Contains(l, "uses: actions/setup-go@") {
			continue
		}
		st := setupGoStep{line: i + 1, with: map[string]pin{}}
		usesIndent := indentOf(strings.Replace(l, "- ", "  ", 1))
		for j := i + 1; j < len(lines); j++ {
			lj := lines[j]
			if strings.TrimSpace(lj) == "" {
				continue
			}
			if indentOf(lj) < usesIndent || (indentOf(lj) == usesIndent && strings.HasPrefix(strings.TrimSpace(lj), "- ")) {
				break // next step or end of the steps list
			}
			if indentOf(lj) > usesIndent {
				if k, v, ok := strings.Cut(strings.TrimSpace(lj), ":"); ok && strings.TrimSpace(v) != "" {
					st.with[k] = pin{file: path, line: j + 1, text: strings.TrimSpace(lj), ver: strings.Trim(strings.TrimSpace(v), `"'`)}
				}
			}
		}
		steps = append(steps, st)
	}
	return steps
}

func TestGoVersionPinsAgree(t *testing.T) {
	goDir, toolchain := goModPins(t)
	stage := dockerBuildStage(t)
	minor := minorOf(goDir.ver)

	checks := []struct {
		name string
		got  pin
	}{
		{"go.mod toolchain line shares the go directive's minor", toolchain},
		{"Dockerfile build stage uses golang:<go.mod minor>-alpine", stage},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) {
			if minorOf(c.got.ver) != minor {
				t.Errorf("%s has Go %s, but %s declares minor %s", c.got, minorOf(c.got.ver), goDir, minor)
			}
		})
	}

	t.Run("ci.yml setup-go reads go.mod, never a literal", func(t *testing.T) {
		steps := setupGoSteps(t, ciPath)
		if len(steps) == 0 {
			t.Fatalf("%s: no actions/setup-go step found", ciPath)
		}
		for _, st := range steps {
			if lit, ok := st.with["go-version"]; ok {
				t.Errorf("%s sets a literal go-version %s; go.mod (%s) says %s — use `go-version-file: go.mod`", lit, lit.ver, goDir, minor)
			}
			if f, ok := st.with["go-version-file"]; !ok || f.ver != "go.mod" {
				t.Errorf("%s:%d: setup-go step has no `go-version-file: go.mod`", ciPath, st.line)
			}
		}
	})

	t.Run("Go minor is supported", func(t *testing.T) {
		parts := strings.SplitN(minor, ".", 2)
		n, err := strconv.Atoi(parts[len(parts)-1])
		if err != nil || parts[0] != "1" || n < minGoMinor {
			t.Errorf("%s declares Go %s; the oldest allowed minor is 1.%d", goDir, minor, minGoMinor)
		}
	})
}
