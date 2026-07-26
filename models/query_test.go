package models

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestRestaurantQueryNormalize(t *testing.T) {
	defaults := RestaurantQuery{Sort: SortNewest, Page: 1, PerPage: DefaultPerPage}

	cases := []struct {
		name string
		in   RestaurantQuery
		want RestaurantQuery
	}{
		{"zero value gets defaults", RestaurantQuery{}, defaults},
		{"unknown sort falls back to newest", RestaurantQuery{Sort: "cheapest"}, defaults},
		{"unknown price range is dropped", RestaurantQuery{PriceRange: "free"}, defaults},
		{"page below one becomes the first page", RestaurantQuery{Page: -5}, defaults},
		{"per page above the cap falls back to the default", RestaurantQuery{PerPage: maxPerPage + 1}, defaults},
		{
			"valid price range is kept",
			RestaurantQuery{PriceRange: "$$"},
			RestaurantQuery{PriceRange: "$$", Sort: SortNewest, Page: 1, PerPage: DefaultPerPage},
		},
		{
			"text fields are trimmed",
			RestaurantQuery{Search: "  sushi  ", Cuisine: "\tJapanese\n"},
			RestaurantQuery{Search: "sushi", Cuisine: "Japanese", Sort: SortNewest, Page: 1, PerPage: DefaultPerPage},
		},
		{
			"explicit sort and page are preserved",
			RestaurantQuery{Sort: SortName, Page: 4},
			RestaurantQuery{Sort: SortName, Page: 4, PerPage: DefaultPerPage},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in
			got.Normalize()
			if got != tc.want {
				t.Errorf("Normalize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// Handlers normalize before building pagination links and FindRestaurants
// normalizes again, so a second pass must not change anything.
func TestRestaurantQueryNormalizeIsIdempotent(t *testing.T) {
	query := RestaurantQuery{Search: "  ramen ", Sort: "bogus", Page: 0, PerPage: 999}
	query.Normalize()
	once := query
	query.Normalize()

	if query != once {
		t.Errorf("second Normalize() changed the query: %+v then %+v", once, query)
	}
}

// The cap is counted in runes, so a multi-byte search term is not cut mid-character.
func TestRestaurantQueryTruncatesOverlongSearch(t *testing.T) {
	query := RestaurantQuery{Search: strings.Repeat("é", maxSearchLen+50)}
	query.Normalize()

	if n := utf8.RuneCountInString(query.Search); n != maxSearchLen {
		t.Errorf("search length = %d runes, want %d", n, maxSearchLen)
	}
	if !utf8.ValidString(query.Search) {
		t.Error("truncated search is not valid UTF-8")
	}
}

func TestRestaurantQueryIsFiltered(t *testing.T) {
	cases := map[string]struct {
		query RestaurantQuery
		want  bool
	}{
		"nothing set":     {RestaurantQuery{}, false},
		"sort only":       {RestaurantQuery{Sort: SortName}, false},
		"page only":       {RestaurantQuery{Page: 3}, false},
		"search set":      {RestaurantQuery{Search: "ramen"}, true},
		"cuisine set":     {RestaurantQuery{Cuisine: "Thai"}, true},
		"price range set": {RestaurantQuery{PriceRange: "$$"}, true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			query := tc.query
			query.Normalize()
			if got := query.IsFiltered(); got != tc.want {
				t.Errorf("IsFiltered() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The search term is interpolated into a MongoDB $regex. Without escaping, the
// user supplies the pattern, which invites both syntax errors and pathological
// backtracking.
func TestSearchFilterEscapesRegexMetacharacters(t *testing.T) {
	const search = `a.*b(c[d`
	query := RestaurantQuery{Search: search}
	query.Normalize()

	clauses, ok := query.filter()["$or"].([]bson.M)
	if !ok {
		t.Fatalf("search filter has no $or clause: %#v", query.filter())
	}
	if len(clauses) == 0 {
		t.Fatal("search filter produced no $or clauses")
	}

	want := regexp.QuoteMeta(search)
	if _, err := regexp.Compile(want); err != nil {
		t.Fatalf("escaped pattern does not compile: %v", err)
	}

	for _, clause := range clauses {
		for field, raw := range clause {
			condition, ok := raw.(bson.M)
			if !ok {
				t.Errorf("%s condition is %T, want bson.M", field, raw)
				continue
			}
			if got := condition["$regex"]; got != want {
				t.Errorf("%s $regex = %q, want %q", field, got, want)
			}
		}
	}
}

func TestFilterOmitsUnsetFields(t *testing.T) {
	query := RestaurantQuery{}
	query.Normalize()

	if filter := query.filter(); len(filter) != 0 {
		t.Errorf("empty query produced filter %#v, want no conditions", filter)
	}
}

func TestFilterMatchesCuisineAndPriceExactly(t *testing.T) {
	query := RestaurantQuery{Cuisine: "Japanese", PriceRange: "$$"}
	query.Normalize()
	filter := query.filter()

	if got := filter["cuisine"]; got != "Japanese" {
		t.Errorf(`filter["cuisine"] = %v, want "Japanese"`, got)
	}
	if got := filter["priceRange"]; got != "$$" {
		t.Errorf(`filter["priceRange"] = %v, want "$$"`, got)
	}
	if _, ok := filter["$or"]; ok {
		t.Error("filter has a search clause but no search term was set")
	}
}

func TestSortOrder(t *testing.T) {
	newestFirst := bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}

	cases := map[string]struct {
		sort string
		want bson.D
	}{
		"newest":         {SortNewest, newestFirst},
		"unknown":        {"bogus", newestFirst},
		"empty defaults": {"", newestFirst},
		"oldest":         {SortOldest, bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}},
		"name":           {SortName, bson.D{{Key: "name", Value: 1}, {Key: "_id", Value: 1}}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			query := RestaurantQuery{Sort: tc.sort}
			query.Normalize()

			if got := query.sortOrder(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("sortOrder() = %v, want %v", got, tc.want)
			}
		})
	}
}

// Every sort must be a total order. Timestamps only have millisecond resolution
// and names repeat, so ties are ordinary; an unstable order across a skip/limit
// boundary would show a restaurant on two pages or on neither.
func TestSortOrderBreaksTiesDeterministically(t *testing.T) {
	for _, sort := range []string{SortNewest, SortOldest, SortName} {
		t.Run(sort, func(t *testing.T) {
			query := RestaurantQuery{Sort: sort}
			query.Normalize()

			order := query.sortOrder()
			last := order[len(order)-1]
			if last.Key != "_id" {
				t.Errorf("sort %q ends with %q, want a trailing _id tiebreaker", sort, last.Key)
			}
		})
	}
}

func TestPriceRangesIsACopy(t *testing.T) {
	ranges := PriceRanges()
	if len(ranges) == 0 {
		t.Fatal("PriceRanges() is empty")
	}
	ranges[0] = "mutated"

	if PriceRanges()[0] == "mutated" {
		t.Error("PriceRanges() exposes the package's allowlist to mutation")
	}
}
