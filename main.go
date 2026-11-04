package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/luiscleto/shepherdr/internal/access"
	"github.com/luiscleto/shepherdr/internal/herdr"
	"github.com/luiscleto/shepherdr/internal/notifications"
	"github.com/luiscleto/shepherdr/internal/server"
	"github.com/luiscleto/shepherdr/internal/shares"
	"github.com/luiscleto/shepherdr/internal/uploads"
	"github.com/mdp/qrterminal/v3"
)

//go:embed all:web/dist
var browserFiles embed.FS

// releaseVersion is set for release candidates with -ldflags "-X main.releaseVersion=v0.1.0".
var releaseVersion string

func main() {
	// shepherdr share is for agents: it only streams files to the running
	// Shepherdr and takes none of the server's flags.
	if len(os.Args) > 1 && os.Args[1] == "share" {
		os.Exit(shares.RunShare(os.Args[2:], os.Stdout, os.Stderr, os.Getenv))
	}
	if err := run(); err != nil {
		slog.Error("Shepherdr stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	defaultSocket, err := defaultHerdrSocket()
	if err != nil {
		return err
	}
	listenAddress := flag.String("listen", "127.0.0.1:8787", "localhost address to listen on")
	socketPath := flag.String("herdr-socket", defaultSocket, "Unix socket for the Herdr TUI")
	var herdrCoreSocket optionalStringFlag
	flag.Var(&herdrCoreSocket, "herdr-core-socket", "Unix socket for herdr-core (default $XDG_RUNTIME_DIR/herdr-core/default.sock)")
	terminalLabEnabled := flag.Bool("terminal-lab", false, "enable the development-only terminal comparison lab")
	noSignIn := flag.Bool("no-sign-in", false, "run without passkey protection for this start")
	var noSignInOrigin optionalStringFlag
	flag.Var(&noSignInOrigin, "no-sign-in-origin", "with -no-sign-in, one private HTTPS origin accepted besides loopback, for this start")
	var publicOrigin optionalStringFlag
	flag.Var(&publicOrigin, "public-origin", "canonical private HTTPS origin for protected access")
	var sessionLifetime optionalStringFlag
	flag.Var(&sessionLifetime, "session-lifetime", "protected session duration: 1d through 365d, or none")
	var vapidContact optionalStringFlag
	flag.Var(&vapidContact, "vapid-contact", "operator contact for Web Push (mailto: or HTTPS URI)")
	resetNotifications := flag.Bool("reset-notifications", false, "clear notification subscriptions, keys, and contact, then exit")
	showVersion := flag.Bool("version", false, "print the Shepherdr version and exit")
	uploadParent := flag.String("upload-parent", os.TempDir(), "parent directory for workspace file uploads")
	uploadLimitText := flag.String("upload-limit", "1GiB", "file bytes allowed per send: bytes, KiB, MiB, GiB, or none; staged files live under -upload-parent, which defaults to the system temp directory and may be RAM-backed")
	uploadMinRateText := flag.String("upload-min-rate", "8KiB", "slowest average a file send may arrive at, per second, after its first 30s: bytes, KiB, MiB, GiB, or none")
	requestBodyTimeText := flag.String("request-body-time", "30s", "time allowed to send any other request body, such as 30s or 2m")
	herdrReplyLimitText := flag.String("herdr-reply-limit", "64MiB", "largest reply Shepherdr reads from Herdr: bytes, KiB, MiB, or GiB, at least 1MiB")
	logLevelText := flag.String("log-level", "info", "minimum log level: debug, info, warn, or error")
	var shareDirectory optionalStringFlag
	flag.Var(&shareDirectory, "share-dir", "folder that keeps the files agents share with shepherdr share, until they are deleted (default $XDG_DATA_HOME/shepherdr/shared, else ~/.local/share/shepherdr/shared)")
	shareLimitText := flag.String("share-limit", "1GiB", "largest file an agent may share: bytes, KiB, MiB, GiB, or none")
	shareTotalText := flag.String("share-total", "10GiB", "most bytes shared files may keep in all: bytes, KiB, MiB, GiB, or none")
	var shareSocket optionalStringFlag
	flag.Var(&shareSocket, "share-socket", "owner-only socket shepherdr share streams to (default $XDG_RUNTIME_DIR/shepherdr/share.sock)")
	showSkill := flag.Bool("skill", false, "print a note for agents about shepherdr share, then exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("shepherdr %s\n", buildVersion(releaseVersion, readBuildInfo()))
		return nil
	}
	if *showSkill {
		fmt.Println(shares.AgentNote)
		return nil
	}
	logLevel, err := parseLogLevel(*logLevelText)
	if err != nil {
		return err
	}

	notificationPath, notificationPathErr := notifications.DefaultStatePath()
	accessPath, accessPathErr := access.DefaultStatePath()
	uploadPath, uploadPathErr := uploads.DefaultStatePath()
	if accessPathErr != nil {
		return accessPathErr
	}
	if len(flag.Args()) > 0 {
		if flag.Args()[0] != "access" {
			return fmt.Errorf("unknown command %q", flag.Args()[0])
		}
		if *noSignIn || noSignInOrigin.set || publicOrigin.set || sessionLifetime.set || vapidContact.set || *resetNotifications ||
			flagWasSet("listen") || flagWasSet("herdr-socket") || herdrCoreSocket.set || flagWasSet("terminal-lab") ||
			flagWasSet("upload-parent") || flagWasSet("upload-limit") || flagWasSet("upload-min-rate") ||
			flagWasSet("request-body-time") || flagWasSet("herdr-reply-limit") || flagWasSet("log-level") ||
			shareDirectory.set || flagWasSet("share-limit") || flagWasSet("share-total") || shareSocket.set {
			return errors.New("access commands cannot be combined with server configuration flags")
		}
		if notificationPathErr != nil {
			return notificationPathErr
		}
		return runAccessCommand(flag.Args()[1:], accessPath, notificationPath)
	}
	if *noSignIn && (publicOrigin.set || sessionLifetime.set) {
		return errors.New("-no-sign-in cannot be combined with -public-origin or -session-lifetime")
	}
	if noSignInOrigin.set && (publicOrigin.set || sessionLifetime.set) {
		return errors.New("-no-sign-in-origin cannot be combined with -public-origin or -session-lifetime")
	}
	if noSignInOrigin.set && !*noSignIn {
		return errors.New("-no-sign-in-origin needs -no-sign-in")
	}
	var signInOffOrigin *access.Origin
	if noSignInOrigin.set {
		parsed, err := access.ParseCanonicalOrigin(noSignInOrigin.value)
		if err != nil {
			return errors.New("-no-sign-in-origin must be exactly https://lowercase-ascii-dns-name[:nondefault-port] without a path")
		}
		signInOffOrigin = &parsed
	}
	if uploadPathErr != nil {
		return uploadPathErr
	}
	uploadLimit, err := uploads.ParseLimit(*uploadLimitText)
	if err != nil {
		return fmt.Errorf("-upload-limit: %w", err)
	}
	shareLimit, err := uploads.ParseLimit(*shareLimitText)
	if err != nil {
		return errors.New("-share-limit must be a positive byte count using optional KiB, MiB, or GiB, or none")
	}
	shareTotal, err := uploads.ParseLimit(*shareTotalText)
	if err != nil {
		return errors.New("-share-total must be a positive byte count using optional KiB, MiB, or GiB, or none")
	}
	sharePath := shareDirectory.value
	if !shareDirectory.set {
		if sharePath, err = shares.DefaultDirectory(os.Getenv, runtime.GOOS); err != nil {
			return err
		}
	} else if !filepath.IsAbs(sharePath) {
		return errors.New("-share-dir must be an absolute path")
	}
	shareSocketPath := shareSocket.value
	if !shareSocket.set {
		if shareSocketPath, err = shares.DefaultSocketPath(os.Getenv); err != nil {
			return err
		}
	} else if !filepath.IsAbs(shareSocketPath) {
		return errors.New("-share-socket must be an absolute path")
	}
	uploadMinRate, err := parseUploadMinRate(*uploadMinRateText)
	if err != nil {
		return err
	}
	requestBodyTime, err := parseRequestBodyTime(*requestBodyTimeText)
	if err != nil {
		return err
	}
	herdrReplyLimit, err := parseHerdrReplyLimit(*herdrReplyLimitText)
	if err != nil {
		return err
	}
	herdr.SetReplyLimit(herdrReplyLimit)
	if *resetNotifications {
		if notificationPathErr != nil {
			return notificationPathErr
		}
		serviceLock, err := access.HoldServiceLock(accessPath)
		if err != nil {
			return errors.New("Stop Shepherdr first")
		}
		if serviceLock != nil {
			defer serviceLock.Close()
		}
		if err := notifications.Reset(notificationPath); err != nil {
			return err
		}
		fmt.Println("Notification state reset. Configure -vapid-contact and enable each browser again.")
		return nil
	}
	contact := ""
	if vapidContact.set {
		contact, err = notifications.ValidateContact(vapidContact.value)
		if err != nil {
			return err
		}
	}

	if err := server.ValidateListenAddress(*listenAddress); err != nil {
		return err
	}
	if *socketPath == "" || !filepath.IsAbs(*socketPath) {
		return fmt.Errorf("-herdr-socket must be an absolute path")
	}
	coreSocketPath, err := herdrCoreSocketPath(herdrCoreSocket, os.Getenv("XDG_RUNTIME_DIR"))
	if err != nil {
		return err
	}
	assets, err := fs.Sub(browserFiles, "web/dist")
	if err != nil {
		return fmt.Errorf("open embedded browser assets: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	var accessStore *access.Store
	var accessManager *access.Manager
	var protectedOrigin access.Origin
	var signInOffLock *os.File
	if *noSignIn {
		signInOffLock, err = access.HoldServiceLock(accessPath)
		if err != nil {
			return fmt.Errorf("hold access service lock: %w", err)
		}
		if signInOffLock != nil {
			defer signInOffLock.Close()
		}
		logger.Warn("Sign-in is off; every browser that can reach Shepherdr has operator authority")
	} else {
		opened, err := access.OpenProtected(access.OpenOptions{
			Path: accessPath, PublicOrigin: publicOrigin.value, SessionLifetime: sessionLifetime.value,
			SessionLifetimeSet: sessionLifetime.set,
		})
		if err != nil {
			return err
		}
		accessStore = opened.Store
		defer accessStore.Close()
		accessManager, err = access.NewManager(accessStore, nil)
		if err != nil {
			return err
		}
		defer accessManager.Close()
		protectedOrigin = opened.Origin
		if opened.BootstrapToken != "" {
			printInvitation(os.Stdout, protectedOrigin.InvitationURL(opened.BootstrapToken))
		}
	}
	uploadManager, err := uploads.Open(*uploadParent, uploadPath, logger)
	if err != nil {
		return fmt.Errorf("open terminal file uploads: %w", err)
	}
	defer uploadManager.Close()
	var notificationStore *notifications.Store
	var notificationWarning error
	if notificationPathErr != nil {
		notificationWarning = notificationPathErr
		notificationStore = notifications.UnavailableStore(notificationWarning)
	} else {
		notificationStore, notificationWarning = notifications.OpenStore(notificationPath, contact)
	}
	if notificationWarning != nil {
		logger.Error("Notifications are unavailable; Home and Terminal will continue", "error", notificationWarning)
	}
	notificationManager := notifications.NewManager(notificationStore, logger)
	if accessManager != nil {
		if err := notificationManager.EnableProtected(accessManager, accessManager.ActiveTrustIDs()); err != nil {
			logger.Error("Protected notifications are unavailable; access remains protected", "error", err)
		}
		accessManager.SetNotificationAuthority(notificationManager)
	}
	// Shepherdr follows the Herdr TUI and herdr-core at once, each on its own
	// socket, with its own terminals, file records and actions.
	actionClient := herdr.NewClient(*socketPath, logger)
	tuiProjector := herdr.NewProjector(actionClient)
	coreProjector := herdr.NewNotFollowedProjector()
	var coreTerminal *server.CoreTerminalBridge
	var coreUploadManager *uploads.Manager
	var coreClient *herdr.CoreClient
	homeFolder, _ := os.UserHomeDir()
	if coreSocketPath != "" {
		coreClient = herdr.NewCoreClient(coreSocketPath, logger)
		coreProjector = herdr.NewCoreProjector(coreClient, homeFolder)
		coreTerminal = server.NewCoreTerminalBridge(coreClient, logger, coreProjector)
		defer coreTerminal.Close()
		coreUploadManager, err = uploads.Open(*uploadParent, filepath.Join(filepath.Dir(uploadPath), "uploads-core.json"), logger)
		if err != nil {
			return fmt.Errorf("open Herdr Core terminal file uploads: %w", err)
		}
		defer coreUploadManager.Close()
	} else {
		logger.Info("Herdr Core is not followed: set -herdr-core-socket")
	}
	var appClient *herdr.AppClient
	if coreSocketPath != "" {
		appClient = herdrAppClient(herdrCoreSocket.set, logger)
		if appClient != nil {
			coreProjector.SetAppWindow(appClient)
		}
	}
	var terminal *server.TerminalBridge
	if herdrBinary, err := exec.LookPath("herdr"); err != nil {
		logger.Warn("Herdr TUI terminals can't open: herdr is not on PATH")
	} else {
		terminal = server.NewTerminalBridge(herdrBinary, *socketPath, logger, tuiProjector)
		defer terminal.Close()
	}
	observeHerdrs(tuiProjector, coreProjector, notificationManager, uploadManager, coreUploadManager)
	application := server.New(assets, tuiProjector, coreProjector, terminal, *terminalLabEnabled, actionClient)
	application.SetFileUploads(uploadManager, uploadLimit)
	application.SetBodyLimits(requestBodyTime, uploadMinRate)
	if coreTerminal != nil {
		application.SetCoreTerminal(coreTerminal)
		application.SetCoreFileUploads(coreUploadManager)
		application.SetCoreActions(coreClient, homeFolder)
		if appClient != nil {
			application.SetCoreAppActions(appClient)
		}
	}
	application.SetNotifications(notificationManager)
	// Another Shepherdr keeping shared files in the same folder turns sharing
	// off here and leaves its folder alone; the rest of Shepherdr runs.
	shareStore, err := shares.Open(sharePath, shareLimit, shareTotal, logger)
	switch {
	case errors.Is(err, shares.ErrInUse):
		logger.Error("Agents can't share files with this Shepherdr", "error", err)
	case err != nil:
		return fmt.Errorf("open shared files: %w", err)
	default:
		defer shareStore.Close()
		application.SetShares(shareStore)
	}
	shareHerdrs := shares.Herdrs{TUISocket: *socketPath, TUI: server.TUIShareLookup(actionClient)}
	if coreClient != nil {
		shareHerdrs.CoreSocket, shareHerdrs.Core = coreSocketPath, server.CoreShareLookup(coreClient, homeFolder)
	}

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	if accessManager != nil {
		application.ConfigureProtectedAccess(accessManager)
	} else {
		_, listenPort, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
		application.ConfigureSignInOff(listenPort, signInOffOrigin)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	notificationManager.Start(ctx)
	shareAddress := ""
	if accessManager != nil {
		shareAddress = protectedOrigin.Value
	} else if signInOffOrigin != nil {
		shareAddress = signInOffOrigin.Value
	}
	shareService := &shares.Service{Store: shareStore, Attribute: shareHerdrs.Attribute, Origin: shareAddress, MinRate: uploadMinRate, Logger: logger,
		Shared: func(records []shares.Record) {
			notifyShared(notificationManager, tuiProjector, coreProjector, records[0].Source)
		}}
	shareCommand := "shepherdr share FILE"
	if shareStore != nil {
		if shareListener, err := shares.Listen(shareSocketPath); err != nil {
			logger.Error("Agents can't share files with this Shepherdr", "error", err)
		} else {
			go shareService.Serve(ctx, shareListener)
			if executable, err := os.Executable(); err == nil {
				shareCommand = executable + " share FILE"
				if shareSocket.set {
					shareCommand = executable + " share -socket " + shareSocketPath + " FILE"
				}
			}
		}
	}
	go tuiProjector.Run(ctx)
	go coreProjector.Run(ctx)

	httpServer := &http.Server{
		Handler:           application.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.Serve(listener) }()
	address := "http://" + listener.Addr().String()
	if accessManager != nil {
		address = protectedOrigin.Value
	} else if signInOffOrigin != nil {
		address = signInOffOrigin.Value
	}
	logger.Info("Shepherdr is ready", "address", address, "local_address", "http://"+listener.Addr().String(), "herdr_socket", *socketPath, "herdr_core_socket", coreSocketPath, "terminal_lab", *terminalLabEnabled, "sign_in_off", *noSignIn, "upload_parent", *uploadParent, "upload_limit", *uploadLimitText, "upload_min_rate", *uploadMinRateText, "request_body_time", requestBodyTime, "herdr_reply_limit", *herdrReplyLimitText, "share_dir", sharePath, "share_limit", *shareLimitText, "share_total", *shareTotalText, "share_command", shareCommand)

	select {
	case <-ctx.Done():
		// A second signal ends the wait for open requests at once.
		again, stopAgain := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stopAgain()
		stop()
		if terminal != nil {
			terminal.Close()
		}
		shutdownCtx, cancel := context.WithTimeout(again, 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			// Close the requests still open, such as a slow file send: their
			// reads fail, and they remove their partial files before the
			// upload records close.
			_ = httpServer.Close()
			return err
		}
		return nil
	case err := <-serveErrors:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// notifyShared sends one File shared notice for the files one shepherdr
// share command stored; they all come from the same terminal, or from
// outside Herdr.
func notifyShared(manager *notifications.Manager, tui, core *herdr.Projector, source *shares.Source) {
	if source == nil {
		manager.FileShared("", herdr.Snapshot{}, "", "", "")
		return
	}
	projector := tui
	if source.Herdr == string(herdr.BackendCore) {
		projector = core
	}
	manager.FileShared(herdr.Backend(source.Herdr), projector.Current().Snapshot, source.PaneID, source.TerminalID, source.Handle())
}

type snapshotObservers interface {
	SetSnapshotObserver(herdr.SnapshotObserver)
	SetPublishedSnapshotObserver(herdr.PublishedSnapshotObserver)
}

// observeHerdrs gives each Herdr its own notification observer and its own
// upload records: each upload manager sees only its own Herdr's reads.
func observeHerdrs(tui, core snapshotObservers, notificationManager *notifications.Manager, uploadManager, coreUploadManager *uploads.Manager) {
	tui.SetSnapshotObserver(notificationManager.Observer(herdr.BackendTUI))
	tui.SetPublishedSnapshotObserver(uploadManager)
	core.SetSnapshotObserver(notificationManager.Observer(herdr.BackendCore))
	if coreUploadManager != nil {
		core.SetPublishedSnapshotObserver(coreUploads{coreUploadManager})
	}
}

// coreUploads keeps files for Other terminals while Home lists the group, as
// for a space. The group never joins Snapshot.Workspaces, because space
// notices rely on its absence, so it is added only for the upload records.
type coreUploads struct{ manager *uploads.Manager }

func (c coreUploads) ObservePublishedSnapshot(snapshot herdr.Snapshot, baseline bool) {
	for _, pane := range snapshot.Panes {
		if pane.WorkspaceID == herdr.OtherTerminalsID {
			snapshot.Workspaces = append(slices.Clone(snapshot.Workspaces), herdr.WorkspaceInfo{WorkspaceID: herdr.OtherTerminalsID})
			break
		}
	}
	c.manager.ObservePublishedSnapshot(snapshot, baseline)
}

func readBuildInfo() *debug.BuildInfo {
	information, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return information
}

func buildVersion(linkerVersion string, information *debug.BuildInfo) string {
	if linkerVersion != "" {
		return linkerVersion
	}
	if information == nil {
		return "devel"
	}
	for _, setting := range information.Settings {
		if setting.Key == "vcs" {
			return "devel"
		}
	}
	if information.Main.Version != "" && information.Main.Version != "(devel)" {
		return information.Main.Version
	}
	return "devel"
}

func parseLogLevel(value string) (slog.Level, error) {
	switch value {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, errors.New("-log-level must be debug, info, warn, or error")
	}
}

// parseUploadMinRate reads -upload-min-rate in bytes a second; none is 0.
func parseUploadMinRate(value string) (int64, error) {
	if value == "none" {
		return 0, nil
	}
	rate, err := uploads.ParseSize(value)
	if err != nil {
		return 0, errors.New("-upload-min-rate must be a positive byte count a second using optional KiB, MiB, or GiB, or none")
	}
	return rate, nil
}

func parseRequestBodyTime(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second {
		return 0, errors.New("-request-body-time must be a duration of at least 1s, such as 30s or 2m")
	}
	return duration, nil
}

func parseHerdrReplyLimit(value string) (int64, error) {
	limit, err := uploads.ParseSize(value)
	if err != nil || limit < 1<<20 {
		return 0, errors.New("-herdr-reply-limit must be a byte count of at least 1MiB using optional KiB, MiB, or GiB")
	}
	return limit, nil
}

type optionalStringFlag struct {
	set   bool
	value string
}

func (f *optionalStringFlag) String() string { return f.value }

func (f *optionalStringFlag) Set(value string) error {
	f.set = true
	f.value = value
	return nil
}

func flagWasSet(name string) bool {
	found := false
	flag.Visit(func(current *flag.Flag) {
		if current.Name == name {
			found = true
		}
	})
	return found
}

func defaultHerdrSocket() (string, error) {
	// Herdr releases use XDG or ~/.config on both Linux and macOS, not
	// macOS Application Support (src/config/io.rs in Herdr 0.8.0–0.9.0).
	configurationDirectory := os.Getenv("XDG_CONFIG_HOME")
	if configurationDirectory == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find user home directory for Herdr socket: %w", err)
		}
		configurationDirectory = filepath.Join(home, ".config")
	}
	return filepath.Join(configurationDirectory, "herdr", "herdr.sock"), nil
}

// herdrAppClient finds the Herdr app's command line for the actions its window
// does, or logs once why they are off. The app at its default home drives the
// default herdr-core, so a custom core socket turns them off.
func herdrAppClient(customCoreSocket bool, logger *slog.Logger) *herdr.AppClient {
	if customCoreSocket {
		logger.Info("Herdr app actions are off: Shepherdr follows a custom Herdr Core socket")
		return nil
	}
	path, err := exec.LookPath("herdr-app")
	if err != nil {
		logger.Info("Herdr app actions are off: herdr-app is not on PATH")
		return nil
	}
	client, err := herdr.NewAppClient(context.Background(), path, logger)
	if err != nil {
		logger.Warn("Herdr app actions are off", "detail", err.Error())
		return nil
	}
	return client
}

// herdrCoreSocketPath returns the herdr-core socket to follow: the flag's
// absolute path, or the default under XDG_RUNTIME_DIR. It returns "" when
// neither is set, and then Shepherdr does not follow herdr-core.
func herdrCoreSocketPath(coreSocket optionalStringFlag, runtimeDirectory string) (string, error) {
	if coreSocket.set {
		if !filepath.IsAbs(coreSocket.value) {
			return "", errors.New("-herdr-core-socket must be an absolute path")
		}
		return coreSocket.value, nil
	}
	if runtimeDirectory == "" {
		return "", nil
	}
	return filepath.Join(runtimeDirectory, "herdr-core", "default.sock"), nil
}

func runAccessCommand(arguments []string, accessPath, notificationPath string) error {
	if len(arguments) == 0 {
		return errors.New("usage: shepherdr access invite|devices|revoke <trust-id>|reset")
	}
	store, _, err := access.OpenExistingStopped(accessPath)
	if err != nil {
		return err
	}
	defer store.Close()
	manager, err := access.NewManager(store, nil)
	if err != nil {
		return err
	}
	defer manager.Close()

	switch arguments[0] {
	case "invite":
		if len(arguments) != 1 {
			return errors.New("usage: shepherdr access invite")
		}
		link, expiresAt, err := manager.CreateLocalInvitation()
		if err != nil {
			return err
		}
		printInvitation(os.Stdout, link)
		fmt.Printf("Expires: %s\n", expiresAt.Format(time.RFC3339))
		return nil
	case "devices":
		if len(arguments) != 1 {
			return errors.New("usage: shepherdr access devices")
		}
		devices := manager.LocalDevices()
		if len(devices) == 0 {
			fmt.Println("No trusted sign-ins.")
			return nil
		}
		for _, device := range devices {
			fmt.Printf("%q\n", trustedSignInLabel(device.Label))
			fmt.Printf("  trust ID %s\n", device.TrustID)
			fmt.Printf("  created %s; last used %s\n", device.CreatedAt.Format(time.RFC3339), device.LastUsedAt.Format(time.RFC3339))
			fmt.Printf("  backup eligible: %t; backup state last reported by this passkey at %s: %t\n", device.BackupEligible, device.BackupObservedAt.Format(time.RFC3339), device.BackupState)
		}
		return nil
	case "revoke":
		if len(arguments) != 2 {
			return errors.New("usage: shepherdr access revoke <trust-id>")
		}
		notificationStore, _ := notifications.OpenStore(notificationPath, "")
		notificationManager := notifications.NewManager(notificationStore, nil)
		notificationContext, stopNotifications := context.WithCancel(context.Background())
		defer stopNotifications()
		notificationManager.Start(notificationContext)
		_ = notificationManager.EnableProtected(manager, manager.ActiveTrustIDs())
		manager.SetNotificationAuthority(notificationManager)
		result, err := manager.RevokeLocal(arguments[1])
		if err != nil {
			return err
		}
		access.WaitRuntimes(result.Runtimes)
		notificationManager.WaitPending()
		if result.NotificationCleanupError != nil {
			return fmt.Errorf("access was revoked, but notification cleanup must be retried: %w", result.NotificationCleanupError)
		}
		fmt.Printf("Trusted sign-in removed: %q\n", trustedSignInLabel(result.Label))
		return nil
	case "reset":
		if len(arguments) != 1 {
			return errors.New("usage: shepherdr access reset")
		}
		notificationStore, _ := notifications.OpenStore(notificationPath, "")
		notificationManager := notifications.NewManager(notificationStore, nil)
		manager.SetNotificationAuthority(notificationManager)
		runtimes, err := manager.Reset()
		if err != nil {
			return err
		}
		access.WaitRuntimes(runtimes)
		if err := notificationManager.ResetProtectedSubscriptions(); err != nil {
			return fmt.Errorf("access was reset, but notification cleanup must be retried: %w", err)
		}
		fmt.Println("Protected access reset. All trusted sign-ins were removed. The next protected start will print a new invitation.")
		return nil
	default:
		return fmt.Errorf("unknown access command %q", arguments[0])
	}
}

func trustedSignInLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "Trusted sign-in"
	}
	return label
}

func printInvitation(writer io.Writer, link string) {
	fmt.Fprintln(writer, "Trust this device")
	fmt.Fprintln(writer, "Open this link on that computer, or scan the QR code on a phone.")
	fmt.Fprintln(writer, link)
	qrterminal.GenerateHalfBlock(link, qrterminal.L, writer)
}
