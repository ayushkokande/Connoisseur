package web

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

// seedReviews adds count reviews to a restaurant, one per author, since one
// person may only review a restaurant once.
func seedReviews(t *testing.T, restaurantID string, count int) {
	t.Helper()
	ctx := context.Background()
	id := mustID(t, restaurantID)

	for i := range count {
		author := models.Author{
			ID:       bson.NewObjectID(),
			Username: fmt.Sprintf("page_reviewer_%d", i),
		}
		text := fmt.Sprintf("Review number %d.", i+1)
		if _, err := models.CreateComment(ctx, id, 4, text, author); err != nil {
			t.Fatalf("seeding review %d: %v", i+1, err)
		}
	}
}

func TestReviewsURL(t *testing.T) {
	const id = "6870d1f2e4b0a1c2d3e4f5a6"

	// The first page is the plain restaurant URL, so the common case stays
	// shareable, and every link lands on the reviews rather than the top.
	if got, want := reviewsURL(id, 1), "/restaurants/"+id+"#reviews"; got != want {
		t.Errorf("page 1 = %q, want %q", got, want)
	}
	if got, want := reviewsURL(id, 3), "/restaurants/"+id+"?page=3#reviews"; got != want {
		t.Errorf("page 3 = %q, want %q", got, want)
	}
}

func TestReviewPageLinks(t *testing.T) {
	const id = "6870d1f2e4b0a1c2d3e4f5a6"

	single := &models.CommentPage{Page: 1, TotalPages: 1}
	if links := reviewPageLinks(id, single); links != nil {
		t.Errorf("a single page produced %d links, want none", len(links))
	}

	spread := &models.CommentPage{Page: 2, TotalPages: 3}
	links := reviewPageLinks(id, spread)
	if len(links) != 3 {
		t.Fatalf("%d links, want 3", len(links))
	}
	if !links[1].Current {
		t.Error("the current page is not marked")
	}
	if links[0].Current || links[2].Current {
		t.Error("a page other than the current one is marked current")
	}

	if got := adjacentReviewURL(id, spread, -1); got != reviewsURL(id, 1) {
		t.Errorf("previous = %q, want %q", got, reviewsURL(id, 1))
	}
	if got := adjacentReviewURL(id, spread, 1); got != reviewsURL(id, 3) {
		t.Errorf("next = %q, want %q", got, reviewsURL(id, 3))
	}
	if got := adjacentReviewURL(id, &models.CommentPage{Page: 1, TotalPages: 1}, 1); got != "" {
		t.Errorf("next from the only page = %q, want empty", got)
	}
}

// The restaurant page used to load and render every review a restaurant had,
// so one with thousands of them did that work on every request.
func TestRestaurantPageShowsOnePageOfReviews(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("review_pager")
	id := owner.createRestaurant("Much Reviewed Bistro")

	const total = models.DefaultReviewsPerPage + 3
	seedReviews(t, id, total)

	body, _ := owner.get("/restaurants/" + id)

	// Oldest first, so the first page holds the earliest reviews and not the
	// last few.
	if !strings.Contains(body, "Review number 1.") {
		t.Error("the first page does not start at the earliest review")
	}
	if strings.Contains(body, fmt.Sprintf("Review number %d.", total)) {
		t.Errorf("the newest review appears on the first page, so all %d were rendered", total)
	}
	if got := strings.Count(body, "Review number "); got != models.DefaultReviewsPerPage {
		t.Errorf("%d reviews rendered, want %d", got, models.DefaultReviewsPerPage)
	}

	// The remainder has to be reachable.
	if !strings.Contains(body, "?page=2#reviews") {
		t.Error("no link to the next page of reviews")
	}

	second, _ := owner.get("/restaurants/" + id + "?page=2")
	if !strings.Contains(second, fmt.Sprintf("Review number %d.", total)) {
		t.Errorf("the second page does not carry review %d", total)
	}
	if strings.Contains(second, "Review number 1.") {
		t.Error("the second page repeats the first review")
	}
}

