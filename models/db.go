package models

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	users       *mongo.Collection
	restaurants *mongo.Collection
	comments    *mongo.Collection
)

// Init connects the package to the given database and ensures indexes.
func Init(db *mongo.Database) error {
	users = db.Collection("users")
	restaurants = db.Collection("restaurants")
	comments = db.Collection("comments")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "username", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}
