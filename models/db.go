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

	if _, err := users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}

	// These back the sort orders and filters on the restaurant index. The free
	// text search is a substring regex and cannot use them.
	if _, err := restaurants.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "createdAt", Value: -1}}},
		{Keys: bson.D{{Key: "name", Value: 1}}},
		{Keys: bson.D{{Key: "cuisine", Value: 1}}},
		{Keys: bson.D{{Key: "priceRange", Value: 1}}},
		{Keys: bson.D{{Key: "avgRating", Value: -1}}},
	}); err != nil {
		return err
	}

	// Every review read is scoped to one restaurant.
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
