package web

import (
	"net/url"
	"testing"

	"github.com/ayushkokande/Connoisseur/models"
)

func normalized(query models.RestaurantQuery) models.RestaurantQuery {
	query.Normalize()
	return query
}

func TestPageNumber(t *testing.T) {
	cases := map[string]int{
		"":      1,
		"abc":   1,
		"0":     1,
		"-3":    1,
		"1":     1,
		"7":     7,
		"1e3":   1,
		"12.5":  1,
		" 4 ":   1,
		"99999": 99999,
	}

	for raw, want := range cases {
		t.Run(raw, func(t *testing.T) {
			if got := pageNumber(raw); got != want {
				t.Errorf("pageNumber(%q) = %d, want %d", raw, got, want)
			}
		})
	}
}

// Paging must not silently drop the filters the user applied.
func TestRestaurantsURLPreservesFilters(t *testing.T) {
	query := normalized(models.RestaurantQuery{
		Search:     "sushi bar",
		Cuisine:    "Japanese",
		PriceRange: "$$",
		Sort:       models.SortName,
	})

	parsed, err := url.Parse(restaurantsURL(query, 3))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/restaurants" {
		t.Errorf("path = %q, want /restaurants", parsed.Path)
	}

	want := url.Values{
		"q":       {"sushi bar"},
		"cuisine": {"Japanese"},
		"price":   {"$$"},
		"sort":    {models.SortName},
		"page":    {"3"},
	}
	got := parsed.Query()
	for key, values := range want {
		if got.Get(key) != values[0] {
			t.Errorf("query %s = %q, want %q", key, got.Get(key), values[0])
		}
	}
}

// Shareable URLs stay short: the default sort and the first page are implied.
func TestRestaurantsURLOmitsDefaults(t *testing.T) {
	query := normalized(models.RestaurantQuery{})

	if got := restaurantsURL(query, 1); got != "/restaurants" {
		t.Errorf("restaurantsURL(default, 1) = %q, want /restaurants", got)
	}

	parsed, err := url.Parse(restaurantsURL(query, 2))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Has("sort") {
		t.Error("default sort should not appear in the URL")
	}
	if got := parsed.Query().Get("page"); got != "2" {
		t.Errorf("page = %q, want 2", got)
	}
}

func TestPageLinksNilWhenSinglePage(t *testing.T) {
	query := normalized(models.RestaurantQuery{})
	results := &models.RestaurantPage{Page: 1, TotalPages: 1}

	if links := pageLinks(query, results); links != nil {
		t.Errorf("pageLinks() = %v, want nil for a single page", links)
	}
}

func TestPageLinksWindowsAroundCurrentPage(t *testing.T) {
	query := normalized(models.RestaurantQuery{})
	results := &models.RestaurantPage{Page: 10, TotalPages: 20}

	links := pageLinks(query, results)
	if len(links) != pageWindow {
		t.Fatalf("got %d links, want %d", len(links), pageWindow)
	}

	var current int
	for i, link := range links {
		if i > 0 && link.Number != links[i-1].Number+1 {
			t.Errorf("links are not consecutive: %d follows %d", link.Number, links[i-1].Number)
		}
		if link.Current {
			current++
			if link.Number != results.Page {
				t.Errorf("current link is page %d, want %d", link.Number, results.Page)
			}
		}
	}
	if current != 1 {
		t.Errorf("%d links marked current, want exactly 1", current)
	}
}

// Near either end the window shifts rather than shrinking, so the control keeps
// a stable width and never points past the last page.
func TestPageLinksClampToBounds(t *testing.T) {
	query := normalized(models.RestaurantQuery{})

	cases := map[string]struct {
		page       int
		totalPages int
		wantFirst  int
		wantLast   int
		wantCount  int
	}{
		"first page":              {1, 20, 1, pageWindow, pageWindow},
		"last page":               {20, 20, 20 - pageWindow + 1, 20, pageWindow},
		"fewer pages than window": {2, 3, 1, 3, 3},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			results := &models.RestaurantPage{Page: tc.page, TotalPages: tc.totalPages}
			links := pageLinks(query, results)

			if len(links) != tc.wantCount {
				t.Fatalf("got %d links, want %d", len(links), tc.wantCount)
			}
			if links[0].Number != tc.wantFirst {
				t.Errorf("first link = %d, want %d", links[0].Number, tc.wantFirst)
			}
			if last := links[len(links)-1].Number; last != tc.wantLast {
				t.Errorf("last link = %d, want %d", last, tc.wantLast)
			}
		})
	}
}

func TestAdjacentPageURL(t *testing.T) {
	query := normalized(models.RestaurantQuery{})

	t.Run("first page has no previous", func(t *testing.T) {
		results := &models.RestaurantPage{Page: 1, TotalPages: 3}
		if got := adjacentPageURL(query, results, -1); got != "" {
			t.Errorf("previous URL = %q, want empty", got)
		}
	})

	t.Run("last page has no next", func(t *testing.T) {
		results := &models.RestaurantPage{Page: 3, TotalPages: 3}
		if got := adjacentPageURL(query, results, 1); got != "" {
			t.Errorf("next URL = %q, want empty", got)
		}
	})

	t.Run("middle page links both ways", func(t *testing.T) {
		results := &models.RestaurantPage{Page: 2, TotalPages: 3}

		if got := adjacentPageURL(query, results, -1); got != "/restaurants" {
			t.Errorf("previous URL = %q, want /restaurants", got)
		}
		next, err := url.Parse(adjacentPageURL(query, results, 1))
		if err != nil {
			t.Fatal(err)
		}
		if got := next.Query().Get("page"); got != "3" {
			t.Errorf("next page = %q, want 3", got)
		}
	})
}
