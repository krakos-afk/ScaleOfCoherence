package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ScaleOfCoherence/internal/api"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/comments"
	"ScaleOfCoherence/internal/config"
	"ScaleOfCoherence/internal/database"
	"ScaleOfCoherence/internal/database/repository"
	"ScaleOfCoherence/internal/items"
	"ScaleOfCoherence/internal/teams"
	"ScaleOfCoherence/internal/workflows"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "apply database migrations and exit")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}

	db, err := database.Connect(cfg.Database)
	if err != nil {
		slog.Error("database connection failed", "err", err)
		os.Exit(1)
	}

	if cfg.MigrateOnBoot || *migrateOnly {
		if err := db.Migrate(); err != nil {
			slog.Error("migrations failed", "err", err)
			os.Exit(1)
		}
		slog.Info("migrations up to date")
	}
	if *migrateOnly {
		if err := db.Close(); err != nil {
			slog.Error("closing database", "err", err)
		}
		return
	}

	authSvc := auth.NewService(repository.NewAuthRepo(db.DB()), cfg.JWTSecret, cfg.JWTTTL)
	teamSvc := teams.NewService(repository.NewTeamRepo(db.DB()))
	itemSvc := items.NewService(repository.NewItemRepo(db.DB()))
	workflowSvc := workflows.NewService(repository.NewWorkflowRepo(db.DB()))
	commentSvc := comments.NewService(repository.NewCommentRepo(db.DB()))

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.NewRouter(authSvc, teamSvc, itemSvc, workflowSvc, commentSvc, db, cfg.SPADir),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer stop()

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	slog.Info("server listening", "addr", srv.Addr)

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	// Stop accepting connections, drain in-flight requests, then close the pool.
	stop()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown incomplete", "err", err)
	}
	if err := db.Close(); err != nil {
		slog.Error("closing database", "err", err)
	}
}
