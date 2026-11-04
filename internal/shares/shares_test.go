package shares

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luiscleto/shepherdr/internal/uploads"
)

// shortDir keeps socket paths under the Unix socket length limit, which a
// test's own temporary folder can pass.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "sh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func openStore(t *testing.T, dir string, limit, total int64) *Store {
	t.Helper()
	store, err := Open(dir, uploads.Limit{Bytes: limit}, uploads.Limit{Bytes: total}, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func put(t *testing.T, store *Store, name, content string, source *Source) Record {
	t.Helper()
	incoming, err := store.Begin(name, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	if err := incoming.Copy(context.Background(), strings.NewReader(content)); err != nil {
		t.Fatal(err)
	}
	record, err := incoming.Commit(source)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, entry := range list {
		names = append(names, entry.Name())
	}
	return names
}

func TestStoreKeepsCopiesAcrossRestartAndClearsLeftovers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	store := openStore(t, dir, 1<<20, 1<<20)
	first := put(t, store, "report.md", "# hi\n", &Source{Herdr: "core", PaneID: "t1", TerminalID: "c:t1", PID: 5, Start: 9, Place: "site"})
	second := put(t, store, "report.md", "again", nil)
	if first.ID == second.ID || first.Name != "report.md" || second.Name != "report.md" {
		t.Fatalf("duplicate names = %+v %+v", first, second)
	}
	info, err := os.Stat(filepath.Join(dir, first.ID, first.Name))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("copy mode = %v, %v", info, err)
	}
	store.Close()

	// An interrupted share, a lost file and a folder the index never named.
	if err := os.WriteFile(filepath.Join(dir, incomingName, "partial"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, second.ID, second.Name)); err != nil {
		t.Fatal(err)
	}
	orphan := strings.Repeat("ab", 16)
	if err := os.Mkdir(filepath.Join(dir, orphan), 0o700); err != nil {
		t.Fatal(err)
	}
	// An index write that stopped before its rename.
	if err := os.WriteFile(filepath.Join(dir, ".shared-123"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened := openStore(t, dir, 1<<20, 1<<20)
	list := reopened.List()
	if len(list) != 1 || list[0].ID != first.ID || list[0].Source == nil || list[0].Source.Place != "site" {
		t.Fatalf("after restart = %+v", list)
	}
	if got := entries(t, filepath.Join(dir, incomingName)); len(got) != 0 {
		t.Fatalf(".incoming = %v", got)
	}
	if _, err := os.Stat(filepath.Join(dir, orphan)); !os.IsNotExist(err) {
		t.Fatalf("orphan folder kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".shared-123")); !os.IsNotExist(err) {
		t.Fatalf("temporary index kept: %v", err)
	}
}

func TestStoreRefusesBeforeStoringAndAbortLeavesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	store := openStore(t, dir, 10, 15)
	if _, err := store.Begin("big", 11); err != ErrTooLarge {
		t.Fatalf("too large = %v", err)
	}
	put(t, store, "a", "0123456789", nil)
	_, err := store.Begin("b", 6)
	full, ok := err.(*FullError)
	if !ok || full.Kept != 10 || full.Total != 15 {
		t.Fatalf("full = %v", err)
	}
	incoming, err := store.Begin("c", 5)
	if err != nil {
		t.Fatal(err)
	}
	// The reservation counts while the bytes arrive.
	if _, err := store.Begin("d", 1); err == nil {
		t.Fatal("reserved bytes were not counted")
	}
	if err := incoming.Copy(context.Background(), strings.NewReader("12")); err != io.ErrUnexpectedEOF {
		t.Fatalf("short copy = %v", err)
	}
	incoming.Abort()
	if got := entries(t, filepath.Join(dir, incomingName)); len(got) != 0 {
		t.Fatalf(".incoming after abort = %v", got)
	}
	if len(store.List()) != 1 {
		t.Fatal("aborted share was listed")
	}
	if _, err := store.Begin("e", 5); err != nil {
		t.Fatalf("reservation not released: %v", err)
	}
}

func TestStoreDeletesExactlyTheListedFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	store := openStore(t, dir, 100, 100)
	a := put(t, store, "a.txt", "a", nil)
	b := put(t, store, "b.txt", "b", nil)
	missing := strings.Repeat("cd", 16)
	deleted, gone, err := store.Delete([]string{a.ID, missing})
	if err != nil || len(deleted) != 1 || deleted[0] != a.ID || len(gone) != 1 || gone[0] != missing {
		t.Fatalf("delete = %v %v %v", deleted, gone, err)
	}
	if _, _, ok := store.Open(a.ID); ok {
		t.Fatal("deleted file still opens")
	}
	if _, err := os.Stat(filepath.Join(dir, a.ID)); !os.IsNotExist(err) {
		t.Fatal("deleted folder kept")
	}
	if file, _, ok := store.Open(b.ID); !ok {
		t.Fatal("other file was deleted")
	} else {
		file.Close()
	}
}

func TestHostileNamesBecomeSafeSeparateCopies(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	store := openStore(t, dir, 1<<20, 1<<20)
	names := map[string]string{
		"../../etc/passwd":                "passwd",
		"evil\u202egnp.exe":               "evilgnp.exe",
		"a\x1b[31mred\x07.txt":            "a[31mred.txt",
		"$(rm -rf ~)`x`;|&.sh":            "rm -rf ~x.sh",
		"..":                              "file",
		strings.Repeat("é", 300) + ".png": strings.Repeat("é", 98) + ".png",
	}
	for input, want := range names {
		record := put(t, store, input, "x", nil)
		if record.Name != want {
			t.Errorf("%q stored as %q, want %q", input, record.Name, want)
		}
		if strings.ContainsAny(record.Name, "/\x00") {
			t.Errorf("unsafe stored name %q", record.Name)
		}
	}
	twice := put(t, store, "../../etc/passwd", "y", nil)
	count := 0
	for _, record := range store.List() {
		if record.Name == "passwd" {
			count++
		}
	}
	if count != 2 || twice.Name != "passwd" {
		t.Fatalf("duplicate hostile names kept %d copies", count)
	}
	// Nothing escaped the store's folder.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "etc")); !os.IsNotExist(err) {
		t.Fatal("a name escaped the store")
	}
}