// Every review has to appear on exactly one page, or paging loses some and
// shows others twice.
func TestReviewPagesCoverEveryReviewOnce(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("review_coverage")
	id := owner.createRestaurant("Complete Bistro")

	const total = models.DefaultReviewsPerPage*2 + 1
	seedReviews(t, id, total)

	seen := map[string]int{}
	for page := 1; page <= 3; page++ {
		body, _ := owner.get(fmt.Sprintf("/restaurants/%s?page=%d", id, page))
		for i := 1; i <= total; i++ {
			review := fmt.Sprintf("Review number %d.", i)
			if strings.Contains(body, review) {
				seen[review]++
			}
		}
	}

	for i := 1; i <= total; i++ {
		review := fmt.Sprintf("Review number %d.", i)
		switch seen[review] {
		case 1:
		case 0:
			t.Errorf("%q appeared on no page", review)
		default:
			t.Errorf("%q appeared on %d pages", review, seen[review])
		}
	}
}

// A visitor's own review may sit on a page they are not looking at, and the
// page still has to offer to edit it rather than invite a second one.
func TestOwnReviewIsOfferedFromAnyPage(t *testing.T) {
	requireMongo(t)

	reviewer := newBrowser(t)
	reviewer.register("review_owner")
	id := reviewer.createRestaurant("Buried Review Bistro")

	// The visitor reviews first, so theirs is the oldest and sits on page one;
	// everyone else's pushes it away from the later pages.
	commentID := reviewer.createRating(id, 5, "Mine, written first.")
	seedReviews(t, id, models.DefaultReviewsPerPage+2)

	for _, page := range []string{"", "?page=2"} {
		body, _ := reviewer.get("/restaurants/" + id + page)
		if !strings.Contains(body, "Edit your review") {
			t.Errorf("page %q does not offer to edit the visitor's own review", page)
		}
		if !strings.Contains(body, "/comments/"+commentID+"/edit") {
			t.Errorf("page %q does not link to the visitor's own review", page)
		}
		if strings.Contains(body, ">Add Review<") {
			t.Errorf("page %q invites a second review", page)
		}
	}
}

// A page number past the end, from a bookmark or a restaurant that has since
// lost reviews, should show the last page rather than an empty one.
func TestReviewPageBeyondTheEndShowsTheLastPage(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("review_overrun")
	id := owner.createRestaurant("Overrun Bistro")

	const total = models.DefaultReviewsPerPage + 2
	seedReviews(t, id, total)

	body, _ := owner.get("/restaurants/" + id + "?page=99")
	if !strings.Contains(body, fmt.Sprintf("Review number %d.", total)) {
		t.Error("a page past the end did not fall back to the last page of reviews")
	}
}

func TestFindCommentsPageClampsItsArguments(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	owner := newBrowser(t)
	owner.register("review_clamp")
	id := mustID(t, owner.createRestaurant("Clamped Bistro"))
	seedReviews(t, id.Hex(), 3)

	cases := map[string]struct {
		page, perPage int
		wantPage      int
		wantPerPage   int
	}{
		"negative page":     {-5, models.DefaultReviewsPerPage, 1, models.DefaultReviewsPerPage},
		"zero per page":     {1, 0, 1, models.DefaultReviewsPerPage},
		"excessive perPage": {1, 10000, 1, models.DefaultReviewsPerPage},
		"page past the end": {50, models.DefaultReviewsPerPage, 1, models.DefaultReviewsPerPage},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			page, err := models.FindCommentsPage(ctx, id, tc.page, tc.perPage)
			if err != nil {
				t.Fatal(err)
			}
			if page.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", page.Page, tc.wantPage)
			}
			if page.PerPage != tc.wantPerPage {
				t.Errorf("PerPage = %d, want %d", page.PerPage, tc.wantPerPage)
			}
			if page.Total != 3 {
				t.Errorf("Total = %d, want 3", page.Total)
			}
		})
	}
}

// A restaurant with no reviews still has to describe one empty page, or the
// controls divide by a page count of zero.
func TestFindCommentsPageOnARestaurantWithNoReviews(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("review_none")
	id := mustID(t, owner.createRestaurant("Unreviewed Bistro"))

	page, err := models.FindCommentsPage(context.Background(), id, 1, models.DefaultReviewsPerPage)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.Comments) != 0 {
		t.Errorf("got %d reviews totalling %d, want none", len(page.Comments), page.Total)
	}
	if page.Page != 1 || page.TotalPages != 1 {
		t.Errorf("page %d of %d, want 1 of 1", page.Page, page.TotalPages)
	}
	if page.HasPrev() || page.HasNext() {
		t.Error("an empty page reports neighbours")
	}
}
