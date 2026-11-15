package pins

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// mutation is one edit applied to a LIVE repo file. The fixtures are the real
// go.mod, Dockerfile and ci.yml, never hand-built copies, so a check that
// passes on the repo and fails on its mutation is proven to read that file.
type mutation struct {
	name     string
	file     string // goModPath | dockerfilePath | ciPath
	old, new string
	// ok: the mutated repo is still consistent and the check must pass
	// (a shape Renovate or cmd/go legitimately writes).
	ok bool
}

// liveFiles reads the three pinned files, keyed by path.
func liveFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, p := range []string{goModPath, dockerfilePath, ciPath} {
		out[p] = strings.Join(readLines(t, p), "\n")
	}
	return out
}

// apply returns files with m applied, failing when m.old is not in the live
// file — a mutation that no longer applies would pass vacuously.
func (m mutation) apply(t *testing.T, files map[string]string) map[string][]string {
	t.Helper()
	if !strings.Contains(files[m.file], m.old) {
		t.Fatalf("mutation %q: %q not found in %s; update the mutation", m.name, m.old, m.file)
	}
	out := map[string][]string{}
	for p, s := range files {
		if p == m.file {
			s = strings.Replace(s, m.old, m.new, 1)
		}
		out[p] = strings.Split(s, "\n")
	}
	return out
}

func TestPinProblemsCatchDrift(t *testing.T) {
	live := liveFiles(t)
	cases := []mutation{
		{name: "go directive moves a minor alone", file: goModPath, old: "go 1.27.0", new: "go 1.28.0"},
		{name: "toolchain moves a minor alone", file: goModPath, old: "toolchain go1.27.4", new: "toolchain go1.28.1"},
		{name: "rc toolchain on a newer minor", file: goModPath, old: "toolchain go1.27.4", new: "toolchain go1.28rc1"},
		{name: "commented toolchain on a newer minor", file: goModPath, old: "toolchain go1.27.4", new: "toolchain go1.28.1 // pinned"},
		{name: "unparsable toolchain line", file: goModPath, old: "toolchain go1.27.4", new: "toolchain  go1.28.1-custom"},
		{name: "Dockerfile stage on another minor", file: dockerfilePath, old: "golang:1.27-alpine", new: "golang:1.26-alpine"},
		{name: "literal go-version in ci.yml", file: ciPath, old: "go-version-file: go.mod", new: `go-version: "1.27"`},
		{name: "go-version-file commented out", file: ciPath, old: "go-version-file: go.mod", new: "# go-version-file: go.mod"},
		{name: "go.mod below the supported floor", file: goModPath, old: "go 1.27.0\n\ntoolchain go1.27.4", new: "go 1.26.0\n\ntoolchain go1.26.9"},

		{name: "toolchain line dropped (equals go)", file: goModPath, old: "go 1.27.0\n\ntoolchain go1.27.4", new: "go 1.27.4", ok: true},
		{name: "toolchain patch with trailing comment", file: goModPath, old: "toolchain go1.27.4", new: "toolchain go1.27.5 // security", ok: true},
		{name: "digest-pinned golang stage", file: dockerfilePath, old: "golang:1.27-alpine", new: "golang:1.27-alpine@sha256:" + strings.Repeat("ab", 32), ok: true},
		{name: "digest-pinned setup-go", file: ciPath, old: "uses: actions/setup-go@v5", new: "uses: actions/setup-go@" + strings.Repeat("c", 40) + " # v5.6.0", ok: true},
	}
	for _, m := range cases {
		t.Run(m.name, func(t *testing.T) {
			f := m.apply(t, live)
			probs := pinProblems(f[goModPath], f[dockerfilePath], f[ciPath])
			if m.ok && len(probs) > 0 {
				t.Errorf("consistent repo reported as drift: %v", probs)
			}
			if !m.ok && len(probs) == 0 {
				t.Errorf("drift not detected")
			}
		})
	}
}

func TestCIGateProblemsCatchMutations(t *testing.T) {
	live := liveFiles(t)
	cases := []mutation{
		{name: "tidy step turned into a no-op", file: ciPath, old: "run: go mod tidy -diff", new: "run: true # go mod tidy -diff"},
		{name: "persist-credentials commented out", file: ciPath, old: "persist-credentials: false", new: "# persist-credentials: false"},
		{name: "scan exit output turned into a no-op", file: ciPath, old: `echo "exit=$rc" >> "$GITHUB_OUTPUT"`, new: `: # exit= GITHUB_OUTPUT`},
		{name: "image push: false commented out", file: ciPath, old: "push: false", new: "# push: false"},
		{name: "test flags moved into a comment", file: ciPath, old: "run: go test ./... -race -cover", new: "run: go test ./... # -race -cover"},
		{name: "scan always exits 0", file: ciPath, old: `exit "$rc"`, new: "exit 0"},
		{name: "scan failure swallowed", file: ciPath, old: `"$(go env GOPATH)/bin/govulncheck" ./...`, new: `"$(go env GOPATH)/bin/govulncheck" ./... || true`},
		{name: "schedule left with no cron", file: ciPath, old: `    - cron: "0 6 * * 1" # Monday 06:00 UTC`, new: ""},
		{name: "issue job runs on every event", file: ciPath, old: "if: failure() && github.event_name == 'schedule' && github.server_url == 'https://github.com'", new: "if: failure() # github.event_name == 'schedule' && github.server_url == 'https://github.com'"},
		{name: "scan job regains issues: write", file: ciPath, old: "    permissions:\n      contents: read\n    outputs:", new: "    permissions:\n      contents: read\n      issues: write\n    outputs:"},
	}
	for _, m := range cases {
		t.Run(m.name, func(t *testing.T) {
			f := m.apply(t, live)
			if probs := ciGateProblems(f[ciPath]); len(probs) == 0 {
				t.Errorf("weakened gate not detected")
			}
		})
	}
}

// TestRenovateGolangciRegexMatches runs renovate.json's own golangci-lint
// regex manager against ci.yml, and against the line digest pinning writes
// (`uses: …@<sha> # v9`): a regex that matches nothing fails silently in
// Renovate and drops the linter out of the go toolchain group.
func TestRenovateGolangciRegexMatches(t *testing.T) {
	raw, err := os.ReadFile("../../renovate.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		CustomManagers []struct {
			DepNameTemplate string   `json:"depNameTemplate"`
			MatchStrings    []string `json:"matchStrings"`
		} `json:"customManagers"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	var pattern string
	for _, m := range cfg.CustomManagers {
		if m.DepNameTemplate == "golangci/golangci-lint" && len(m.MatchStrings) > 0 {
			pattern = m.MatchStrings[0]
		}
	}
	if pattern == "" {
		t.Fatal("renovate.json has no golangci/golangci-lint regex manager")
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("regex does not compile in Go: %v", err)
	}
	ci := liveFiles(t)[ciPath]
	digest := strings.Replace(ci, "golangci-lint-action@v9", "golangci-lint-action@"+strings.Repeat("d", 40)+" # v9.2.0", 1)
	for name, text := range map[string]string{"ci.yml as is": ci, "digest-pinned action": digest} {
		t.Run(name, func(t *testing.T) {
			m := re.FindStringSubmatch(text)
			i := re.SubexpIndex("currentValue")
			if m == nil || i < 0 || m[i] != "v2.15.0" {
				t.Errorf("regex %q does not extract currentValue v2.15.0", pattern)
			}
		})
	}
}
