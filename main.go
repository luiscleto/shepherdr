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
	"syscall"
	"time"

	"github.com/luisc/shepherdr/internal/access"
	"github.com/luisc/shepherdr/internal/herdr"
	"github.com/luisc/shepherdr/internal/notifications"
	"github.com/luisc/shepherdr/internal/server"
	"github.com/mdp/qrterminal/v3"
)

//go:embed all:web/dist
var browserFiles embed.FS

func main() {
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
	socketPath := flag.String("herdr-socket", defaultSocket, "Unix socket for the one Herdr session")
	terminalLabEnabled := flag.Bool("terminal-lab", false, "enable the development-only terminal comparison lab")
	noSignIn := flag.Bool("no-sign-in", false, "run without passkey protection for this start")
	var publicOrigin optionalStringFlag
	flag.Var(&publicOrigin, "public-origin", "canonical private HTTPS origin for protected access")
	var sessionLifetime optionalStringFlag
	flag.Var(&sessionLifetime, "session-lifetime", "protected session duration: 1d through 365d, or none")
	var vapidContact optionalStringFlag
	flag.Var(&vapidContact, "vapid-contact", "operator contact for Web Push (mailto: or HTTPS URI)")
	resetNotifications := flag.Bool("reset-notifications", false, "clear notification subscriptions, keys, and contact, then exit")
	flag.Parse()

	notificationPath, notificationPathErr := notifications.DefaultStatePath()
	accessPath, accessPathErr := access.DefaultStatePath()
	if accessPathErr != nil {
		return accessPathErr
	}
	if len(flag.Args()) > 0 {
		if flag.Args()[0] != "access" {
			return fmt.Errorf("unknown command %q", flag.Args()[0])
		}
		if *noSignIn || publicOrigin.set || sessionLifetime.set || vapidContact.set || *resetNotifications ||
			flagWasSet("listen") || flagWasSet("herdr-socket") || flagWasSet("terminal-lab") {
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
	assets, err := fs.Sub(browserFiles, "web/dist")
	if err != nil {
		return fmt.Errorf("open embedded browser assets: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
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
	client := herdr.NewClient(*socketPath)
	projector := herdr.NewProjector(client)
	projector.SetSnapshotObserver(notificationManager)
	herdrBinary, err := exec.LookPath("herdr")
	if err != nil {
		return fmt.Errorf("find herdr executable for terminal access: %w", err)
	}
	terminal := server.NewTerminalBridge(herdrBinary, *socketPath, logger, projector)
	defer terminal.Close()
	application := server.New(assets, projector, terminal, *terminalLabEnabled, client)
	application.SetNotifications(notificationManager)
	if accessManager != nil {
		application.ConfigureProtectedAccess(accessManager)
	} else {
		application.ConfigureSignInOff()
	}

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	notificationManager.Start(ctx)
	go projector.Run(ctx)

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
	}
	logger.Info("Shepherdr is ready", "address", address, "local_address", "http://"+listener.Addr().String(), "herdr_socket", *socketPath, "terminal_lab", *terminalLabEnabled, "sign_in_off", *noSignIn)

	select {
	case <-ctx.Done():
		terminal.Close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
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
	configurationDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	return filepath.Join(configurationDirectory, "herdr", "herdr.sock"), nil
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
			fmt.Printf("%s  %q\n", device.TrustID, device.Label)
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
		manager.SetNotificationAuthority(notificationManager)
		runtimes, err := manager.RevokeLocal(arguments[1])
		if err != nil {
			return err
		}
		access.WaitRuntimes(runtimes)
		if err := notificationManager.RemoveTrustSubscriptions(arguments[1]); err != nil {
			return fmt.Errorf("access was revoked, but notification cleanup must be retried: %w", err)
		}
		fmt.Println("Trusted sign-in revoked.")
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
		fmt.Println("Protected access reset. The next protected start will print a new invitation.")
		return nil
	default:
		return fmt.Errorf("unknown access command %q", arguments[0])
	}
}

func printInvitation(writer io.Writer, link string) {
	fmt.Fprintln(writer, "Trust this device")
	fmt.Fprintln(writer, "Open this link on that computer, or scan the QR code on a phone.")
	fmt.Fprintln(writer, link)
	qrterminal.GenerateHalfBlock(link, qrterminal.L, writer)
}
