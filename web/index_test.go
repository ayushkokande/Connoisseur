package web

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/models"
)

// seedRestaurants inserts restaurants directly, which keeps index tests focused
// on reading rather than on repeated form submissions.
func seedRestaurants(t *testing.T, specs ...models.Restaurant) {
	t.Helper()
	author := models.Author{ID: bson.NewObjectID(), Username: "index_seeder"}

	for i := range specs {
		restaurant := specs[i]
		restaurant.Author = author
		if restaurant.Image == "" {
			restaurant.Image = "https://example.com/photo.jpg"
		}
		if restaurant.Description == "" {
			restaurant.Description = "A place to eat."
		}
		if restaurant.PriceRange == "" {
			restaurant.PriceRange = "$$"
		}
		if err := models.CreateRestaurant(context.Background(), &restaurant); err != nil {
			t.Fatalf("seeding %q: %v", restaurant.Name, err)
		}
	}
}

// cardCount counts rendered restaurant cards on an index page.
func cardCount(body string) int {
	return strings.Count(body, "More Info")
}

var pageTwoPattern = regexp.MustCompile(`href="(/restaurants\?[^"]*page=2[^"]*)"`)

// pageTwoLink returns the parsed pagination link to page 2. Query parameters
// come out in whatever order url.Values.Encode produces, so tests inspect the
// parsed values rather than matching the raw string.
func pageTwoLink(t *testing.T, body string) *url.URL {
	t.Helper()

	match := pageTwoPattern.FindStringSubmatch(body)
	if match == nil {
		t.Fatal("no pagination link to page 2 was rendered")
	}
	link, err := url.Parse(html.UnescapeString(match[1]))
	if err != nil {
		t.Fatalf("parsing page 2 link: %v", err)
	}
	return link
}

func TestIndexSearchMatchesNameCuisineAndDescription(t *testing.T) {
	requireMongo(t)

	seedRestaurants(t,
		models.Restaurant{Name: "Sushi Palace", Cuisine: "Japanese", Description: "Omakase counter."},
		models.Restaurant{Name: "Pasta House", Cuisine: "Italian", Description: "Handmade tagliatelle."},
		models.Restaurant{Name: "Corner Diner", Cuisine: "American", Description: "Late night sushi, oddly."},
	)

	anon := newBrowser(t)

	cases := map[string]struct {
		search  string
		present []string
		absent  []string
	}{
		"partial word in name": {
			"Sush",
			[]string{"Sushi Palace", "Corner Diner"},
			[]string{"Pasta House"},
		},
		"matches cuisine": {
			"Italian",
			[]string{"Pasta House"},
			[]string{"Sushi Palace", "Corner Diner"},
		},
		"matches description": {
			"tagliatelle",
			[]string{"Pasta House"},
			[]string{"Sushi Palace"},
		},
		"case insensitive": {
			"PALACE",
			[]string{"Sushi Palace"},
			[]string{"Pasta House"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body, _ := anon.get("/restaurants?q=" + tc.search)
			for _, want := range tc.present {
				if !strings.Contains(body, want) {
					t.Errorf("searching %q did not return %q", tc.search, want)
				}
			}
			for _, unwanted := range tc.absent {
				if strings.Contains(body, unwanted) {
					t.Errorf("searching %q unexpectedly returned %q", tc.search, unwanted)
				}
			}
		})
	}
}

// A search term goes into a MongoDB regex, so metacharacters must be treated as
// literal text rather than as a pattern that matches everything.
func TestIndexSearchTreatsMetacharactersLiterally(t *testing.T) {
	requireMongo(t)

	seedRestaurants(t,
		models.Restaurant{Name: "Sushi Palace", Cuisine: "Japanese"},
		models.Restaurant{Name: "Pasta House", Cuisine: "Italian"},
	)

	body, _ := newBrowser(t).get("/restaurants?q=.%2A")
	if n := cardCount(body); n != 0 {
		t.Errorf("searching %q returned %d restaurants, want 0", ".*", n)
	}
	if !strings.Contains(body, "No restaurants match those filters") {
		t.Error("no-matches message was not shown")
	}
}

func TestIndexFiltersByCuisineAndPrice(t *testing.T) {
	requireMongo(t)

	seedRestaurants(t,
		models.Restaurant{Name: "Cheap Sushi", Cuisine: "Japanese", PriceRange: "$"},
		models.Restaurant{Name: "Fancy Sushi", Cuisine: "Japanese", PriceRange: "$$$$"},
		models.Restaurant{Name: "Cheap Pasta", Cuisine: "Italian", PriceRange: "$"},
	)

	anon := newBrowser(t)

	t.Run("by cuisine", func(t *testing.T) {
		body, _ := anon.get("/restaurants?cuisine=Japanese")
		if n := cardCount(body); n != 2 {
			t.Errorf("got %d restaurants, want 2", n)
		}
		if strings.Contains(body, "Cheap Pasta") {
			t.Error("Italian restaurant leaked into a Japanese filter")
		}
	})

	t.Run("by price", func(t *testing.T) {
		body, _ := anon.get("/restaurants?price=%24")
		if n := cardCount(body); n != 2 {
			t.Errorf("got %d restaurants, want 2", n)
		}
		if strings.Contains(body, "Fancy Sushi") {
			t.Error("$$$$ restaurant leaked into a $ filter")
		}
	})

	t.Run("combined", func(t *testing.T) {
		body, _ := anon.get("/restaurants?cuisine=Japanese&price=%24")
		if n := cardCount(body); n != 1 {
			t.Errorf("got %d restaurants, want 1", n)
		}
		if !strings.Contains(body, "Cheap Sushi") {
			t.Error("combined filter did not return Cheap Sushi")
		}
	})

	// A price range that is not on the allowlist is ignored rather than erroring.
	t.Run("unknown price is ignored", func(t *testing.T) {
		body, _ := anon.get("/restaurants?price=free")
		if n := cardCount(body); n != 3 {
			t.Errorf("got %d restaurants, want all 3", n)
		}
	})
}

