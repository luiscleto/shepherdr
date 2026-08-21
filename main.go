package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
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

	"github.com/luisc/shepherdr/internal/herdr"
	"github.com/luisc/shepherdr/internal/server"
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
	flag.Parse()

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
	client := herdr.NewClient(*socketPath)
	projector := herdr.NewProjector(client)
	herdrBinary, err := exec.LookPath("herdr")
	if err != nil {
		return fmt.Errorf("find herdr executable for terminal access: %w", err)
	}
	terminal := server.NewTerminalBridge(herdrBinary, *socketPath, logger, projector)
	defer terminal.Close()
	application := server.New(assets, projector, terminal, *terminalLabEnabled, client)

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go projector.Run(ctx)

	httpServer := &http.Server{
		Handler:           application.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.Serve(listener) }()
	logger.Info("Shepherdr is ready", "address", "http://"+listener.Addr().String(), "herdr_socket", *socketPath, "terminal_lab", *terminalLabEnabled)

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

func defaultHerdrSocket() (string, error) {
	configurationDirectory, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find user configuration directory: %w", err)
	}
	return filepath.Join(configurationDirectory, "herdr", "herdr.sock"), nil
}
