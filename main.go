package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/shivamdubey91/connoisseur/models"
	"github.com/shivamdubey91/connoisseur/web"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// secret returns the named secret. In production it must be supplied; in
// development a random one is generated, which logs everyone out on restart but
// never ships a known key.
func secret(key string, production bool) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if production {
		log.Fatalf("ERROR: %s must be set when APP_ENV=production", key)
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		log.Fatalf("ERROR: generating development %s: %v", key, err)
	}
	log.Printf("WARNING: %s is unset; generated a random development key", key)
	return hex.EncodeToString(buf)
}

func main() {
	production := os.Getenv("APP_ENV") == "production"
	databaseURL := env("DATABASE_URL", "mongodb://localhost:27017")
	databaseName := env("DATABASE_NAME", "connoisseur")

	sessionSecret := secret("SESSION_SECRET", production)
	csrfSecret := secret("CSRF_SECRET", production)

	client, err := mongo.Connect(options.Client().ApplyURI(databaseURL))
	if err != nil {
		log.Fatalf("ERROR: %v", err)
	}
	defer client.Disconnect(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("ERROR: could not reach MongoDB: %v", err)
	}
	log.Println("Successfully connected to the Connoisseur database!")

	if err := models.Init(client.Database(databaseName)); err != nil {
		log.Fatalf("ERROR: initializing models: %v", err)
	}

	// Cookies are marked Secure only in production; over plain HTTP a Secure
	// cookie is never sent back and login would silently fail.
	web.InitSessions(sessionSecret, production)
	if err := web.InitTemplates("templates"); err != nil {
		log.Fatalf("ERROR: %v", err)
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
		log.Println("Connoisseur server has started and is listening on port: " + port)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("ERROR: %v", err)
		}
	}()

	<-shutdown
	log.Println("Shutting down...")
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer stopCancel()
	if err := server.Shutdown(stopCtx); err != nil {
		log.Printf("ERROR: forced shutdown: %v", err)
	}
}
