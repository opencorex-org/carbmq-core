package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/opencorex/crabmq-core/api/db"
	"github.com/opencorex/crabmq-core/api/handlers"
	"github.com/opencorex/crabmq-core/api/services"
	authjwt "github.com/opencorex/crabmq-core/internal/auth/jwt"
	brokerserver "github.com/opencorex/crabmq-core/internal/broker/server"
	"github.com/opencorex/crabmq-core/internal/config"
)

func main() {
	cfg := config.Load()
	logger := newLogger(cfg.Observability.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	command := "all"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	store, err := db.Open(ctx, cfg.Database.URL)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	switch command {
	case "broker":
		if err := runBroker(ctx, cfg, store, logger); err != nil {
			logger.Error("broker exited with error", "error", err)
			os.Exit(1)
		}
	case "api":
		if err := runAPI(ctx, cfg, store, logger); err != nil {
			logger.Error("api exited with error", "error", err)
			os.Exit(1)
		}
	case "all":
		if err := runAll(ctx, cfg, store, logger); err != nil {
			logger.Error("daemon exited with error", "error", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\nusage: crabmqd [broker|api|all]\n", command)
		os.Exit(2)
	}
}

func runBroker(ctx context.Context, cfg config.Config, store *db.Store, logger *slog.Logger) error {
	server := brokerserver.New(cfg, store, logger)
	return server.Run(ctx)
}

func runAPI(ctx context.Context, cfg config.Config, store *db.Store, logger *slog.Logger) error {
	auth := authjwt.New(cfg.Auth)
	deviceService := services.NewDeviceService(store, auth, cfg.API.DefaultTokenTTL)
	bridge := services.NewBridge(cfg, store, auth, logger)
	httpServer := handlers.NewServer(cfg, store, deviceService, bridge, logger)

	errCh := make(chan error, 2)
	go func() {
		errCh <- bridge.Start(ctx)
	}()
	go func() {
		errCh <- httpServer.Serve(ctx)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func runAll(ctx context.Context, cfg config.Config, store *db.Store, logger *slog.Logger) error {
	errCh := make(chan error, 2)
	go func() {
		errCh <- runBroker(ctx, cfg, store, logger)
	}()
	go func() {
		errCh <- runAPI(ctx, cfg, store, logger)
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func newLogger(level string) *slog.Logger {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel}))
}
