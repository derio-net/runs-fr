package pins

import (
	"regexp"
	"strings"
	"testing"
)

// block is a contiguous run of lines under one YAML key, with 1-based line
// numbers kept so failures can point at the file.
type block struct {
	key   string
	line  int
	lines []string
}

// text joins the block's lines for substring checks.
func (b block) text() string { return strings.Join(b.lines, "\n") }

// childBlocks splits the lines nested under `parent:` (a top-level key) into
// one block per direct child key. A line-oriented parse: ci.yml is plain block
// YAML, and this keeps the test stdlib-only.
func childBlocks(t *testing.T, path, parent string) map[string]block {
	t.Helper()
	lines := readLines(t, path)
	out := map[string]block{}
	in := false
	childIndent := -1
	var cur *block
	flush := func() {
		if cur != nil {
			out[cur.key] = *cur
			cur = nil
		}
	}
	for i, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			if cur != nil {
				cur.lines = append(cur.lines, l)
			}
			continue
		}
		ind := indentOf(l)
		if ind == 0 {
			flush()
			in = strings.HasPrefix(l, parent+":")
			// An inline value (`on: [push]`) has no children to split.
			continue
		}
		if !in {
			continue
		}
		if childIndent < 0 {
			childIndent = ind
		}
		if ind == childIndent {
			flush()
			key, _, _ := strings.Cut(trimmed, ":")
			cur = &block{key: key, line: i + 1}
		}
		if cur != nil {
			cur.lines = append(cur.lines, l)
		}
	}
	flush()
	return out
}

var (
	reGovulncheckPinned = regexp.MustCompile(`golang\.org/x/vuln/cmd/govulncheck@v\d+\.\d+\.\d+\b`)
	reGoTestRun         = regexp.MustCompile(`run:\s*go test\b.*`)
	reIfLine            = regexp.MustCompile(`(?m)^\s{4}if:\s*(.+)$`)
)

func TestCIGates(t *testing.T) {
	triggers := childBlocks(t, ciPath, "on")
	jobs := childBlocks(t, ciPath, "jobs")

	job := func(t *testing.T, name string) block {
		t.Helper()
		b, ok := jobs[name]
		if !ok {
			t.Fatalf("%s: no `%s` job (jobs: %v)", ciPath, name, keys(jobs))
		}
		return b
	}

	t.Run("on: includes schedule and workflow_dispatch", func(t *testing.T) {
		for _, k := range []string{"push", "pull_request", "schedule", "workflow_dispatch"} {
			if _, ok := triggers[k]; !ok {
				t.Errorf("%s: `on:` has no `%s` trigger (has %v)", ciPath, k, keys(triggers))
			}
		}
	})

	t.Run("govulncheck job is a pinned hard gate", func(t *testing.T) {
		b := job(t, "govulncheck")
		txt := b.text()
		if !reGovulncheckPinned.MatchString(txt) {
			t.Errorf("%s:%d: govulncheck job does not run golang.org/x/vuln/cmd/govulncheck@v<semver>", ciPath, b.line)
		}
		if strings.Contains(txt, "govulncheck@latest") {
			t.Errorf("%s:%d: govulncheck job runs @latest; pin a version", ciPath, b.line)
		}
		if strings.Contains(txt, "continue-on-error") {
			t.Errorf("%s:%d: govulncheck job sets continue-on-error; it must fail the workflow", ciPath, b.line)
		}
	})

	t.Run("go job checks tidy and tests with -race -cover", func(t *testing.T) {
		b := job(t, "go")
		txt := b.text()
		if !strings.Contains(txt, "go mod tidy -diff") {
			t.Errorf("%s:%d: go job has no `go mod tidy -diff` step", ciPath, b.line)
		}
		run := reGoTestRun.FindString(txt)
		if run == "" {
			t.Fatalf("%s:%d: go job has no `run: go test …` step", ciPath, b.line)
		}
		for _, flag := range []string{"-race", "-cover"} {
			if !strings.Contains(run, flag) {
				t.Errorf("%s:%d: go job's test step %q lacks %s", ciPath, b.line, run, flag)
			}
		}
	})

	t.Run("image job builds without pushing", func(t *testing.T) {
		b := job(t, "image")
		txt := b.text()
		if !strings.Contains(txt, "docker/build-push-action@") {
			t.Errorf("%s:%d: image job does not use docker/build-push-action", ciPath, b.line)
		}
		if !strings.Contains(txt, "push: false") {
			t.Errorf("%s:%d: image job's build does not set `push: false`", ciPath, b.line)
		}
	})

	t.Run("new jobs share the go job's authority gate", func(t *testing.T) {
		want := reIfLine.FindStringSubmatch(job(t, "go").text())
		if want == nil {
			t.Fatalf("%s: go job has no job-level `if:`", ciPath)
		}
		for _, name := range []string{"govulncheck", "image", "helm"} {
			b := job(t, name)
			got := reIfLine.FindStringSubmatch(b.text())
			if got == nil || got[1] != want[1] {
				t.Errorf("%s:%d: %s job's `if:` %q differs from the go job's %q", ciPath, b.line, name, first(got), want[1])
			}
		}
	})
}

func keys(m map[string]block) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func first(m []string) string {
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
