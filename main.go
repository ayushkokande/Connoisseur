package main

import (
	"context"
	"log"
	"net/http"
	"os"
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

func main() {
	databaseURL := env("DATABASE_URL", "mongodb://localhost:27017")
	databaseName := env("DATABASE_NAME", "connoisseur")

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

	web.InitSessions(env("SESSION_SECRET", "connoisseur-dev-secret"))
	if err := web.InitTemplates("templates"); err != nil {
		log.Fatalf("ERROR: %v", err)
	}

	if os.Getenv("SEED_DB") == "true" {
		seedDB()
	}

	port := env("PORT", "3000")
	log.Println("Connoisseur server has started and is listening on port: " + port)
	if err := http.ListenAndServe(":"+port, web.Routes("public")); err != nil {
		log.Fatalf("ERROR: %v", err)
	}
}
