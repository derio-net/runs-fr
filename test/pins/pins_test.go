// Package pins guards the version pins that must agree across the repo's build
// files: go.mod, the Dockerfile's Go build stage, and CI's setup-go steps. A
// drifting Renovate PR fails here with a message naming the file, the line and
// both versions.
//
// Every check is a pure function over a file's lines, so mutation_test.go can
// prove each one against a mutated copy of the live file.
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

var reMinor = regexp.MustCompile(`^(\d+)\.(\d+)`)

// minorOf reduces "1.27.0", "1.27" or "1.28rc1" to its minor ("1.27", "1.28").
func minorOf(v string) string {
	if m := reMinor.FindStringSubmatch(v); m != nil {
		return m[1] + "." + m[2]
	}
	return v
}

// stripYAMLComment drops a YAML comment: a whole-line `#…`, or a ` #…` tail
// outside quotes. Checks then match only what the workflow actually runs.
func stripYAMLComment(l string) string {
	if strings.HasPrefix(strings.TrimSpace(l), "#") {
		return ""
	}
	var quote rune
	for i, r := range l {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '#' && i > 0 && (l[i-1] == ' ' || l[i-1] == '\t'):
			return strings.TrimRight(l[:i], " \t")
		}
	}
	return l
}

var (
	// go.mod allows a trailing `// comment` on either line.
	reGoDirective = regexp.MustCompile(`^go\s+(\d+\.\d+(?:\.\d+)?)\s*(?://.*)?$`)
	reToolchain   = regexp.MustCompile(`^toolchain\s+go(\d+\.\d+(?:\.\d+)?(?:(?:rc|beta)\d+)?)\s*(?://.*)?$`)
	reToolchainKw = regexp.MustCompile(`^toolchain\b`)
	// An optional @sha256 digest: Renovate's dockerfile manager writes one
	// when digest pinning is on.
	reBuildStage = regexp.MustCompile(`^FROM\s+golang:(\d+\.\d+(?:\.\d+)?)-alpine(?:@sha256:[0-9a-f]+)?\s+AS\s+build\b`)
)

// parseGoMod returns go.mod's go directive and its toolchain. cmd/go omits a
// `toolchain` line equal to the go directive (and `go mod tidy` removes one),
// so a MISSING line means "the go directive's version". A present line that
// does not parse is an error: falling back there would hide real drift.
func parseGoMod(path string, lines []string) (goDir, toolchain pin, err error) {
	goDir, ok := findFirstIn(path, lines, reGoDirective)
	if !ok {
		return pin{}, pin{}, fmt.Errorf("%s: no `go X.Y[.Z]` directive", path)
	}
	if toolchain, ok = findFirstIn(path, lines, reToolchain); ok {
		return goDir, toolchain, nil
	}
	for i, l := range lines {
		if reToolchainKw.MatchString(l) {
			return pin{}, pin{}, fmt.Errorf("%s:%d %q: unparsable toolchain line (want `toolchain goX.Y[.Z][rcN]`)", path, i+1, strings.TrimSpace(l))
		}
	}
	toolchain = goDir
	toolchain.text += " (no toolchain line: it equals the go directive)"
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

// setupGoStep is one actions/setup-go step in a workflow and its `with:` keys.
type setupGoStep struct {
	line int
	with map[string]pin
}

var reIndent = regexp.MustCompile(`^(\s*)`)

func indentOf(l string) int { return len(reIndent.FindString(l)) }

// setupGoStepsIn finds every `uses: actions/setup-go@…` step (a tag or a
// digest-pinned `@<sha> # vX`) and collects the scalar keys of its `with:`
// block, comments stripped (a line-oriented parse of plain block YAML).
func setupGoStepsIn(path string, lines []string) []setupGoStep {
	var steps []setupGoStep
	for i, raw := range lines {
		l := stripYAMLComment(raw)
		if !strings.Contains(l, "uses: actions/setup-go@") {
			continue
		}
		st := setupGoStep{line: i + 1, with: map[string]pin{}}
		usesIndent := indentOf(strings.Replace(l, "- ", "  ", 1))
		for j := i + 1; j < len(lines); j++ {
			lj := stripYAMLComment(lines[j])
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

// pinProblems reports every way go.mod, the Dockerfile and ci.yml disagree on
// the Go minor, or fall below the supported floor. Empty means consistent.
func pinProblems(goMod, dockerfile, ci []string) []string {
	var probs []string
	goDir, toolchain, err := parseGoMod(goModPath, goMod)
	if err != nil {
		return append(probs, err.Error())
	}
	minor := minorOf(goDir.ver)
	if got := minorOf(toolchain.ver); got != minor {
		probs = append(probs, fmt.Sprintf("%s has Go %s, but %s declares minor %s", toolchain, got, goDir, minor))
	}
	if stage, err := parseBuildStage(dockerfilePath, dockerfile); err != nil {
		probs = append(probs, err.Error())
	} else if got := minorOf(stage.ver); got != minor {
		probs = append(probs, fmt.Sprintf("%s has Go %s, but %s declares minor %s", stage, got, goDir, minor))
	}
	steps := setupGoStepsIn(ciPath, ci)
	if len(steps) == 0 {
		probs = append(probs, ciPath+": no actions/setup-go step found")
	}
	for _, st := range steps {
		if lit, ok := st.with["go-version"]; ok {
			probs = append(probs, fmt.Sprintf("%s sets a literal go-version %s; go.mod (%s) says %s — use `go-version-file: go.mod`", lit, lit.ver, goDir, minor))
		}
		if f, ok := st.with["go-version-file"]; !ok || f.ver != "go.mod" {
			probs = append(probs, fmt.Sprintf("%s:%d: setup-go step has no `go-version-file: go.mod`", ciPath, st.line))
		}
	}
	parts := strings.SplitN(minor, ".", 2)
	if n, err := strconv.Atoi(parts[len(parts)-1]); err != nil || parts[0] != "1" || n < minGoMinor {
		probs = append(probs, fmt.Sprintf("%s declares Go %s; the oldest allowed minor is 1.%d", goDir, minor, minGoMinor))
	}
	return probs
}

func TestGoVersionPinsAgree(t *testing.T) {
	for _, p := range pinProblems(readLines(t, goModPath), readLines(t, dockerfilePath), readLines(t, ciPath)) {
		t.Error(p)
	}
}
