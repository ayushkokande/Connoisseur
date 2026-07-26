package models

import (
	"context"
	"fmt"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// Migrate brings existing data up to the current schema. It is idempotent and
// safe to run on every startup: each step is a no-op once applied.
//
// Reviews used to be linked by an array of comment IDs on the restaurant
// document. That reference now lives on the review instead, so the array has to
// be unrolled before anything can read reviews by restaurant.
func Migrate(ctx context.Context) error {
	linked, err := linkCommentsToRestaurants(ctx)
	if err != nil {
		return fmt.Errorf("linking legacy comments: %w", err)
	}

	refreshed, err := refreshAllRatings(ctx)
	if err != nil {
		return fmt.Errorf("refreshing rating summaries: %w", err)
	}

	if linked > 0 || refreshed > 0 {
		slog.Info("migrated legacy data",
			"comments_linked", linked,
			"restaurants_refreshed", refreshed,
		)
	}
	return nil
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
