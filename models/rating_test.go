package models

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestValidateRating(t *testing.T) {
	cases := map[int]bool{
		0:  false,
		-1: false,
		1:  true,
		3:  true,
		5:  true,
		6:  false,
	}

	for rating, wantValid := range cases {
		err := validateRating(rating)
		if wantValid && err != nil {
			t.Errorf("rating %d was rejected: %v", rating, err)
		}
		if !wantValid && err == nil {
			t.Errorf("rating %d was accepted, want rejected", rating)
		}
		if err != nil && !IsValidationError(err) {
			t.Errorf("rating %d produced %T, want a user-facing ValidationError", rating, err)
		}
	}
}

func TestStars(t *testing.T) {
	cases := map[int]string{
		1: "★☆☆☆☆",
		3: "★★★☆☆",
		5: "★★★★★",
		// Off the scale, including the 0 carried by pre-ratings reviews.
		0:  "",
		6:  "",
		-2: "",
	}

	for rating, want := range cases {
		if got := Stars(rating); got != want {
			t.Errorf("Stars(%d) = %q, want %q", rating, got, want)
		}
	}
}

func TestRatingChoicesAreBestFirst(t *testing.T) {
	choices := RatingChoices()
	want := []int{5, 4, 3, 2, 1}

	if len(choices) != len(want) {
		t.Fatalf("RatingChoices() = %v, want %v", choices, want)
	}
	for i, choice := range choices {
		if choice != want[i] {
			t.Fatalf("RatingChoices() = %v, want %v", choices, want)
		}
	}
}

func TestReviewsMaintainRatingSummary(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	restaurant := newRestaurant(t, "Summary Bistro")
	author := Author{ID: bson.NewObjectID(), Username: "reviewer"}

	if got := reload(t, restaurant.ID); got.ReviewCount != 0 || got.AvgRating != 0 {
		t.Errorf("new restaurant has %d reviews averaging %v, want 0 and 0",
			got.ReviewCount, got.AvgRating)
	}

	first, err := CreateComment(ctx, restaurant.ID, 4, "Pretty good.", author)
	if err != nil {
		t.Fatal(err)
	}
	if got := reload(t, restaurant.ID); got.ReviewCount != 1 || got.AvgRating != 4 {
		t.Errorf("after one 4-star review: %d reviews averaging %v, want 1 and 4",
			got.ReviewCount, got.AvgRating)
	}

	if _, err := CreateComment(ctx, restaurant.ID, 2, "Not for me.", author); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, restaurant.ID); got.ReviewCount != 2 || got.AvgRating != 3 {
		t.Errorf("after adding a 2-star review: %d reviews averaging %v, want 2 and 3",
			got.ReviewCount, got.AvgRating)
	}

	if err := UpdateComment(ctx, first.ID, 5, "Better on a second visit."); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, restaurant.ID); got.AvgRating != 3.5 {
		t.Errorf("after raising a review to 5 stars: average %v, want 3.5", got.AvgRating)
	}

	if err := DeleteComment(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if got := reload(t, restaurant.ID); got.ReviewCount != 1 || got.AvgRating != 2 {
		t.Errorf("after deleting the 5-star review: %d reviews averaging %v, want 1 and 2",
			got.ReviewCount, got.AvgRating)
	}
}

// A restaurant's summary must not be affected by reviews of other restaurants.
func TestRatingSummaryIsScopedToOneRestaurant(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	first := newRestaurant(t, "First")
	second := newRestaurant(t, "Second")
	author := Author{ID: bson.NewObjectID(), Username: "reviewer"}

	if _, err := CreateComment(ctx, first.ID, 5, "Superb.", author); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateComment(ctx, second.ID, 1, "Dreadful.", author); err != nil {
		t.Fatal(err)
	}

	if got := reload(t, first.ID); got.AvgRating != 5 || got.ReviewCount != 1 {
		t.Errorf("first restaurant: %d reviews averaging %v, want 1 and 5", got.ReviewCount, got.AvgRating)
	}
	if got := reload(t, second.ID); got.AvgRating != 1 || got.ReviewCount != 1 {
		t.Errorf("second restaurant: %d reviews averaging %v, want 1 and 1", got.ReviewCount, got.AvgRating)
	}
}