func TestIndexSortOrder(t *testing.T) {
	requireMongo(t)

	// Inserted in this order, so "Alpha" is the oldest and "Charlie" the newest.
	seedRestaurants(t,
		models.Restaurant{Name: "Bravo", Cuisine: "Thai"},
		models.Restaurant{Name: "Alpha", Cuisine: "Thai"},
		models.Restaurant{Name: "Charlie", Cuisine: "Thai"},
	)

	anon := newBrowser(t)

	cases := map[string]struct {
		sort  string
		first string
	}{
		"newest first (default)": {"", "Charlie"},
		"oldest first":           {models.SortOldest, "Bravo"},
		"by name":                {models.SortName, "Alpha"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body, _ := anon.get("/restaurants?sort=" + tc.sort)
			positions := map[string]int{}
			for _, restaurant := range []string{"Alpha", "Bravo", "Charlie"} {
				positions[restaurant] = strings.Index(body, ">"+restaurant+"<")
			}
			for restaurant, at := range positions {
				if at < 0 {
					t.Fatalf("%q is missing from the page", restaurant)
				}
				if restaurant != tc.first && at < positions[tc.first] {
					t.Errorf("%q appears before %q, which should be first", restaurant, tc.first)
				}
			}
		})
	}
}

func TestIndexPaginates(t *testing.T) {
	requireMongo(t)

	const extra = 3
	specs := make([]models.Restaurant, 0, models.DefaultPerPage+extra)
	for i := range models.DefaultPerPage + extra {
		specs = append(specs, models.Restaurant{
			Name:    fmt.Sprintf("Restaurant %02d", i),
			Cuisine: "Fusion",
		})
	}
	seedRestaurants(t, specs...)

	anon := newBrowser(t)

	firstPage, _ := anon.get("/restaurants")
	if n := cardCount(firstPage); n != models.DefaultPerPage {
		t.Errorf("first page shows %d restaurants, want %d", n, models.DefaultPerPage)
	}
	if !strings.Contains(firstPage, "page 1 of 2") {
		t.Error("first page does not report its position")
	}

	secondPage, _ := anon.get("/restaurants?page=2")
	if n := cardCount(secondPage); n != extra {
		t.Errorf("second page shows %d restaurants, want %d", n, extra)
	}

	// A page beyond the end shows the last page rather than an empty grid.
	beyond, _ := anon.get("/restaurants?page=99")
	if n := cardCount(beyond); n != extra {
		t.Errorf("page 99 shows %d restaurants, want the last page's %d", n, extra)
	}
}

// Paging through filtered results must keep the filters attached, or page two
// quietly shows everything.
func TestPaginationLinksKeepFilters(t *testing.T) {
	requireMongo(t)

	specs := make([]models.Restaurant, 0, models.DefaultPerPage+1)
	for i := range models.DefaultPerPage + 1 {
		specs = append(specs, models.Restaurant{
			Name:    fmt.Sprintf("Ramen %02d", i),
			Cuisine: "Japanese",
		})
	}
	specs = append(specs, models.Restaurant{Name: "Lone Trattoria", Cuisine: "Italian"})
	seedRestaurants(t, specs...)

	body, _ := newBrowser(t).get("/restaurants?cuisine=Japanese")
	if got := pageTwoLink(t, body).Query().Get("cuisine"); got != "Japanese" {
		t.Errorf("page 2 link carries cuisine=%q, want Japanese", got)
	}

	secondPage, _ := newBrowser(t).get("/restaurants?cuisine=Japanese&page=2")
	if strings.Contains(secondPage, "Lone Trattoria") {
		t.Error("page 2 of a filtered list included a restaurant from another cuisine")
	}
}

func TestIndexShowsEmptyStateWithoutFilters(t *testing.T) {
	requireMongo(t)

	body, _ := newBrowser(t).get("/restaurants")
	if !strings.Contains(body, "No restaurants yet") {
		t.Error("empty database did not produce the welcome empty state")
	}
	if strings.Contains(body, "No restaurants match those filters") {
		t.Error("unfiltered empty page showed the no-matches message")
	}
}

// The cuisine menu is built from the data, so it should list what exists and
// nothing else.
func TestIndexCuisineMenuReflectsData(t *testing.T) {
	requireMongo(t)

	seedRestaurants(t,
		models.Restaurant{Name: "One", Cuisine: "Peruvian"},
		models.Restaurant{Name: "Two", Cuisine: "Peruvian"},
		models.Restaurant{Name: "Three", Cuisine: "Ethiopian"},
	)

	body, _ := newBrowser(t).get("/restaurants")
	for _, cuisine := range []string{"Peruvian", "Ethiopian"} {
		if !strings.Contains(body, `<option value="`+cuisine+`"`) {
			t.Errorf("cuisine menu is missing %q", cuisine)
		}
	}
	if strings.Count(body, `<option value="Peruvian"`) != 1 {
		t.Error("cuisine menu lists Peruvian more than once")
	}
}
