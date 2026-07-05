package localterm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/derio-net/runs-fr/internal/runs"
)

// fakeTmux records invocations and scripts responses per leading subcommand
// (after any -S socket prefix, which it strips and records separately).
type fakeTmux struct {
	calls   [][]string
	sockets []string
	respond func(args []string) ([]byte, error)
}

func (f *fakeTmux) run(_ context.Context, args ...string) ([]byte, error) {
	socket := ""
	if len(args) >= 2 && args[0] == "-S" {
		socket = args[1]
		args = args[2:]
	}
	f.sockets = append(f.sockets, socket)
	f.calls = append(f.calls, args)
	return f.respond(args)
}

// exitErr mimics tmux exiting nonzero: the lister must classify by output,
// not by error type internals.
var exitErr = errors.New("exit status 1")

func TestListSortsAndTags(t *testing.T) {
	ft := &fakeTmux{respond: func(args []string) ([]byte, error) {
		return []byte("run-b\nrun-a\n"), nil
	}}
	l := NewLister("", "default")
	l.runTmux = ft.run

	got, err := l.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []runs.Run{
		{ID: "run-a", Status: "Running", Runner: "default"},
		{ID: "run-b", Status: "Running", Runner: "default"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %#v\nwant %#v", got, want)
	}
	if !reflect.DeepEqual(ft.calls[0], []string{"ls", "-F", "#{session_name}"}) {
		t.Errorf("List() tmux args = %v", ft.calls[0])
	}
}

func TestListNoServerIsEmpty(t *testing.T) {
	// tmux exits 1 with "no server running on ..." when the last session has
	// ended — that is zero runs, not an error.
	ft := &fakeTmux{respond: func([]string) ([]byte, error) {
		return []byte("no server running on /tmp/tmux-1000/default"), exitErr
	}}
	l := NewLister("", "")
	l.runTmux = ft.run

	got, err := l.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v, want nil for no-server", err)
	}
	if len(got) != 0 {
		t.Errorf("List() = %v, want empty", got)
	}
}

func TestListRealErrorWrapped(t *testing.T) {
	ft := &fakeTmux{respond: func([]string) ([]byte, error) {
		return []byte("error connecting to /x (permission denied)"), exitErr
	}}
	l := NewLister("", "")
	l.runTmux = ft.run

	if _, err := l.List(context.Background()); err == nil {
		t.Fatal("List() expected error for non-no-server failure")
	}
}

func TestResolveExactMatch(t *testing.T) {
	ft := &fakeTmux{respond: func(args []string) ([]byte, error) {
		if args[0] == "has-session" && args[2] == "=run-a" {
			return nil, nil
		}
		return []byte("can't find session"), exitErr
	}}
	l := NewLister("", "")
	l.runTmux = ft.run

	got, err := l.Resolve(context.Background(), "run-a")
	if err != nil {
		t.Fatalf("Resolve(run-a) error = %v", err)
	}
	if got != "run-a" {
		t.Errorf("Resolve(run-a) = %q, want the session name itself", got)
	}
	if !reflect.DeepEqual(ft.calls[0], []string{"has-session", "-t", "=run-a"}) {
		t.Errorf("Resolve tmux args = %v", ft.calls[0])
	}

	if _, err := l.Resolve(context.Background(), "run-absent"); !errors.Is(err, runs.ErrNotFound) {
		t.Errorf("Resolve(absent) error = %v, want runs.ErrNotFound", err)
	}
}

func TestSocketFlagPrefixed(t *testing.T) {
	ft := &fakeTmux{respond: func([]string) ([]byte, error) { return []byte(""), nil }}
	l := NewLister("/run/tmux/cnc.sock", "")
	l.runTmux = ft.run

	if _, err := l.List(context.Background()); err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if ft.sockets[0] != "/run/tmux/cnc.sock" {
		t.Errorf("List() socket = %q, want /run/tmux/cnc.sock", ft.sockets[0])
	}
}

func TestListSkipsBlankLines(t *testing.T) {
	ft := &fakeTmux{respond: func([]string) ([]byte, error) {
		return []byte(fmt.Sprintf("run-a\n\n%s\n", "")), nil
	}}
	l := NewLister("", "")
	l.runTmux = ft.run

	got, err := l.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != "run-a" {
		t.Errorf("List() = %v, want just run-a", got)
	}
}
