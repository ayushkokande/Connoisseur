package models

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Author is the denormalized owner reference stored on restaurants and comments.
type Author struct {
	ID       bson.ObjectID `bson:"id"`
	Username string        `bson:"username"`
}

type Restaurant struct {
	ID          bson.ObjectID   `bson:"_id,omitempty"`
	Name        string          `bson:"name"`
	Image       string          `bson:"image"`
	Cuisine     string          `bson:"cuisine"`
	PriceRange  string          `bson:"priceRange"`
	Description string          `bson:"description"`
	CreatedAt   time.Time       `bson:"createdAt"`
	Author      Author          `bson:"author"`
	Comments    []bson.ObjectID `bson:"comments"`
}

func CreateRestaurant(ctx context.Context, r *Restaurant) error {
	r.ID = bson.NewObjectID()
	r.CreatedAt = time.Now()
	if r.Comments == nil {
		r.Comments = []bson.ObjectID{}
	}
	_, err := restaurants.InsertOne(ctx, r)
	return err
}

func FindAllRestaurants(ctx context.Context) ([]Restaurant, error) {
	cursor, err := restaurants.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var results []Restaurant
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func FindRestaurantByID(ctx context.Context, id bson.ObjectID) (*Restaurant, error) {
	var r Restaurant
	if err := restaurants.FindOne(ctx, bson.M{"_id": id}).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func UpdateRestaurant(ctx context.Context, id bson.ObjectID, fields bson.M) error {
	_, err := restaurants.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": fields})
	return err
}

func DeleteRestaurant(ctx context.Context, id bson.ObjectID) (*Restaurant, error) {
	var r Restaurant
	if err := restaurants.FindOneAndDelete(ctx, bson.M{"_id": id}).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

func AddCommentToRestaurant(ctx context.Context, restaurantID, commentID bson.ObjectID) error {
	_, err := restaurants.UpdateOne(ctx, bson.M{"_id": restaurantID},
		bson.M{"$push": bson.M{"comments": commentID}})
	return err
}

func RemoveCommentFromRestaurant(ctx context.Context, restaurantID, commentID bson.ObjectID) error {
	_, err := restaurants.UpdateOne(ctx, bson.M{"_id": restaurantID},
		bson.M{"$pull": bson.M{"comments": commentID}})
	return err
}

func DeleteAllRestaurants(ctx context.Context) error {
	_, err := restaurants.DeleteMany(ctx, bson.M{})
	return err
}

func DeleteAllComments(ctx context.Context) error {
	_, err := comments.DeleteMany(ctx, bson.M{})
	return err
}
