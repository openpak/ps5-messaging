// Command ps5-messaging serves the PlayStation groups and messages API
// (m.np.playstation.com /api/gamingLoungeGroups/).
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

	"github.com/openpak/ps5-common/config"
	"github.com/openpak/ps5-common/httpx"
	"github.com/openpak/ps5-common/oauth"
	"github.com/openpak/ps5-common/pgdb"
	"openpak/ps5-messaging/internal/messaging"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}
	if cfg.TokenKey == "" {
		log.Error("config", "err", "PS5_TOKEN_KEY is required")
		os.Exit(2)
	}
	signer, err := oauth.NewSigner([]byte(cfg.TokenKey))
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var store messaging.Store
	if cfg.DatabaseURL == "" && config.DevMemory(os.Getenv) {
		log.Warn("PS5_DEV_MEMORY=1: messages are kept in memory and lost on restart (development only)")
		store = messaging.NewMemory()
	} else {
		pool, err := pgdb.Open(ctx, cfg.DatabaseURL, pgdb.DefaultMaxConns)
		if err != nil {
			log.Error("database", "err", err)
			os.Exit(2)
		}
		defer pool.Close()
		if store, err = messaging.NewPostgres(ctx, pool); err != nil {
			log.Error("database schema", "err", err)
			os.Exit(2)
		}
		log.Info("groups and messages stored in Postgres")
	}
	h := httpx.Logger(log, messaging.Handler(store, signer))
	srv := &http.Server{Addr: cfg.Addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Info("ps5-messaging listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
