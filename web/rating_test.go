package web

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/models"
)

// seedRated inserts a restaurant with the given ratings already reviewed, which
// is faster and clearer than driving the form once per review.
func seedRated(t *testing.T, name string, ratings ...int) *models.Restaurant {
	t.Helper()
	ctx := context.Background()

	restaurant := &models.Restaurant{
		Name:        name,
		Image:       "https://example.com/photo.jpg",
		Cuisine:     "Rated",
		PriceRange:  "$$",
		Description: "Seeded for rating tests.",
		Author:      models.Author{ID: bson.NewObjectID(), Username: "rating_seeder"},
	}
	if err := models.CreateRestaurant(ctx, restaurant); err != nil {
		t.Fatalf("creating %q: %v", name, err)
	}

	author := models.Author{ID: bson.NewObjectID(), Username: "rating_reviewer"}
	for _, rating := range ratings {
		if _, err := models.CreateComment(ctx, restaurant.ID, rating, "Seeded review.", author); err != nil {
			t.Fatalf("reviewing %q: %v", name, err)
		}
	}
	return restaurant
}

func TestReviewUpdatesRestaurantRating(t *testing.T) {
	requireMongo(t)

	reviewer := newBrowser(t)
	reviewer.register("rating_author")
	id := reviewer.createRestaurant("Rated Bistro")

	reviewer.createRating(id, 5, "Outstanding from start to finish.")

	body, _ := reviewer.get("/restaurants/" + id)
	if !strings.Contains(body, "★★★★★") {
		t.Error("five-star review did not render five stars on the restaurant page")
	}
	if !strings.Contains(body, "5.0 from 1 review") {
		t.Errorf("rating summary missing or wrong:\n%s", body)
	}

	restaurant, err := models.FindRestaurantByID(context.Background(), mustID(t, id))
	if err != nil {
		t.Fatal(err)
	}
	if restaurant.AvgRating != 5 || restaurant.ReviewCount != 1 {
		t.Errorf("stored summary is %d reviews averaging %v, want 1 and 5",
			restaurant.ReviewCount, restaurant.AvgRating)
	}
}

func TestReviewWithoutValidRatingIsRejected(t *testing.T) {
	requireMongo(t)

	reviewer := newBrowser(t)
	reviewer.register("rating_invalid")
	id := reviewer.createRestaurant("Strict Bistro")

	for _, rating := range []string{"", "0", "6", "five"} {
		t.Run("rating="+rating, func(t *testing.T) {
			resp := reviewer.post("/restaurants/"+id+"/comments/new",
				"/restaurants/"+id+"/comments",
				url.Values{"rating": {rating}, "text": {"Trying to skip the stars."}})
			resp.Body.Close()

			reviews, err := models.FindCommentsByRestaurant(context.Background(), mustID(t, id))
			if err != nil {
				t.Fatal(err)
			}
			if len(reviews) != 0 {
				t.Errorf("review with rating %q was stored", rating)
			}
		})
	}
}

func TestEditingAReviewUpdatesTheAverage(t *testing.T) {
	requireMongo(t)

	reviewer := newBrowser(t)
	reviewer.register("rating_editor")
	id := reviewer.createRestaurant("Revised Bistro")
	commentID := reviewer.createRating(id, 2, "Disappointing.")

	resp := reviewer.post(
		"/restaurants/"+id+"/comments/"+commentID+"/edit",
		"/restaurants/"+id+"/comments/"+commentID+"?_method=PUT",
		url.Values{"rating": {"5"}, "text": {"They turned it around."}})
	resp.Body.Close()

	restaurant, err := models.FindRestaurantByID(context.Background(), mustID(t, id))
	if err != nil {
		t.Fatal(err)
	}
	if restaurant.AvgRating != 5 {
		t.Errorf("average = %v after raising the review to 5 stars, want 5", restaurant.AvgRating)
	}
}

