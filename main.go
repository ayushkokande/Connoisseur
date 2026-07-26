package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/ayushkokande/Connoisseur/models"
	"github.com/ayushkokande/Connoisseur/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// fatal logs a final error and exits, since slog has no Fatal equivalent.
func fatal(msg string, args ...any) {
	slog.Error(msg, args...)
	os.Exit(1)
}

// newLogger emits JSON in production so a log collector can parse it, and
// human-readable text everywhere else.
func newLogger(production bool) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if production {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// secret returns the named secret. In production it must be supplied; in
// development a random one is generated, which logs everyone out on restart but
// never ships a known key.
func secret(key string, production bool) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if production {
		fatal("secret must be set when APP_ENV=production", "variable", key)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		fatal("generating development secret", "variable", key, "error", err)
	}
	slog.Warn("secret is unset; generated a random development key", "variable", key)
	return hex.EncodeToString(buf)
}

func main() {
	production := os.Getenv("APP_ENV") == "production"
	slog.SetDefault(newLogger(production))

	databaseURL := env("DATABASE_URL", "mongodb://localhost:27017")
	databaseName := env("DATABASE_NAME", "connoisseur")

	sessionSecret := secret("SESSION_SECRET", production)
	csrfSecret := secret("CSRF_SECRET", production)

	client, err := mongo.Connect(options.Client().ApplyURI(databaseURL))
	if err != nil {
		fatal("configuring MongoDB client", "error", err)
	}
	defer client.Disconnect(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		fatal("could not reach MongoDB", "error", err)
	}
	slog.Info("connected to the Connoisseur database", "database", databaseName)

	if err := models.Init(client.Database(databaseName)); err != nil {
		fatal("initializing models", "error", err)
	}

	// Cookies are marked Secure only in production; over plain HTTP a Secure
	// cookie is never sent back and login would silently fail.
	web.InitSessions(sessionSecret, production)
	if err := web.InitTemplates("templates"); err != nil {
		fatal("parsing templates", "error", err)
	}

	if os.Getenv("SEED_DB") == "true" {
		seedDB()
	}

	port := env("PORT", "3000")
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      web.Routes("public", csrfSecret, production),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 20 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("server listening", "port", port, "production", production)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal("server stopped unexpectedly", "error", err)
		}
	}()

	<-shutdown
	slog.Info("shutting down")
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err := server.Shutdown(stopCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}
}
