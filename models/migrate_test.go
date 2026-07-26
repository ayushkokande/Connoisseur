package models

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// legacyFixture writes documents in the pre-ratings shape: restaurants holding
// an array of comment IDs, comments with neither a restaurant reference nor a
// rating, and no summary fields anywhere.
func legacyFixture(t *testing.T) (restaurantID bson.ObjectID, commentIDs []bson.ObjectID) {
	t.Helper()
	ctx := context.Background()

	restaurantID = bson.NewObjectID()
	author := bson.M{"id": bson.NewObjectID(), "username": "legacy_user"}

	for range 2 {
		id := bson.NewObjectID()
		commentIDs = append(commentIDs, id)
		if _, err := comments.InsertOne(ctx, bson.M{
			"_id":       id,
			"text":      "A review from before ratings existed.",
			"createdAt": time.Now(),
			"author":    author,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := restaurants.InsertOne(ctx, bson.M{
		"_id":         restaurantID,
		"name":        "Legacy Bistro",
		"image":       "https://example.com/legacy.jpg",
		"cuisine":     "Classic",
		"priceRange":  "$$",
		"description": "Stored in the old shape.",
		"createdAt":   time.Now(),
		"author":      author,
		"comments":    commentIDs,
	}); err != nil {
		t.Fatal(err)
	}
	return restaurantID, commentIDs
}

func TestMigrateLinksLegacyComments(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	restaurantID, commentIDs := legacyFixture(t)

	if err := Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	reviews, err := FindCommentsByRestaurant(ctx, restaurantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != len(commentIDs) {
		t.Fatalf("found %d reviews after migration, want %d", len(reviews), len(commentIDs))
	}
	for _, review := range reviews {
		if review.RestaurantID != restaurantID {
			t.Errorf("review %s points at %s, want %s", review.ID.Hex(), review.RestaurantID.Hex(), restaurantID.Hex())
		}
		if review.IsRated() {
			t.Errorf("legacy review %s came out rated, want unrated", review.ID.Hex())
		}
	}

	// The embedded array is the thing being replaced, so it should be gone.
	var raw bson.M
	if err := restaurants.FindOne(ctx, bson.M{"_id": restaurantID}).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["comments"]; present {
		t.Error("the legacy comments array is still on the restaurant document")
	}

	// Legacy reviews are unrated, so they count but leave no average.
	summary := reload(t, restaurantID)
	if summary.ReviewCount != len(commentIDs) {
		t.Errorf("review count = %d, want %d", summary.ReviewCount, len(commentIDs))
	}
	if summary.AvgRating != 0 {
		t.Errorf("average = %v, want 0 for unrated legacy reviews", summary.AvgRating)
	}
}

// Migrate runs on every startup, so running it twice must not change anything
// or lose data.
func TestMigrateIsIdempotent(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	restaurantID, commentIDs := legacyFixture(t)

	if err := Migrate(ctx); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	first := reload(t, restaurantID)

	if err := Migrate(ctx); err != nil {
		t.Fatalf("second migration: %v", err)
	}
	second := reload(t, restaurantID)

	if first.ReviewCount != second.ReviewCount || first.AvgRating != second.AvgRating {
		t.Errorf("second run changed the summary: %d/%v then %d/%v",
			first.ReviewCount, first.AvgRating, second.ReviewCount, second.AvgRating)
	}

	reviews, err := FindCommentsByRestaurant(ctx, restaurantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != len(commentIDs) {
		t.Errorf("second run left %d reviews, want %d", len(reviews), len(commentIDs))
	}
}

// A comment whose restaurant was already gone was unreachable in the old shape
// and cannot be linked in the new one, so migration discards it.
func TestMigrateDropsOrphanedComments(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	orphan := bson.NewObjectID()
	if _, err := comments.InsertOne(ctx, bson.M{
		"_id":       orphan,
		"text":      "Belongs to a restaurant that no longer exists.",
		"createdAt": time.Now(),
		"author":    bson.M{"id": bson.NewObjectID(), "username": "ghost"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	count, err := comments.CountDocuments(ctx, bson.M{"_id": orphan})
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("orphaned comment survived the migration")
	}
}

// Migrate is called at every startup, including on a database that has never
// held anything.
func TestMigrateOnEmptyDatabase(t *testing.T) {
	requireMongo(t)

	if err := Migrate(context.Background()); err != nil {
		t.Fatalf("migrating an empty database: %v", err)
	}
}
