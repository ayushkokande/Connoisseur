package models

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	database    *mongo.Database
	users       *mongo.Collection
	restaurants *mongo.Collection
	comments    *mongo.Collection
)

// searchIndexName is the text index backing restaurant search.
const searchIndexName = "restaurant_search"

// ErrNotInitialized is returned when the package is used before Init succeeds.
var ErrNotInitialized = errors.New("models: database not initialized")

// Init connects the package to the given database and ensures indexes.
func Init(db *mongo.Database) error {
	database = db
	users = db.Collection("users")
	restaurants = db.Collection("restaurants")
	comments = db.Collection("comments")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// The unique index on usernameLower, like the one on reviews below, is
	// created by Migrate: older data can hold names that collide once case is
	// ignored, and those have to be settled before the index will build.

	// These back the sort orders and filters on the restaurant index, and the
	// text index backs the search. A search is answered from the text index
	// where it can be, and only falls back to a substring scan for the partial
	// words the index cannot match.
	if _, err := restaurants.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "createdAt", Value: -1}}},
		{Keys: bson.D{{Key: "name", Value: 1}}},
		{Keys: bson.D{{Key: "cuisine", Value: 1}}},
		{Keys: bson.D{{Key: "priceRange", Value: 1}}},
		{Keys: bson.D{{Key: "avgRating", Value: -1}}},
		{
			// A collection may only have one text index, so all three searched
			// fields belong to this one.
			Keys: bson.D{
				{Key: "name", Value: "text"},
				{Key: "cuisine", Value: "text"},
				{Key: "description", Value: "text"},
			},
			Options: options.Index().SetName(searchIndexName),
		},
	}); err != nil {
		return err
	}

	// Every review read is scoped to one restaurant. The unique index enforcing
	// one review per author is created by Migrate instead, which has to clear out
	// any pre-existing duplicates before the index can build.
	_, err := comments.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "restaurantId", Value: 1}, {Key: "createdAt", Value: 1}},
	})
	return err
}

// Ping reports whether the database is reachable.
func Ping(ctx context.Context) error {
	if database == nil {
		return ErrNotInitialized
	}
	return database.Client().Ping(ctx, nil)
}
