// Command server runs the task-management REST API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"taskmanager/internal/cache"
	"taskmanager/internal/config"
	"taskmanager/internal/handler"
	"taskmanager/internal/middleware"
	"taskmanager/internal/service"
	"taskmanager/internal/store"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		fatal("open store", err)
	}
	defer st.DB().Close()

	// Wait for MySQL (useful when the container starts before the DB).
	if err := waitFor(ctx, st.Ping, 30*time.Second); err != nil {
		fatal("mysql not ready", err)
	}
	if err := st.Migrate(ctx); err != nil {
		fatal("migrate", err)
	}

	tc := cache.New(cfg.RedisAddr, cfg.CacheTTL, cache.WithCircuitBreaker(cfg.CBThreshold, cfg.CBCooldown))
	if err := tc.Ping(ctx); err != nil {
		// Redis is optional: the service keeps working with the cache disabled.
		slog.Warn("redis unavailable; cache disabled", "error", err)
	}

	authSvc := service.NewAuthService(st, cfg.JWTSecret, cfg.JWTExpiry)
	teamSvc := service.NewTeamService(st)
	taskSvc := service.NewTaskService(st, tc)
	statsSvc := service.NewStatsService(st)

	rl := middleware.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)
	defer rl.Stop()

	h := &handler.Handlers{Auth: authSvc, Teams: teamSvc, Tasks: taskSvc, Stats: statsSvc}
	router := handler.NewRouter(h, authSvc, rl)

	srv := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: router,
	}

	go func() {
		slog.Info("server started", "port", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("server", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
	slog.Info("server stopped")
}

func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

// waitFor retries fn until it succeeds or the timeout elapses.
func waitFor(ctx context.Context, fn func(context.Context) error, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		if err := fn(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