func TestViewsMarkListedFilesSeen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	store := openStore(t, dir, 100, 100)
	source := &Source{Herdr: "tui", PaneID: "w1:p1", TerminalID: "term_a", PID: 7, Start: 1}
	other := &Source{Herdr: "tui", PaneID: "w1:p2", TerminalID: "term_b", PID: 8, Start: 1}
	put(t, store, "a", "a", source)
	put(t, store, "b", "b", other)
	owner := new(byte)
	store.SetView(owner, &View{Handle: source.Handle()})
	seen := map[string]bool{}
	for _, record := range store.List() {
		seen[record.Name] = record.Seen
	}
	if !seen["a"] || seen["b"] {
		t.Fatalf("terminal view marked %v", seen)
	}
	// While the view stays, a new file from that terminal is seen at once.
	c := put(t, store, "c", "c", source)
	d := put(t, store, "d", "d", nil)
	if !c.Seen || d.Seen {
		t.Fatalf("arrivals: c=%v d=%v", c.Seen, d.Seen)
	}
	store.SetView(owner, nil)
	e := put(t, store, "e", "e", source)
	if e.Seen {
		t.Fatal("a left view still marked files")
	}
	// A view by current IDs needs the terminal to be current.
	store.SetCurrent(func(s Source) bool { return s.PaneID == "w1:p2" })
	store.SetView(owner, &View{Herdr: "tui", PaneID: "w1:p2", TerminalID: "term_b"})
	for _, record := range store.List() {
		if record.Name == "b" && !record.Seen {
			t.Fatal("view by IDs didn't mark its terminal's file")
		}
	}
	store.Close()
	reopened := openStore(t, dir, 100, 100)
	for _, record := range reopened.List() {
		if record.Name == "a" && !record.Seen {
			t.Fatal("seen was not kept")
		}
	}
}