// Reviews written before ratings existed carry rating 0. They still count as
// reviews but must not drag the average down to zero.
func TestUnratedReviewsAreExcludedFromTheAverage(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	restaurant := newRestaurant(t, "Legacy Bistro")
	author := Author{ID: bson.NewObjectID(), Username: "reviewer"}

	if _, err := CreateComment(ctx, restaurant.ID, 4, "Rated.", author); err != nil {
		t.Fatal(err)
	}
	// Written directly, since the model layer will not accept a 0 rating.
	if _, err := comments.InsertOne(ctx, Comment{
		ID:           bson.NewObjectID(),
		RestaurantID: restaurant.ID,
		Rating:       0,
		Text:         "Written before ratings existed.",
		Author:       author,
	}); err != nil {
		t.Fatal(err)
	}
	if err := RefreshRating(ctx, restaurant.ID); err != nil {
		t.Fatal(err)
	}

	got := reload(t, restaurant.ID)
	if got.ReviewCount != 2 {
		t.Errorf("review count = %d, want 2 (the unrated review still counts)", got.ReviewCount)
	}
	if got.AvgRating != 4 {
		t.Errorf("average = %v, want 4 (the unrated review is excluded)", got.AvgRating)
	}
}

// A restaurant whose only reviews are unrated has no average to show.
func TestOnlyUnratedReviewsLeaveNoAverage(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	restaurant := newRestaurant(t, "Unrated Bistro")
	if _, err := comments.InsertOne(ctx, Comment{
		ID:           bson.NewObjectID(),
		RestaurantID: restaurant.ID,
		Text:         "No stars on this one.",
		Author:       Author{ID: bson.NewObjectID(), Username: "reviewer"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := RefreshRating(ctx, restaurant.ID); err != nil {
		t.Fatal(err)
	}

	got := reload(t, restaurant.ID)
	if got.AvgRating != 0 {
		t.Errorf("average = %v, want 0", got.AvgRating)
	}
	if got.IsRated() {
		t.Error("IsRated() is true, but the restaurant has no rated reviews")
	}
}

func TestCreateCommentRejectsInvalidRating(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	restaurant := newRestaurant(t, "Strict Bistro")
	author := Author{ID: bson.NewObjectID(), Username: "reviewer"}

	for _, rating := range []int{0, -1, 6} {
		if _, err := CreateComment(ctx, restaurant.ID, rating, "Some text.", author); err == nil {
			t.Errorf("rating %d was accepted", rating)
		}
	}

	reviews, err := FindCommentsByRestaurant(ctx, restaurant.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 0 {
		t.Errorf("%d invalid review(s) were stored", len(reviews))
	}
}

func TestDisplayRating(t *testing.T) {
	cases := map[float64]string{
		0:     "0.0",
		3:     "3.0",
		4.25:  "4.2",
		4.5:   "4.5",
		4.666: "4.7",
	}

	for average, want := range cases {
		restaurant := Restaurant{AvgRating: average}
		if got := restaurant.DisplayRating(); got != want {
			t.Errorf("DisplayRating() for %v = %q, want %q", average, got, want)
		}
	}
}

func TestRestaurantStarsRoundToNearest(t *testing.T) {
	cases := map[float64]string{
		4.2: "★★★★☆",
		4.6: "★★★★★",
		3.5: "★★★★☆",
		0:   "",
	}

	for average, want := range cases {
		restaurant := Restaurant{AvgRating: average}
		if got := restaurant.Stars(); got != want {
			t.Errorf("Stars() for average %v = %q, want %q", average, got, want)
		}
	}
}
