// Command server runs the task-management REST API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"taskmanager/internal/cache"
	"taskmanager/internal/config"
	"taskmanager/internal/handler"
	"taskmanager/internal/service"
	"taskmanager/internal/store"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.DB().Close()

	// Wait for MySQL (useful when the container starts before the DB).
	if err := waitFor(ctx, st.Ping, 30*time.Second); err != nil {
		log.Fatalf("mysql not ready: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	tc := cache.New(cfg.RedisAddr, cfg.CacheTTL)
	if err := tc.Ping(ctx); err != nil {
		// Redis is optional: the service keeps working with the cache disabled.
		log.Printf("warning: redis unavailable (%v); cache disabled", err)
	}

	authSvc := service.NewAuthService(st, cfg.JWTSecret, cfg.JWTExpiry)
	teamSvc := service.NewTeamService(st)
	taskSvc := service.NewTaskService(st, tc)
	statsSvc := service.NewStatsService(st)

	h := &handler.Handlers{Auth: authSvc, Teams: teamSvc, Tasks: taskSvc, Stats: statsSvc}
	router := handler.NewRouter(h, authSvc)

	srv := &http.Server{
		Addr:    ":" + cfg.ServerPort,
		Handler: router,
	}

	go func() {
		log.Printf("listening on :%s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	log.Println("server stopped")
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
