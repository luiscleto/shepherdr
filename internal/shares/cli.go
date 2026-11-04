package shares

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/luiscleto/shepherdr/internal/uploads"
)

// AgentNote is what `shepherdr --skill` prints and Settings shows under For
// your agents, for a person to paste into their agents' instructions.
const AgentNote = `# Sharing files with the person (Shepherdr)

When the person asks to see a file you made or found, such as an image, a
screenshot, a PDF, a video or a log, share it with them through Shepherdr:

    shepherdr share FILE...

Shepherdr keeps a copy of each file as it is now and shows it in Shared files
on their phone or computer until they delete it. Share only what they asked
to see. ` + "`shepherdr share --help`" + ` has the details.`

const shareHelp = `Usage: shepherdr share [-socket PATH] FILE...

Shares files with the person using Shepherdr. Use it when they ask to see a
file, such as an image, a screenshot, a PDF, a video or a log.

Shepherdr copies each file as it is now. Later changes to the file aren't
shared; share it again to send a new version. The person sees it in Shared
files on every device they use Shepherdr on, and can open, download or
delete it. Its label says which terminal shared it.

You can't list, read back or delete shared files. Share only what the person
asked to see: Shepherdr keeps each copy until they delete it.

Share regular files, not folders; share a zip for several. Shepherdr refuses a
file larger than its limit for one file, or one that would pass its total.

In a sandbox that blocks local sockets, run shepherdr share outside it.

Options:
  -socket PATH   Shepherdr's share socket
                 (default $XDG_RUNTIME_DIR/shepherdr/share.sock)
`

// RunShare is `shepherdr share`. It returns the exit code: nonzero when any
// file wasn't shared.
func RunShare(arguments []string, stdout, stderr io.Writer, getenv func(string) string) int {
	flags := flag.NewFlagSet("share", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	socket := flags.String("socket", "", "")
	if err := flags.Parse(arguments); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, shareHelp)
			return 0
		}
		fmt.Fprintf(stderr, "%v\n\n%s", err, shareHelp)
		return 2
	}
	files := flags.Args()
	if len(files) == 0 {
		fmt.Fprint(stderr, shareHelp)
		return 2
	}
	path := *socket
	if path == "" {
		var err error
		if path, err = DefaultSocketPath(getenv); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	connection, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, dialFailure(path, err))
		return 1
	}
	defer connection.Close()
	session := shareSession{connection: connection, reader: bufio.NewReader(connection), stdout: stdout, stderr: stderr}
	if err := session.send(opening{Claim: claimFromEnvironment(getenv)}); err != nil {
		fmt.Fprintln(stderr, "Shepherdr stopped answering, so nothing was shared.")
		return 1
	}
	failed := false
	for index, name := range files {
		outcome := session.share(name)
		if outcome == shareFailed {
			failed = true
		}
		if outcome == shareLost {
			for _, rest := range files[index+1:] {
				fmt.Fprintf(stderr, "%s wasn't shared.\n", filepath.Base(rest))
			}
			return 1
		}
	}
	if failed {
		return 1
	}
	return 0
}

// dialFailure says why the share socket couldn't be reached. No socket, or
// nothing listening, means Shepherdr isn't running, unless its folder is one
// Shepherdr refuses to listen in. Anything else, such as a sandbox that
// blocks local sockets, is not that, and saying so would send the person to
// restart a Shepherdr that is running.
func dialFailure(path string, err error) string {
	directory := filepath.Dir(path)
	if info, statErr := os.Lstat(directory); errors.Is(err, syscall.ENOENT) && statErr == nil && !uploads.OwnerOnlyDirectory(info) {
		return fmt.Sprintf("Shepherdr couldn't open its share socket in %s, so nothing was shared. Shepherdr's log says why: "+
			"that folder must be an owner-only 0700 folder.", directory)
	}
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return "Shepherdr isn't running, so nothing was shared. Ask the person to start Shepherdr, then share again."
	}
	reason := err.Error()
	var errno syscall.Errno
	if errors.As(err, &errno) {
		reason = errno.Error()
	}
	return fmt.Sprintf("Shepherdr's share socket %s can't be reached from here (%s), so nothing was shared. "+
		"A sandbox may be blocking local sockets: run shepherdr share where it can reach that socket.", path, reason)
}

