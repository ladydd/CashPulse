package main

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cashpulse/internal/api"
	"cashpulse/internal/auth"
	"cashpulse/internal/config"
	"cashpulse/internal/parser"
	"cashpulse/internal/service"
	"cashpulse/internal/store"
)

//go:embed all:dist
var embeddedDist embed.FS

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}

	st, err := store.Open(cfg.DatabasePath)
	if err != nil {
		slog.Error("open store", "err", err)
		os.Exit(1)
	}
	defer st.Close()

	loc := cfg.Location
	p := parser.New(loc)
	svc := service.New(st, p, loc)

	sess := auth.NewStore(cfg.AdminPassword, cfg.SessionTTL, cfg.SecureCookie)
	a := &api.Auth{
		IngestToken: cfg.IngestToken,
		AdminToken:  cfg.AdminToken,
		Sessions:    sess,
		Guard:       auth.NewLoginGuard(),
	}
	h := api.NewHandler(svc, a)

	var staticFS fs.FS
	if sub, err := fs.Sub(embeddedDist, "dist"); err == nil {
		if f, err := sub.Open("index.html"); err == nil {
			_ = f.Close()
			staticFS = sub
			slog.Info("serving embedded web UI")
		}
	}

	router := api.NewRouter(h, staticFS)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("CashPulse listening",
			"addr", srv.Addr,
			"db", cfg.DatabasePath,
			"tz", cfg.Timezone,
			"password_login", cfg.AdminPassword != "",
			"secure_cookie", cfg.SecureCookie,
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	slog.Info("shutting down")
	_ = srv.Close()
}
