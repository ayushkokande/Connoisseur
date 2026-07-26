package models

import (
	"context"
	"math"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Author is the denormalized owner reference stored on restaurants and comments.
type Author struct {
	ID       bson.ObjectID `bson:"id"`
	Username string        `bson:"username"`
}

type Restaurant struct {
	ID          bson.ObjectID `bson:"_id,omitempty"`
	Name        string        `bson:"name"`
	Image       string        `bson:"image"`
	Cuisine     string        `bson:"cuisine"`
	PriceRange  string        `bson:"priceRange"`
	Description string        `bson:"description"`
	CreatedAt   time.Time     `bson:"createdAt"`
	Author      Author        `bson:"author"`

	// ReviewCount and AvgRating summarise the reviews in the comments
	// collection. They are derived data, kept on the document so the index can
	// sort and filter by rating without joining, and recomputed from source by
	// RefreshRating after every review write.
	ReviewCount int     `bson:"reviewCount"`
	AvgRating   float64 `bson:"avgRating"`
}

// IsRated reports whether there is an average worth showing. It tests the
// average rather than the review count, because a restaurant reviewed only
// before ratings existed has reviews but nothing to put stars on.
func (r Restaurant) IsRated() bool { return r.AvgRating > 0 }

// Stars renders the average rating, rounded to the nearest whole star.
func (r Restaurant) Stars() string { return Stars(int(math.Round(r.AvgRating))) }

// DisplayRating is the average rounded to one decimal place.
func (r Restaurant) DisplayRating() string {
	return strconv.FormatFloat(r.AvgRating, 'f', 1, 64)
}

func CreateRestaurant(ctx context.Context, r *Restaurant) error {
	if err := r.Validate(); err != nil {
		return err
	}
	r.ID = bson.NewObjectID()
	r.CreatedAt = time.Now()
	r.ReviewCount = 0
	r.AvgRating = 0
	_, err := restaurants.InsertOne(ctx, r)
	return err
}

func FindRestaurantByID(ctx context.Context, id bson.ObjectID) (*Restaurant, error) {
	var r Restaurant
	if err := restaurants.FindOne(ctx, bson.M{"_id": id}).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateRestaurant validates and writes the user-editable fields of r. Author,
// createdAt and the rating summary are deliberately not touched.
func UpdateRestaurant(ctx context.Context, id bson.ObjectID, r *Restaurant) error {
	if err := r.Validate(); err != nil {
		return err
	}
	fields := bson.M{
		"name":        r.Name,
		"image":       r.Image,
		"cuisine":     r.Cuisine,
		"priceRange":  r.PriceRange,
		"description": r.Description,
	}
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

// RefreshRating recomputes a restaurant's review count and average from the
// reviews themselves. Deriving the summary rather than adjusting it means a
// write that fails partway leaves stale numbers rather than permanently wrong
// ones: the next review, or a repair pass, puts them right. Unrated legacy
// reviews are counted but excluded from the average.
func RefreshRating(ctx context.Context, restaurantID bson.ObjectID) error {
	cursor, err := comments.Aggregate(ctx, []bson.M{
		{"$match": bson.M{"restaurantId": restaurantID}},
		{"$group": bson.M{
			"_id":     nil,
			"count":   bson.M{"$sum": 1},
			"ratings": bson.M{"$push": bson.M{"$cond": bson.A{bson.M{"$gte": bson.A{"$rating", minRating}}, "$rating", "$$REMOVE"}}},
		}},
	})
	if err != nil {
		return err
	}

	var summaries []struct {
		Count   int   `bson:"count"`
		Ratings []int `bson:"ratings"`
	}
	if err := cursor.All(ctx, &summaries); err != nil {
		return err
	}

	count, average := 0, 0.0
	if len(summaries) > 0 {
		count = summaries[0].Count
		if rated := summaries[0].Ratings; len(rated) > 0 {
			total := 0
			for _, rating := range rated {
				total += rating
			}
			average = float64(total) / float64(len(rated))
		}
	}

	_, err = restaurants.UpdateOne(ctx,
		bson.M{"_id": restaurantID},
		bson.M{"$set": bson.M{"reviewCount": count, "avgRating": average}})
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