func TestSourceKeyNeverMatchesALaterTerminalWithTheSameIDs(t *testing.T) {
	first := Source{Herdr: "tui", PaneID: "w1:p1", TerminalID: "term_1", PID: 100, Start: 5000}
	later := first
	later.PID, later.Start = 200, 9000
	if first.Key() == later.Key() || first.Handle() == later.Handle() {
		t.Fatal("a later terminal with the same IDs got the same key")
	}
	if Running(first) {
		t.Fatal("a process that never ran counts as running")
	}
	_, start, err := processFacts(os.Getpid())
	if err != nil || !Running(Source{PID: os.Getpid(), Start: start}) {
		t.Fatalf("this process isn't running: %v", err)
	}
}

func TestListenRefusesAnOpenFolderAndReplacesOnlyAStaleSocket(t *testing.T) {
	base := shortDir(t)
	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Listen(filepath.Join(open, "share.sock")); err == nil {
		t.Fatal("a 0755 folder was accepted")
	}
	path := filepath.Join(base, "private", "share.sock")
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(filepath.Dir(path)); info.Mode().Perm() != 0o700 {
		t.Fatalf("folder mode %v", info.Mode())
	}
	if _, err := Listen(path); err == nil || !strings.Contains(err.Error(), "already sharing") {
		t.Fatalf("second listener = %v", err)
	}
	// A socket left behind without a listener is stale.
	listener.SetUnlinkOnClose(false)
	listener.Close()
	again, err := Listen(path)
	if err != nil {
		t.Fatalf("stale socket = %v", err)
	}
	again.Close()
}

type served struct {
	path   string
	store  *Store
	shared chan []Record
}

