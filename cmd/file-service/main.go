package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/fileservice"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := execute(os.Args, os.Stdout, logger, run); err != nil {
		logger.Error("file-service exited", "error", err)
		os.Exit(1)
	}
}

func execute(args []string, out io.Writer, logger *slog.Logger, runServer func(*slog.Logger) error) error {
	if len(args) > 1 {
		if len(args) != 4 || args[1] != "verify-retention" || args[2] != "--work-dir" {
			return errors.New("usage: file-service verify-retention --work-dir <temporary-directory>")
		}
		if err := verifyRetention(args[3]); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, "RETENTION_DIRECTORY_OK")
		return err
	}
	return runServer(logger)
}

func run(logger *slog.Logger) error {
	cfg, err := fileservice.ConfigFromEnv()
	if err != nil {
		return err
	}
	repo, err := fileservice.OpenRepository(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open metadata repository: %w", err)
	}
	defer repo.Close()
	objects, err := fileservice.NewLocalObjectStore(cfg.DataRoot)
	if err != nil {
		return err
	}
	clock := fileservice.RealClock{}
	service := &fileservice.Service{
		Manager: &fileservice.Manager{
			Repo: repo, Objects: objects, Clock: clock, MaxFileBytes: cfg.MaxFileBytes,
			QuotaBytes: cfg.QuotaBytes, Retention: cfg.Retention, TombstoneRetention: cfg.TombstoneRetention, ObjectPrefix: cfg.ObjectPrefix,
		},
		Capabilities:  fileservice.NewGrantStore(clock, cfg.CapabilityMaxAge),
		UploadTickets: fileservice.NewUploadTicketStore(clock, cfg.CapabilityMaxAge),
		IssuerToken:   cfg.IssuerToken,
		AuditToken:    cfg.AuditToken,
		PublicBaseURL: cfg.PublicBaseURL,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go service.RunSweeper(ctx, cfg.SweepInterval, func(err error) {
		logger.Error("file sweeper failed", "error", err)
	})
	publicServer := &http.Server{Addr: cfg.Addr, Handler: service.PublicHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	internalServer := &http.Server{Addr: cfg.InternalAddr, Handler: service.InternalHandler(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: time.Minute}
	fatalErr := make(chan error, 2)
	serve := func(name string, server *http.Server) {
		logger.Info("file-service listener started", "listener", name, "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatalErr <- fmt.Errorf("%s listener: %w", name, err)
		}
	}
	go serve("public", publicServer)
	go serve("internal", internalServer)

	var runErr error
	select {
	case runErr = <-fatalErr:
		stop()
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := publicServer.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	if err := internalServer.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	return runErr
}
