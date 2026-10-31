package herdr

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeApp is a tiny herdr-app script: it records its arguments and prints the
// stdout, stderr and exit code the test gives it. It never talks to a window.
type fakeApp struct {
	dir string
}

func newFakeApp(t *testing.T) (*fakeApp, *AppClient) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
dir=$(dirname "$0")
printf '%s\n' "$@" > "$dir/args"
[ -f "$dir/sleep" ] && sleep 5
cat "$dir/stdout"
cat "$dir/stderr" >&2
exit $(cat "$dir/code")
`
	if err := os.WriteFile(filepath.Join(dir, "herdr-app"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &fakeApp{dir: dir}
	app.answer(t, 0, "", "")
	return app, &AppClient{path: filepath.Join(dir, "herdr-app"), timeout: 2 * time.Second, status: time.Second}
}

func (f *fakeApp) answer(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	for name, content := range map[string]string{"stdout": stdout, "stderr": stderr, "code": string(rune('0' + code))} {
		if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fakeApp) args(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, "args"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestAppClientSendsOneEncodedObjectAndReadsTheReply(t *testing.T) {
	app, client := newFakeApp(t)
	app.answer(t, 0, `{"space":"sp1","tab":"tb1","terminal":"t2"}`, "")
	var created struct{ Space, Tab, Terminal string }
	// A name shaped like JSON stays a string: it is never a --key value.
	err := client.Do(context.Background(), "space.new", map[string]any{"name": "123", "folder": "/w/a b; rm -rf ~"}, &created)
	if err != nil || created.Space != "sp1" || created.Terminal != "t2" {
		t.Fatalf("created = %+v, %v", created, err)
	}
	if got, want := app.args(t), []string{"do", "space.new", `{"folder":"/w/a b; rm -rf ~","name":"123"}`}; !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestAppClientClassifiesEachOutcome(t *testing.T) {
	app, client := newFakeApp(t)
	cases := []struct {
		name           string
		code           int
		stdout, stderr string
		check          func(error) bool
	}{
		{"refused", 1, `{"error":"no space \"sp9\""}`, "", func(err error) bool {
			var apiError *APIError
			return errors.As(err, &apiError) && apiError.Message == `no space "sp9"`
		}},
		{"did not answer", 1, `{"error":"the window did not answer"}`, "", MutationMayHaveRun},
		{"no window", 2, "", "herdr-app: no Herdr window at /x/herdr-app.sock: Connection refused (os error 111)\n", func(err error) bool {
			return errors.Is(err, ErrAppWindowClosed)
		}},
		{"hung up", 2, "", "herdr-app: the window hung up\n", MutationMayHaveRun},
		{"unreadable", 0, "not json", "", MutationMayHaveRun},
		{"other exit", 3, "", "", MutationMayHaveRun},
	}
	for _, test := range cases {
		app.answer(t, test.code, test.stdout, test.stderr)
		var result map[string]any
		if err := client.Do(context.Background(), "space.rename", map[string]any{"id": "sp9", "name": ""}, &result); !test.check(err) {
			t.Fatalf("%s: err = %v", test.name, err)
		}
	}

	if err := os.WriteFile(filepath.Join(app.dir, "sleep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	client.timeout = 100 * time.Millisecond
	var result map[string]any
	if err := client.Do(context.Background(), "space.rename", map[string]any{"id": "sp9", "name": ""}, &result); !MutationMayHaveRun(err) || !MutationTimedOut(err) {
		t.Fatalf("timeout: err = %v", err)
	}

	missing := &AppClient{path: filepath.Join(app.dir, "missing"), timeout: time.Second}
	if err := missing.Do(context.Background(), "space.rename", map[string]any{}, &result); err == nil || MutationMayHaveRun(err) {
		t.Fatalf("missing binary: err = %v", err)
	}
}

func TestAppClientWindowOpenOnlyWhenStatusAnswers(t *testing.T) {
	app, client := newFakeApp(t)
	if !client.WindowOpen(context.Background()) || !reflect.DeepEqual(app.args(t), []string{"status"}) {
		t.Fatal("status exit 0 is not open")
	}
	app.answer(t, 2, "", "herdr-app: no Herdr window at /x/herdr-app.sock: Connection refused (os error 111)\n")
	if client.WindowOpen(context.Background()) {
		t.Fatal("status exit 2 is open")
	}

	// A window that doesn't answer counts as closed after the status limit,
	// well before the command limit.
	app.answer(t, 0, "", "")
	if err := os.WriteFile(filepath.Join(app.dir, "sleep"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	client.timeout, client.status = time.Minute, 200*time.Millisecond
	started := time.Now()
	if client.WindowOpen(context.Background()) {
		t.Fatal("a hung status is open")
	}
	// The fake's sleep keeps its output open, so WaitDelay adds up to 1 s.
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("a hung status took %s", elapsed)
	}
}

func TestNewAppClientGatesVersionAndCommands(t *testing.T) {
	app, client := newFakeApp(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	commands := `[{"name":"space.new","args":[{"name":"name"},{"name":"folder"},{"name":"tab"}]},` +
		`{"name":"pane.split","args":[{"name":"direction"},{"name":"pane"}]},` +
		`{"name":"space.rename","args":[{"name":"id"},{"name":"name"}]}]`
	script := `#!/bin/sh
case "$1" in
  --version) echo "herdr-app $(cat "$(dirname "$0")/version")" ;;
  commands) cat "$(dirname "$0")/commands" ;;
  *) exit 9 ;;
esac
`
	if err := os.WriteFile(client.path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(app.dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("version", "0.1.10")
	write("commands", commands)
	if _, err := NewAppClient(context.Background(), client.path, logger); err != nil {
		t.Fatalf("validated app: %v", err)
	}
	write("version", "0.1.11")
	if _, err := NewAppClient(context.Background(), client.path, logger); err != nil {
		t.Fatalf("newer app: %v", err)
	}
	write("version", "0.1.9")
	if _, err := NewAppClient(context.Background(), client.path, logger); err == nil {
		t.Fatal("older app passed the gate")
	}
	write("version", "0.1.10")
	write("commands", strings.Replace(commands, `{"name":"pane"}`, `{"name":"target"}`, 1))
	if _, err := NewAppClient(context.Background(), client.path, logger); err == nil || !strings.Contains(err.Error(), "pane.split") {
		t.Fatalf("missing argument: %v", err)
	}
	write("commands", strings.Replace(commands, `"space.rename"`, `"space.retitle"`, 1))
	if _, err := NewAppClient(context.Background(), client.path, logger); err == nil || !strings.Contains(err.Error(), "space.rename") {
		t.Fatalf("missing command: %v", err)
	}
}
