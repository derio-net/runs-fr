package pins

import "testing"

// TestParseGoMod covers the go.mod shapes cmd/go and Renovate (with gomodTidy)
// actually produce. cmd/go drops a `toolchain` line equal to the go directive,
// so a consistent go.mod may have none.
func TestParseGoMod(t *testing.T) {
	cases := []struct {
		name          string
		lines         []string
		wantGo, wantT string
		wantErr       bool
	}{
		{"go and toolchain", []string{"module m", "", "go 1.27.0", "", "toolchain go1.27.4"}, "1.27.0", "1.27.4", false},
		{"toolchain omitted (equals go)", []string{"module m", "", "go 1.27.4"}, "1.27.4", "1.27.4", false},
		{"no go directive", []string{"module m", "toolchain go1.27.4"}, "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			goDir, tc, err := parseGoMod("go.mod", c.lines)
			if c.wantErr {
				if err == nil {
					t.Fatalf("want an error, got go=%s toolchain=%s", goDir.ver, tc.ver)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if goDir.ver != c.wantGo || tc.ver != c.wantT {
				t.Errorf("got go=%s toolchain=%s, want go=%s toolchain=%s", goDir.ver, tc.ver, c.wantGo, c.wantT)
			}
		})
	}
}

// TestParseBuildStage covers the tag shapes Renovate's dockerfile manager may
// write, digest pinning included.
func TestParseBuildStage(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		want    string
		wantErr bool
	}{
		{"minor tag", "FROM golang:1.27-alpine AS build", "1.27", false},
		{"patch tag", "FROM golang:1.27.4-alpine AS build", "1.27.4", false},
		{"digest pinned", "FROM golang:1.27-alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef AS build", "1.27", false},
		{"not the build stage", "FROM golang:1.27-alpine AS tools", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := parseBuildStage("Dockerfile", []string{"# syntax=docker/dockerfile:1", c.line})
			if c.wantErr {
				if err == nil {
					t.Fatalf("want an error, got %s", p.ver)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.ver != c.want {
				t.Errorf("got %s, want %s", p.ver, c.want)
			}
		})
	}
}
