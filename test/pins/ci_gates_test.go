package pins

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// block is a contiguous run of lines under one YAML key, comments stripped,
// with the 1-based line number of its key kept so failures can point at it.
type block struct {
	key   string
	line  int
	lines []string
}

// text joins the block's lines for the anchored checks below.
func (b block) text() string { return strings.Join(b.lines, "\n") }

// childBlocksIn splits the lines nested under `parent:` (a top-level key) into
// one block per direct child key. Comments are stripped first, so a commented
// out key or a value moved into a `# …` tail does not count as present.
func childBlocksIn(lines []string, parent string) map[string]block {
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
	for i, raw := range lines {
		l := stripYAMLComment(raw)
		if strings.TrimSpace(l) == "" {
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
			key, _, _ := strings.Cut(strings.TrimSpace(l), ":")
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
	reGoTestRun         = regexp.MustCompile(`(?m)^\s+run:\s*go test \./\.\.\.(.*)$`)
	reTidyRun           = regexp.MustCompile(`(?m)^\s+run:\s*go mod tidy -diff\s*$`)
	reJobIf             = regexp.MustCompile(`(?m)^\s{4}if:\s*(.+)$`)
	reCron              = regexp.MustCompile(`(?m)^\s+- cron:\s*"[^"]+"\s*$`)
	rePersistFalse      = regexp.MustCompile(`(?m)^\s+persist-credentials:\s*false\s*$`)
	rePushFalse         = regexp.MustCompile(`(?m)^\s+push:\s*false\s*$`)
	reIssuesWrite       = regexp.MustCompile(`(?m)^\s+issues:\s*write\s*$`)
	reNeedsGovulncheck  = regexp.MustCompile(`(?m)^\s+needs:\s*govulncheck\s*$`)
	// The scan step: run the installed binary, capture its exit code at once,
	// publish it, and exit with it — the hard gate R5 and the output R6 reads.
	reScanPropagates = regexp.MustCompile(`"\$\(go env GOPATH\)/bin/govulncheck" \./\.\.\.\s*\n\s*rc=\$\?\s*\n\s*echo "exit=\$rc" >> "\$GITHUB_OUTPUT"\s*\n\s*exit "\$rc"`)
	reSwallowed      = regexp.MustCompile(`\|\|\s*true|(?m)^\s*exit 0\s*$`)
)

// ciGateProblems reports every way ci.yml fails to carry the deps-gate CI
// gates (spec R3, R5, R6, R9, R11). Empty means every gate is in place.
func ciGateProblems(lines []string) []string {
	var probs []string
	bad := func(format string, a ...any) { probs = append(probs, fmt.Sprintf(format, a...)) }
	triggers := childBlocksIn(lines, "on")
	jobs := childBlocksIn(lines, "jobs")
	job := func(name string) (block, bool) {
		b, ok := jobs[name]
		if !ok {
			bad("%s: no `%s` job (jobs: %v)", ciPath, name, keys(jobs))
		}
		return b, ok
	}
	jobIf := func(b block) string {
		if m := reJobIf.FindStringSubmatch(b.text()); m != nil {
			return m[1]
		}
		return ""
	}

	for _, k := range []string{"push", "pull_request", "schedule", "workflow_dispatch"} {
		if _, ok := triggers[k]; !ok {
			bad("%s: `on:` has no `%s` trigger (has %v)", ciPath, k, keys(triggers))
		}
	}
	if s, ok := triggers["schedule"]; ok && !reCron.MatchString(s.text()) {
		bad("%s:%d: `schedule:` has no `- cron: \"…\"` entry, so the weekly scan never runs", ciPath, s.line)
	}

	if b, ok := job("govulncheck"); ok {
		txt := b.text()
		if !reGovulncheckPinned.MatchString(txt) || strings.Contains(txt, "govulncheck@latest") {
			bad("%s:%d: govulncheck job does not install a pinned golang.org/x/vuln/cmd/govulncheck@v<semver>", ciPath, b.line)
		}
		if strings.Contains(txt, "continue-on-error") {
			bad("%s:%d: govulncheck job sets continue-on-error; it must fail the workflow", ciPath, b.line)
		}
		// `go run` reports any non-zero child exit as 1, hiding govulncheck's
		// exit 3 (vulnerabilities found) behind its exit 1 (tool error).
		if strings.Contains(txt, "go run golang.org/x/vuln/cmd/govulncheck") {
			bad("%s:%d: govulncheck job uses `go run`, which masks govulncheck's exit code", ciPath, b.line)
		}
		if !reScanPropagates.MatchString(txt) {
			bad("%s:%d: govulncheck scan step does not run the installed binary, publish `exit=$rc` and `exit \"$rc\"`", ciPath, b.line)
		}
		if reSwallowed.MatchString(txt) {
			bad("%s:%d: govulncheck job swallows a failure (`|| true` or `exit 0`)", ciPath, b.line)
		}
		// Least privilege: the scan runs on every PR and push.
		if reIssuesWrite.MatchString(txt) {
			bad("%s:%d: govulncheck job holds issues: write; only govulncheck-issue may", ciPath, b.line)
		}
		if !rePersistFalse.MatchString(txt) {
			bad("%s:%d: govulncheck job's checkout persists the token; set persist-credentials: false", ciPath, b.line)
		}
	}

	if b, ok := job("govulncheck-issue"); ok {
		txt := b.text()
		cond := jobIf(b)
		for _, want := range []string{"failure()", "github.event_name == 'schedule'", "github.server_url == 'https://github.com'"} {
			if !strings.Contains(cond, want) {
				bad("%s:%d: govulncheck-issue job's `if:` %q lacks %q", ciPath, b.line, cond, want)
			}
		}
		if !reNeedsGovulncheck.MatchString(txt) {
			bad("%s:%d: govulncheck-issue job does not `needs: govulncheck`", ciPath, b.line)
		}
		if !reIssuesWrite.MatchString(txt) {
			bad("%s:%d: govulncheck-issue job has no `issues: write` permission", ciPath, b.line)
		}
		if !strings.Contains(txt, "needs.govulncheck.outputs.exit") {
			bad("%s:%d: govulncheck-issue job does not read the scan's exit code", ciPath, b.line)
		}
		// Exit 3 is "vulnerabilities found"; anything else is the job failing.
		if !strings.Contains(txt, "reachable vulnerability on main") || !strings.Contains(txt, "scan failed on main") {
			bad("%s:%d: govulncheck-issue job does not title a vulnerability apart from a failed scan", ciPath, b.line)
		}
		if strings.Contains(txt, "contents: write") {
			bad("%s:%d: govulncheck-issue job needs no contents: write", ciPath, b.line)
		}
	}

	if b, ok := job("go"); ok {
		txt := b.text()
		if !reTidyRun.MatchString(txt) {
			bad("%s:%d: go job has no `run: go mod tidy -diff` step", ciPath, b.line)
		}
		m := reGoTestRun.FindStringSubmatch(txt)
		if m == nil {
			bad("%s:%d: go job has no `run: go test ./...` step", ciPath, b.line)
		} else {
			for _, flag := range []string{"-race", "-cover"} {
				if !strings.Contains(m[1], flag) {
					bad("%s:%d: go job's test step lacks %s", ciPath, b.line, flag)
				}
			}
		}
	}

	if b, ok := job("image"); ok {
		txt := b.text()
		if !strings.Contains(txt, "docker/build-push-action@") {
			bad("%s:%d: image job does not use docker/build-push-action", ciPath, b.line)
		}
		if !rePushFalse.MatchString(txt) {
			bad("%s:%d: image job's build does not set `push: false`", ciPath, b.line)
		}
	}

	// Every job that runs on both forges shares the go job's authority gate.
	if g, ok := job("go"); ok {
		want := jobIf(g)
		if want == "" {
			bad("%s: go job has no job-level `if:`", ciPath)
		}
		for _, name := range []string{"govulncheck", "image", "helm"} {
			if b, ok := job(name); ok && jobIf(b) != want {
				bad("%s:%d: %s job's `if:` %q differs from the go job's %q", ciPath, b.line, name, jobIf(b), want)
			}
		}
	}
	return probs
}

func TestCIGates(t *testing.T) {
	for _, p := range ciGateProblems(readLines(t, ciPath)) {
		t.Error(p)
	}
}

func keys(m map[string]block) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
