package models

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Some tests here need a MongoDB reachable at TEST_DATABASE_URL (default
// mongodb://localhost:27017) and wipe the connoisseur_models_test database.
// They skip, rather than fail, when no MongoDB is available.

var (
	mongoAvailable bool
	testDB         *mongo.Database
)

func TestMain(m *testing.M) {
	// Migrate logs a summary line; failures report what they need themselves.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	uri := os.Getenv("TEST_DATABASE_URL")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err == nil && client.Ping(ctx, nil) == nil {
		mongoAvailable = true
		testDB = client.Database("connoisseur_models_test")
		if err := Init(testDB); err != nil {
			panic(err)
		}
	}

	code := m.Run()

	if client != nil {
		_ = client.Disconnect(context.Background())
	}
	os.Exit(code)
}

// requireMongo skips a test without a database, and gives the ones that run a
// clean set of collections to work with.
func requireMongo(t *testing.T) {
	t.Helper()
	if !mongoAvailable {
		t.Skip("no MongoDB available; set TEST_DATABASE_URL to run these tests")
	}

	ctx := context.Background()
	for _, name := range []string{"users", "restaurants", "comments"} {
		if _, err := testDB.Collection(name).DeleteMany(ctx, bson.M{}); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
	}

	// Indexes outlive DeleteMany, and the unique review index is created by
	// Migrate rather than Init. Dropping it gives each test the same starting
	// point as a fresh database, which is what lets the migration tests seed the
	// duplicates they exist to clean up.
	err := testDB.Collection("comments").Indexes().DropOne(ctx, uniqueReviewIndexName)
	if err != nil && !strings.Contains(err.Error(), "index not found") {
		t.Fatalf("dropping the unique review index: %v", err)
	}
}

const uniqueReviewIndexName = "restaurantId_1_author.id_1"

// requireUniqueReviewIndex installs the index that enforces one review per
// author. Tests of that rule need it, because the rule is the index.
func requireUniqueReviewIndex(t *testing.T) {
	t.Helper()
	if err := createUniqueReviewIndex(context.Background()); err != nil {
		t.Fatalf("creating the unique review index: %v", err)
	}
}

// newAuthor returns a distinct review author.
func newAuthor(username string) Author {
	return Author{ID: bson.NewObjectID(), Username: username}
}

// newRestaurant inserts a restaurant and returns it.
func newRestaurant(t *testing.T, name string) *Restaurant {
	t.Helper()
	restaurant := &Restaurant{
		Name:        name,
		Image:       "https://example.com/photo.jpg",
		Cuisine:     "Testing",
		PriceRange:  "$$",
		Description: "A restaurant used by the tests.",
		Author:      Author{ID: bson.NewObjectID(), Username: "tester"},
	}
	if err := CreateRestaurant(context.Background(), restaurant); err != nil {
		t.Fatalf("creating restaurant %q: %v", name, err)
	}
	return restaurant
}

// reload fetches a restaurant's current state from the database.
func reload(t *testing.T, id bson.ObjectID) *Restaurant {
	t.Helper()
	restaurant, err := FindRestaurantByID(context.Background(), id)
	if err != nil {
		t.Fatalf("reloading restaurant: %v", err)
	}
	return restaurant
}