func claimFromEnvironment(getenv func(string) string) Claim {
	var claim Claim
	if socket, pane := getenv("HERDR_SOCKET_PATH"), getenv("HERDR_PANE_ID"); socket != "" && pane != "" {
		claim.TUI = &TUIClaim{Socket: socket, Pane: pane}
	}
	if socket, terminal := getenv("HC_TERMINAL_SOCKET"), getenv("HC_TERMINAL_ID"); socket != "" && terminal != "" {
		claim.Core = &CoreClaim{Socket: socket, Terminal: terminal}
	}
	return claim
}

type shareOutcome int

const (
	shareDone shareOutcome = iota
	shareFailed
	shareLost
)

type shareSession struct {
	connection net.Conn
	reader     *bufio.Reader
	stdout     io.Writer
	stderr     io.Writer
}

func (s *shareSession) send(value any) error {
	_ = s.connection.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return json.NewEncoder(s.connection).Encode(value)
}

func (s *shareSession) receive() (Reply, error) {
	// Shepherdr answers once the bytes are on its disk, which can take a
	// while for a large file on a busy machine.
	_ = s.connection.SetReadDeadline(time.Now().Add(5 * time.Minute))
	line, err := s.reader.ReadBytes('\n')
	if err != nil {
		return Reply{}, err
	}
	var reply Reply
	return reply, json.Unmarshal(line, &reply)
}

func (s *shareSession) share(name string) shareOutcome {
	shown := filepath.Base(name)
	// Opening without blocking keeps a named pipe from holding the command;
	// only a regular file is shared.
	file, err := os.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			fmt.Fprintf(s.stderr, "%s wasn't shared: there's no such file.\n", shown)
		case errors.Is(err, os.ErrPermission):
			fmt.Fprintf(s.stderr, "%s wasn't shared: you can't read it.\n", shown)
		default:
			fmt.Fprintf(s.stderr, "%s wasn't shared: %v.\n", shown, err)
		}
		return shareFailed
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fmt.Fprintf(s.stderr, "%s wasn't shared. Share files, not folders.\n", shown)
		return shareFailed
	}
	if err := s.send(fileHeader{Name: filepath.Base(name), Size: info.Size()}); err != nil {
		fmt.Fprintf(s.stderr, "%s wasn't shared: Shepherdr stopped answering.\n", shown)
		return shareLost
	}
	reply, err := s.receive()
	if err != nil {
		fmt.Fprintf(s.stderr, "%s wasn't shared: Shepherdr stopped answering.\n", shown)
		return shareLost
	}
	if reply.Error != "" {
		fmt.Fprintf(s.stderr, "%s wasn't shared: %s.\n", shown, strings.TrimSuffix(reply.Error, "."))
		return shareFailed
	}
	_ = s.connection.SetWriteDeadline(time.Time{})
	copied, err := io.CopyN(s.connection, file, info.Size())
	if err != nil || copied != info.Size() {
		if errors.Is(err, io.EOF) {
			fmt.Fprintf(s.stderr, "%s wasn't shared: it got shorter while it was being read.\n", shown)
		} else {
			fmt.Fprintf(s.stderr, "%s wasn't shared: Shepherdr stopped answering.\n", shown)
		}
		return shareLost
	}
	result, err := s.receive()
	if err != nil || result.Error != "" || result.ID == "" {
		message := "Shepherdr stopped answering"
		if result.Error != "" {
			message = strings.TrimSuffix(result.Error, ".")
		}
		fmt.Fprintf(s.stderr, "%s wasn't shared: %s.\n", shown, message)
		return shareLost
	}
	fmt.Fprintf(s.stdout, "Shared %s (%s) in Shepherdr.\n", result.Name, FormatSize(result.Size))
	if result.Address != "" {
		fmt.Fprintf(s.stdout, "  %s\n", result.Address)
	}
	return shareDone
}
