package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const (
	minimumValidatedAppVersion = "0.1.10"
	maximumValidatedAppVersion = "0.1.12"
	appCommandTimeout          = 10 * time.Second
	appStatusTimeout           = 2 * time.Second
	appWindowDidNotAnswer      = "the window did not answer"
)

// ErrAppWindowClosed means no Herdr app window answered, so a command was
// not sent.
var ErrAppWindowClosed = errors.New("the Herdr app window is closed")

// appRequiredArguments are the app commands Shepherdr uses and the arguments
// it sends to each.
var appRequiredArguments = map[string][]string{
	"space.new":    {"name", "folder"},
	"pane.split":   {"direction", "pane"},
	"space.rename": {"id", "name"},
}

// AppClient drives the Herdr app window through its documented herdr-app
// command line. It only ever runs --version, commands, status and do: a bare
// herdr-app opens or raises the window. It runs without a shell, in
// Shepherdr's own environment, so HERDR_APP_HOME picks the same app.
type AppClient struct {
	path    string
	timeout time.Duration
	// status bounds the window check: a local round trip, so a window that
	// doesn't answer by then counts as closed.
	status time.Duration
}

// NewAppClient checks the herdr-app at path: its version and the commands and
// arguments Shepherdr uses. An error says why app actions are off.
func NewAppClient(ctx context.Context, path string, logger *slog.Logger) (*AppClient, error) {
	client := &AppClient{path: path, timeout: appCommandTimeout, status: appStatusTimeout}
	output, _, err := client.run(ctx, "--version")
	if err != nil {
		return nil, fmt.Errorf("herdr-app --version failed: %w", err)
	}
	version := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(output)), "herdr-app"))
	parsed, ok := parseSemanticVersion(version)
	switch {
	case !ok:
		logger.Warn(fmt.Sprintf("Herdr app reported version %q, which Shepherdr could not verify; Shepherdr validated Herdr app %s through %s; continuing best-effort", version, minimumValidatedAppVersion, maximumValidatedAppVersion))
	case semver.Compare(parsed, "v"+minimumValidatedAppVersion) < 0:
		return nil, fmt.Errorf("Herdr app %s is older than the validated Herdr app %s through %s", version, minimumValidatedAppVersion, maximumValidatedAppVersion)
	case semver.Compare(parsed, "v"+maximumValidatedAppVersion) > 0 || semver.Prerelease(parsed) != "":
		logger.Warn(fmt.Sprintf("Herdr app %s is newer than the validated Herdr app %s through %s; continuing best-effort", version, minimumValidatedAppVersion, maximumValidatedAppVersion))
	}
	output, _, err = client.run(ctx, "commands")
	if err != nil {
		return nil, fmt.Errorf("herdr-app commands failed: %w", err)
	}
	if err := checkAppCommands(output); err != nil {
		return nil, err
	}
	return client, nil
}

func checkAppCommands(output []byte) error {
	var commands []struct {
		Name string `json:"name"`
		Args []struct {
			Name string `json:"name"`
		} `json:"args"`
	}
	if err := json.Unmarshal(output, &commands); err != nil {
		return errors.New("herdr-app commands printed an unexpected list")
	}
	offered := make(map[string][]string, len(commands))
	for _, command := range commands {
		arguments := []string{}
		for _, argument := range command.Args {
			arguments = append(arguments, argument.Name)
		}
		offered[command.Name] = arguments
	}
	for _, name := range []string{"space.new", "pane.split", "space.rename"} {
		arguments, ok := offered[name]
		if !ok {
			return fmt.Errorf("this Herdr app doesn't offer %s", name)
		}
		for _, argument := range appRequiredArguments[name] {
			if !contains(arguments, argument) {
				return fmt.Errorf("this Herdr app's %s doesn't take %s", name, argument)
			}
		}
	}
	return nil
}

// WindowOpen reports whether the app window answers within the status limit.
// Its socket file stays after the window closes, so only an answer counts.
func (c *AppClient) WindowOpen(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, c.status)
	defer cancel()
	_, _, err := c.run(ctx, "status")
	return err == nil
}

// Do runs one app command with args sent as one encoded JSON object, never
// built by a shell, and decodes the reply into result. It returns
// ErrAppWindowClosed when nothing was sent, an *APIError when the app refused
// the command, and a submitted *MutationError when the outcome is unknown.
func (c *AppClient) Do(ctx context.Context, command string, args map[string]any, result any) error {
	encoded, err := json.Marshal(args)
	if err != nil {
		return &MutationError{Err: fmt.Errorf("encode Herdr app %s arguments: %w", command, err)}
	}
	stdout, stderr, err := c.run(ctx, "do", command, string(encoded))
	var exit *exec.ExitError
	switch {
	case err == nil:
		if json.Unmarshal(stdout, result) != nil {
			return &MutationError{Err: fmt.Errorf("Herdr app %s printed an unexpected reply", command), Submitted: true}
		}
		return nil
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || errors.Is(err, ErrReplyTooLarge):
		return &MutationError{Err: fmt.Errorf("Herdr app %s: %w", command, err), Submitted: true}
	case !errors.As(err, &exit):
		// herdr-app did not start, so nothing reached the window.
		return &MutationError{Err: fmt.Errorf("run herdr-app: %w", err)}
	case exit.ExitCode() == 1:
		var reply struct {
			Error *string `json:"error"`
		}
		if json.Unmarshal(stdout, &reply) != nil || reply.Error == nil {
			return &MutationError{Err: fmt.Errorf("Herdr app %s printed an unexpected error", command), Submitted: true}
		}
		if *reply.Error == appWindowDidNotAnswer {
			return &MutationError{Err: fmt.Errorf("Herdr app %s: %s", command, *reply.Error), Submitted: true}
		}
		return &APIError{Code: "app_refused", Message: *reply.Error}
	case exit.ExitCode() == 2 && appWindowMissing(stderr):
		return ErrAppWindowClosed
	default:
		// "the window hung up", a kill or anything newer: it may have run.
		return &MutationError{Err: fmt.Errorf("Herdr app %s: %s", command, strings.TrimSpace(string(stderr))), Submitted: true}
	}
}

// appWindowMissing reads the CLI's stderr for "no Herdr window at …", which
// it prints before sending anything.
func appWindowMissing(stderr []byte) bool {
	message := strings.TrimPrefix(strings.TrimSpace(string(stderr)), "herdr-app: ")
	return strings.HasPrefix(message, "no Herdr window at ")
}

func (c *AppClient) run(ctx context.Context, args ...string) ([]byte, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var stdout, stderr boundedOutput
	command := exec.CommandContext(ctx, c.path, args...)
	command.Stdout, command.Stderr = &stdout, &stderr
	command.WaitDelay = time.Second
	err := command.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	} else if stdout.tooLarge || stderr.tooLarge {
		err = replyTooLarge{replyLimit}
	}
	return stdout.data, stderr.data, err
}
