package models

import (
	"context"
	"errors"
	"fmt"
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
	owner := bson.M{"id": bson.NewObjectID(), "username": "legacy_owner"}

	// Distinct authors, so both reviews survive the one-per-author rule and this
	// fixture isolates the linking behaviour from the deduplication behaviour.
	for i := range 2 {
		id := bson.NewObjectID()
		commentIDs = append(commentIDs, id)
		if _, err := comments.InsertOne(ctx, bson.M{
			"_id":       id,
			"text":      "A review from before ratings existed.",
			"createdAt": time.Now(),
			"author":    bson.M{"id": bson.NewObjectID(), "username": fmt.Sprintf("legacy_user_%d", i)},
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
		"author":      owner,
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

// Older data predates the one-review-per-author rule, so the migration has to
// clear duplicates out before the unique index can be built.
func TestMigrateRemovesDuplicateReviews(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	restaurant := newRestaurant(t, "Duplicated Bistro")
	author := newAuthor("prolific_reviewer")

	// Three reviews of one restaurant by one author, oldest first. The newest is
	// the opinion that should survive.
	base := time.Now().Add(-3 * time.Hour)
	for i, rating := range []int{1, 3, 5} {
		if _, err := comments.InsertOne(ctx, Comment{
			ID:           bson.NewObjectID(),
			RestaurantID: restaurant.ID,
			Rating:       rating,
			Text:         fmt.Sprintf("Visit number %d.", i+1),
			CreatedAt:    base.Add(time.Duration(i) * time.Hour),
			Author:       author,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	remaining, err := FindCommentsByRestaurant(ctx, restaurant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 1 {
		t.Fatalf("%d reviews remain, want 1", len(remaining))
	}
	if remaining[0].Rating != 5 {
		t.Errorf("the surviving review is rated %d, want 5 (the most recent)", remaining[0].Rating)
	}
	if remaining[0].Text != "Visit number 3." {
		t.Errorf("the surviving review reads %q, want the most recent one", remaining[0].Text)
	}

	// The summary has to reflect the deletions, not the three original reviews.
	summary := reload(t, restaurant.ID)
	if summary.ReviewCount != 1 || summary.AvgRating != 5 {
		t.Errorf("summary is %d reviews averaging %v, want 1 and 5",
			summary.ReviewCount, summary.AvgRating)
	}

	// Having cleaned up, the migration should have installed the index that stops
	// it happening again.
	if _, err := CreateComment(ctx, restaurant.ID, 5, "And again.", author); !errors.Is(err, ErrAlreadyReviewed) {
		t.Errorf("a further review returned %v, want ErrAlreadyReviewed", err)
	}
}

// The same author reviewing two different restaurants is not a duplicate.
func TestMigrateKeepsOneAuthorsReviewsOfDifferentRestaurants(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	first := newRestaurant(t, "First Bistro")
	second := newRestaurant(t, "Second Bistro")
	author := newAuthor("travelling_reviewer")

	for _, restaurant := range []*Restaurant{first, second} {
		if _, err := comments.InsertOne(ctx, Comment{
			ID:           bson.NewObjectID(),
			RestaurantID: restaurant.ID,
			Rating:       4,
			Text:         "Worth a visit.",
			CreatedAt:    time.Now(),
			Author:       author,
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx); err != nil {
		t.Fatalf("migrating: %v", err)
	}

	for _, restaurant := range []*Restaurant{first, second} {
		reviews, err := FindCommentsByRestaurant(ctx, restaurant.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(reviews) != 1 {
			t.Errorf("%s has %d reviews, want 1", restaurant.Name, len(reviews))
		}
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