func serve(t *testing.T, attribute func(context.Context, int, Claim) *Source, limit, total int64) served {
	t.Helper()
	base := shortDir(t)
	store := openStore(t, filepath.Join(base, "shared"), limit, total)
	path := filepath.Join(base, "run", "share.sock")
	listener, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	shared := make(chan []Record, 8)
	service := &Service{Store: store, Attribute: attribute, Origin: "https://host.example", MinRate: 1, Logger: quietLogger(),
		Shared: func(records []Record) { shared <- records }}
	done := make(chan struct{})
	go func() { service.Serve(ctx, listener); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return served{path: path, store: store, shared: shared}
}

func runShare(t *testing.T, env map[string]string, arguments ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := RunShare(arguments, &stdout, &stderr, func(name string) string { return env[name] })
	return stdout.String(), stderr.String(), code
}

func TestShareCommandStreamsFilesAndReportsEach(t *testing.T) {
	server := serve(t, nil, 1<<20, 1<<20)
	dir := t.TempDir()
	image := filepath.Join(dir, "pricing.png")
	if err := os.WriteFile(image, bytes.Repeat([]byte{1}, 96_000), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runShare(t, nil, "-socket", server.path, image, dir, filepath.Join(dir, "missing.txt"))
	if code == 0 {
		t.Fatal("a failure exited 0")
	}
	list := server.store.List()
	if len(list) != 1 || list[0].Name != "pricing.png" || list[0].Size != 96_000 || list[0].Source != nil {
		t.Fatalf("list = %+v", list)
	}
	wantOut := "Shared pricing.png (96 KB) in Shepherdr.\n  https://host.example/#shared=" + list[0].ID + "\n"
	if stdout != wantOut {
		t.Fatalf("stdout = %q", stdout)
	}
	if !strings.Contains(stderr, "Share files, not folders.") || !strings.Contains(stderr, "missing.txt wasn't shared: there's no such file.") {
		t.Fatalf("stderr = %q", stderr)
	}
	if burst := <-server.shared; len(burst) != 1 {
		t.Fatalf("burst = %v", burst)
	}
	stored, err := os.ReadFile(filepath.Join(server.store.dir, list[0].ID, list[0].Name))
	if err != nil || len(stored) != 96_000 {
		t.Fatalf("stored = %d %v", len(stored), err)
	}
}

func TestShareCommandSaysWhenShepherdrIsNotRunningOrFull(t *testing.T) {
	_, stderr, code := runShare(t, nil, "-socket", filepath.Join(shortDir(t), "none.sock"), "x")
	if code == 0 || !strings.Contains(stderr, "Shepherdr isn't running") {
		t.Fatalf("not running: %d %q", code, stderr)
	}
	// A socket folder Shepherdr refuses to listen in is not "not running".
	open := shortDir(t)
	if err := os.Chmod(open, 0o755); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runShare(t, nil, "-socket", filepath.Join(open, "share.sock"), "x")
	if code == 0 || strings.Contains(stderr, "isn't running") || !strings.Contains(stderr, "Shepherdr couldn't open its share socket in "+open) {
		t.Fatalf("refused folder: %d %q", code, stderr)
	}
	// A socket that is there but can't be reached is not "not running".
	blocked := serve(t, nil, 1, 1)
	if err := os.Chmod(filepath.Dir(blocked.path), 0); err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runShare(t, nil, "-socket", blocked.path, "x")
	os.Chmod(filepath.Dir(blocked.path), 0o700)
	if code == 0 || strings.Contains(stderr, "isn't running") || !strings.Contains(stderr, "can't be reached from here (permission denied)") {
		t.Fatalf("blocked socket: %d %q", code, stderr)
	}
	server := serve(t, nil, 10, 12)
	dir := t.TempDir()
	big, small := filepath.Join(dir, "big.bin"), filepath.Join(dir, "small.bin")
	os.WriteFile(big, make([]byte, 11), 0o600)
	os.WriteFile(small, make([]byte, 7), 0o600)
	_, stderr, code = runShare(t, nil, "-socket", server.path, big, small, small)
	if code == 0 || !strings.Contains(stderr, "big.bin wasn't shared: it's larger than 10 B") ||
		!strings.Contains(stderr, "small.bin wasn't shared: shared files are full: 7 B of 12 B kept") {
		t.Fatalf("limits: %d %q", code, stderr)
	}
	if len(server.store.List()) != 1 {
		t.Fatal("refused files were stored")
	}
}

func TestAKilledShareLeavesNoFileAndNoEntry(t *testing.T) {
	server := serve(t, nil, 1<<20, 1<<20)
	connection, err := net.Dial("unix", server.path)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(connection)
	encoder.Encode(opening{})
	encoder.Encode(fileHeader{Name: "half.bin", Size: 1000})
	reply := make([]byte, 64)
	count, _ := connection.Read(reply)
	if !strings.Contains(string(reply[:count]), `"ready":true`) {
		t.Fatalf("reply = %q", reply[:count])
	}
	connection.Write(make([]byte, 400))
	time.Sleep(50 * time.Millisecond)
	connection.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(entries(t, filepath.Join(server.store.dir, incomingName))) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := entries(t, filepath.Join(server.store.dir, incomingName)); len(got) != 0 || len(server.store.List()) != 0 {
		t.Fatalf("left %v and %d entries", got, len(server.store.List()))
	}
	if _, err := server.store.Begin("next", 1<<20); err != nil {
		t.Fatalf("killed share kept its reservation: %v", err)
	}
}

func TestAttributionChecksTheClaimedTerminalIsAnAncestor(t *testing.T) {
	// A process that is not our ancestor stands for another terminal.
	stranger := exec.Command("sleep", "30")
	if err := stranger.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stranger.Process.Kill(); stranger.Wait() })
	lookup := func(pid int) Lookup {
		return func(_ context.Context, claimed string) (Terminal, bool, error) {
			return Terminal{PID: pid, PaneID: claimed, TerminalID: "term_x", Place: "site", Agent: "claude"}, true, nil
		}
	}
	claim := Claim{TUI: &TUIClaim{Socket: "/run/herdr.sock", Pane: "w1:p1"}}
	honest := Herdrs{TUISocket: "/run/herdr.sock", TUI: lookup(os.Getppid())}
	source := honest.Attribute(context.Background(), os.Getpid(), claim)
	if source == nil || source.Herdr != "tui" || source.PaneID != "w1:p1" || source.PID != os.Getppid() || source.Start == 0 {
		t.Fatalf("honest claim = %+v", source)
	}
	forged := Herdrs{TUISocket: "/run/herdr.sock", TUI: lookup(stranger.Process.Pid)}
	if source := forged.Attribute(context.Background(), os.Getpid(), claim); source != nil {
		t.Fatalf("forged claim = %+v", source)
	}
	otherSocket := Herdrs{TUISocket: "/run/other.sock", TUI: lookup(os.Getppid())}
	if source := otherSocket.Attribute(context.Background(), os.Getpid(), claim); source != nil {
		t.Fatal("a claim naming an unfollowed Herdr was accepted")
	}
	if source := honest.Attribute(context.Background(), os.Getpid(), Claim{}); source != nil {
		t.Fatal("no claim was attributed")
	}
}

