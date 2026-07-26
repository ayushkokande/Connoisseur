package models

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Migrate brings existing data up to the current schema. It is idempotent and
// safe to run on every startup: each step is a no-op once applied.
//
// Reviews used to be linked by an array of comment IDs on the restaurant
// document. That reference now lives on the review instead, so the array has to
// be unrolled before anything can read reviews by restaurant.
//
// A restaurant may also only be reviewed once per author now, which older data
// does not necessarily satisfy, so duplicates are removed here. That is why the
// unique index is created at the end of this function rather than in Init:
// building it against data still containing duplicates fails, and an index
// build that fails during startup takes the whole application down.
func Migrate(ctx context.Context) error {
	linked, err := linkCommentsToRestaurants(ctx)
	if err != nil {
		return fmt.Errorf("linking legacy comments: %w", err)
	}

	deduped, err := removeDuplicateReviews(ctx)
	if err != nil {
		return fmt.Errorf("removing duplicate reviews: %w", err)
	}

	refreshed, err := refreshAllRatings(ctx)
	if err != nil {
		return fmt.Errorf("refreshing rating summaries: %w", err)
	}

	if err := createUniqueReviewIndex(ctx); err != nil {
		return fmt.Errorf("enforcing one review per author: %w", err)
	}

	if linked > 0 || deduped > 0 || refreshed > 0 {
		slog.Info("migrated legacy data",
			"comments_linked", linked,
			"duplicate_reviews_removed", deduped,
			"restaurants_refreshed", refreshed,
		)
	}
	return nil
}

// removeDuplicateReviews keeps each author's most recent review of a restaurant
// and deletes their earlier ones, since the newest is the opinion they would
// expect to find when they go to edit it. Restaurants that lose a review have
// their summary recomputed.
func removeDuplicateReviews(ctx context.Context) (int, error) {
	cursor, err := comments.Aggregate(ctx, []bson.M{
		{"$sort": bson.M{"createdAt": -1, "_id": -1}},
		{"$group": bson.M{
			"_id":          bson.M{"restaurantId": "$restaurantId", "authorId": "$author.id"},
			"ids":          bson.M{"$push": "$_id"},
			"restaurantId": bson.M{"$first": "$restaurantId"},
		}},
		{"$match": bson.M{"$expr": bson.M{"$gt": bson.A{bson.M{"$size": "$ids"}, 1}}}},
	})
	if err != nil {
		return 0, err
	}

	var groups []struct {
		IDs          []bson.ObjectID `bson:"ids"`
		RestaurantID bson.ObjectID   `bson:"restaurantId"`
	}
	if err := cursor.All(ctx, &groups); err != nil {
		return 0, err
	}

	removed := 0
	for _, group := range groups {
		superseded := group.IDs[1:]
		result, err := comments.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": superseded}})
		if err != nil {
			return removed, err
		}
		removed += int(result.DeletedCount)

		if err := RefreshRating(ctx, group.RestaurantID); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// createUniqueReviewIndex enforces one review per author per restaurant. Creating
// an index that already exists is a no-op, so this is safe to repeat.
func createUniqueReviewIndex(ctx context.Context) error {
	_, err := comments.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "restaurantId", Value: 1}, {Key: "author.id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

// linkCommentsToRestaurants copies each restaurant's embedded comment IDs onto
// the comments themselves, then drops the array. Reviews whose restaurant no
// longer exists were already unreachable and are removed.
func linkCommentsToRestaurants(ctx context.Context) (int, error) {
	cursor, err := restaurants.Find(ctx, bson.M{"comments": bson.M{"$exists": true}})
	if err != nil {
		return 0, err
	}

	var legacy []struct {
		ID       bson.ObjectID   `bson:"_id"`
		Comments []bson.ObjectID `bson:"comments"`
	}
	if err := cursor.All(ctx, &legacy); err != nil {
		return 0, err
	}

	linked := 0
	for _, restaurant := range legacy {
		if len(restaurant.Comments) > 0 {
			result, err := comments.UpdateMany(ctx,
				bson.M{"_id": bson.M{"$in": restaurant.Comments}, "restaurantId": bson.M{"$exists": false}},
				bson.M{"$set": bson.M{"restaurantId": restaurant.ID}})
			if err != nil {
				return linked, err
			}
			linked += int(result.ModifiedCount)
		}

		if _, err := restaurants.UpdateOne(ctx,
			bson.M{"_id": restaurant.ID},
			bson.M{"$unset": bson.M{"comments": ""}}); err != nil {
			return linked, err
		}
	}

	// Anything still unlinked belonged to a restaurant that no longer exists, so
	// it was already unreachable. This runs even when no restaurant carried a
	// legacy array, since orphans can outlive the restaurants that referenced them.
	if _, err := comments.DeleteMany(ctx, bson.M{"restaurantId": bson.M{"$exists": false}}); err != nil {
		return linked, err
	}
	return linked, nil
}

// refreshAllRatings fills in the review count and average for restaurants that
// have never had one computed.
func refreshAllRatings(ctx context.Context) (int, error) {
	cursor, err := restaurants.Find(ctx,
		bson.M{"$or": []bson.M{
			{"reviewCount": bson.M{"$exists": false}},
			{"avgRating": bson.M{"$exists": false}},
		}})
	if err != nil {
		return 0, err
	}

	var stale []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cursor.All(ctx, &stale); err != nil {
		return 0, err
	}

	for _, restaurant := range stale {
		if err := RefreshRating(ctx, restaurant.ID); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}