func TestDeletingARestaurantRemovesItsReviews(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("rating_deleter")
	id := owner.createRestaurant("Doomed Bistro")
	owner.createRating(id, 4, "Enjoyed it while it lasted.")

	resp := owner.post("/restaurants/"+id, "/restaurants/"+id+"?_method=DELETE", url.Values{})
	resp.Body.Close()

	reviews, err := models.FindCommentsByRestaurant(context.Background(), mustID(t, id))
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 0 {
		t.Errorf("%d review(s) outlived their restaurant", len(reviews))
	}
}

func TestIndexShowsRatingsOnCards(t *testing.T) {
	requireMongo(t)

	seedRated(t, "Loved Bistro", 5, 4)
	seedRated(t, "Unreviewed Bistro")

	body, _ := newBrowser(t).get("/restaurants")
	if !strings.Contains(body, "4.5") {
		t.Error("index card does not show the average rating")
	}
	if !strings.Contains(body, "Not yet rated") {
		t.Error("index card for an unreviewed restaurant does not say so")
	}
}

func TestIndexSortsByRating(t *testing.T) {
	requireMongo(t)

	seedRated(t, "Middling Bistro", 3)
	seedRated(t, "Excellent Bistro", 5, 5)
	seedRated(t, "Poor Bistro", 1)

	body, _ := newBrowser(t).get("/restaurants?sort=" + models.SortRating)

	best := strings.Index(body, "Excellent Bistro")
	middle := strings.Index(body, "Middling Bistro")
	worst := strings.Index(body, "Poor Bistro")
	if best < 0 || middle < 0 || worst < 0 {
		t.Fatal("not all restaurants rendered")
	}
	if !(best < middle && middle < worst) {
		t.Error("top-rated sort did not order the restaurants best to worst")
	}
}

// A single five-star review should not outrank a restaurant with many.
func TestRatingSortBreaksTiesByReviewCount(t *testing.T) {
	requireMongo(t)

	seedRated(t, "Popular Bistro", 5, 5, 5)
	seedRated(t, "Lucky Bistro", 5)

	body, _ := newBrowser(t).get("/restaurants?sort=" + models.SortRating)

	popular := strings.Index(body, "Popular Bistro")
	lucky := strings.Index(body, "Lucky Bistro")
	if popular < 0 || lucky < 0 {
		t.Fatal("not all restaurants rendered")
	}
	if popular > lucky {
		t.Error("a restaurant with one 5-star review outranked one with three")
	}
}

func TestIndexFiltersByMinimumRating(t *testing.T) {
	requireMongo(t)

	seedRated(t, "Great Bistro", 5)
	seedRated(t, "Average Bistro", 3)
	seedRated(t, "Unreviewed Bistro")

	anon := newBrowser(t)

	t.Run("four stars and up", func(t *testing.T) {
		body, _ := anon.get("/restaurants?rating=4")
		if !strings.Contains(body, "Great Bistro") {
			t.Error("a 5-star restaurant was excluded by a 4+ filter")
		}
		for _, excluded := range []string{"Average Bistro", "Unreviewed Bistro"} {
			if strings.Contains(body, excluded) {
				t.Errorf("%q passed a 4+ star filter", excluded)
			}
		}
	})

	t.Run("out of range rating is ignored", func(t *testing.T) {
		body, _ := anon.get("/restaurants?rating=9")
		if n := cardCount(body); n != 3 {
			t.Errorf("got %d restaurants, want all 3", n)
		}
	})
}

// The rating filter has to survive paging like every other filter.
func TestRatingFilterSurvivesPagination(t *testing.T) {
	requireMongo(t)

	for i := range models.DefaultPerPage + 1 {
		seedRated(t, "Top Bistro "+strconv.Itoa(i), 5)
	}
	seedRated(t, "Weak Bistro", 1)

	body, _ := newBrowser(t).get("/restaurants?rating=4")
	if got := pageTwoLink(t, body).Query().Get("rating"); got != "4" {
		t.Errorf("page 2 link carries rating=%q, want 4", got)
	}

	secondPage, _ := newBrowser(t).get("/restaurants?rating=4&page=2")
	if strings.Contains(secondPage, "Weak Bistro") {
		t.Error("page 2 of a 4+ star filter included a 1-star restaurant")
	}
}