func TestFileTypesAndSizes(t *testing.T) {
	for name, want := range map[string]string{
		"a.PNG": "image/png image", "a.svg": "image/svg+xml image", "a.mov": "video/quicktime video",
		"a.md": "text/plain; charset=utf-8 text", "a.pdf": "application/pdf pdf",
		"a.html": "application/octet-stream other", "a": "application/octet-stream other",
	} {
		contentType, kind := TypeOf(name)
		if got := contentType + " " + string(kind); got != want {
			t.Errorf("%s = %s, want %s", name, got, want)
		}
	}
	for size, want := range map[int64]string{0: "0 B", 999: "999 B", 96_000: "96 KB", 1_200_000: "1.2 MB", 600_000_000: "600 MB", 1 << 30: "1.1 GB"} {
		if got := FormatSize(size); got != want {
			t.Errorf("FormatSize(%d) = %s, want %s", size, got, want)
		}
	}
	// Limits read in the binary units they are given in.
	for size, want := range map[int64]string{12: "12 B", 10 << 10: "10 KiB", 39_000: "38 KiB", 1 << 30: "1 GiB", 10 << 30: "10 GiB", 1536 << 20: "1.5 GiB"} {
		if got := formatLimit(size); got != want {
			t.Errorf("formatLimit(%d) = %s, want %s", size, got, want)
		}
	}
	if Tag("x.jpeg") != "JPG" || Tag("x.tar.gz") != "GZ" || Tag("x.verylong") != "FILE" || Tag("noext") != "FILE" {
		t.Fatal("tags")
	}
}

func TestASecondStoreOnTheSameFolderIsRefusedAndChangesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "shared")
	first := openStore(t, dir, 1<<20, 1<<20)
	put(t, first, "kept.txt", "kept", nil)
	// The first store has a share in flight.
	inFlight := filepath.Join(dir, incomingName, "in-flight")
	if err := os.WriteFile(inFlight, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	index, _ := os.ReadFile(filepath.Join(dir, indexName))
	second, err := Open(dir, uploads.Limit{Unlimited: true}, uploads.Limit{Unlimited: true}, quietLogger())
	if !errors.Is(err, ErrInUse) || second != nil {
		t.Fatalf("second open = %v, %v", second, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, indexName))
	if _, err := os.Stat(inFlight); err != nil || string(after) != string(index) {
		t.Fatalf("second open changed the folder: %v, index equal %v", err, string(after) == string(index))
	}
	first.Close()
	reopened := openStore(t, dir, 1<<20, 1<<20)
	if list := reopened.List(); len(list) != 1 || list[0].Name != "kept.txt" {
		t.Fatalf("after the first closed = %+v", list)
	}
}
