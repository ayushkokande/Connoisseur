package models

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// seedSearchable inserts restaurants with distinctive text to search for.
func seedSearchable(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	for _, r := range []Restaurant{
		{Name: "Trattoria Roma", Cuisine: "Italian", Description: "Handmade tagliatelle and a gluten-free menu."},
		{Name: "Sushi Palace", Cuisine: "Japanese", Description: "Omakase counter by the window."},
		{Name: "Corner Diner", Cuisine: "American", Description: "Late night sushi, oddly."},
	} {
		restaurant := r
		restaurant.Image = "https://example.com/photo.jpg"
		restaurant.PriceRange = "$$"
		restaurant.Author = Author{ID: bson.NewObjectID(), Username: "searcher"}
		if err := CreateRestaurant(ctx, &restaurant); err != nil {
			t.Fatalf("creating %q: %v", r.Name, err)
		}
	}
}

// found returns the names a search matches.
func found(t *testing.T, search string) []string {
	t.Helper()
	page, err := FindRestaurants(context.Background(), RestaurantQuery{Search: search})
	if err != nil {
		t.Fatalf("searching %q: %v", search, err)
	}
	names := make([]string, 0, len(page.Restaurants))
	for _, r := range page.Restaurants {
		names = append(names, r.Name)
	}
	slices.Sort(names)
	return names
}

// explainFilter returns MongoDB's query plan for a filter, so a test can assert
// how the search is executed rather than only what it returns.
func explainFilter(t *testing.T, filter bson.M) string {
	t.Helper()
	var plan bson.M
	err := database.RunCommand(context.Background(), bson.D{
		{Key: "explain", Value: bson.D{
			{Key: "find", Value: "restaurants"},
			{Key: "filter", Value: filter},
		}},
		{Key: "verbosity", Value: "queryPlanner"},
	}).Decode(&plan)
	if err != nil {
		t.Fatalf("explaining the query: %v", err)
	}
	return fmt.Sprintf("%v", plan)
}

// The point of the change: an ordinary search is answered from the text index
// instead of reading every restaurant.
func TestWordSearchUsesTheTextIndex(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	query := RestaurantQuery{Search: "tagliatelle"}
	query.Normalize()

	plan := explainFilter(t, query.filter(searchWords))
	if !strings.Contains(plan, "TEXT") {
		t.Errorf("a word search does not use the text index:\n%s", plan)
	}
	if strings.Contains(plan, "COLLSCAN") {
		t.Errorf("a word search still scans the collection:\n%s", plan)
	}
}

// The fallback is a scan by design; this records that, so the cost of taking it
// is visible rather than assumed.
func TestSubstringSearchScansTheCollection(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	query := RestaurantQuery{Search: "taglia"}
	query.Normalize()

	plan := explainFilter(t, query.filter(searchSubstring))
	if !strings.Contains(plan, "COLLSCAN") {
		t.Errorf("the substring fallback was expected to scan:\n%s", plan)
	}
}

// $text refuses to run at all without its index, where the scan needs nothing.
// Search must keep working on a database where the index has not been built —
// an older deployment, or a collection restored without its indexes — rather
// than returning an error to everyone searching.
func TestSearchStillWorksWithoutTheTextIndex(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)
	ctx := context.Background()

	if err := dropIndex(ctx, restaurants, searchIndexName); err != nil {
		t.Fatalf("dropping the text index: %v", err)
	}
	t.Cleanup(func() {
		if err := Init(testDB); err != nil {
			t.Fatalf("restoring indexes: %v", err)
		}
	})

	if got := found(t, "tagliatelle"); !slices.Equal(got, []string{"Trattoria Roma"}) {
		t.Errorf("searching without the text index returned %v, want Trattoria Roma", got)
	}
}

func TestSearchFindsWholeWords(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	cases := map[string][]string{
		"tagliatelle": {"Trattoria Roma"},
		"Italian":     {"Trattoria Roma"},
		// Case and stemming are the text index's own doing.
		"PALACE": {"Sushi Palace"},
		// Present in one name and another's description.
		"sushi": {"Corner Diner", "Sushi Palace"},
	}

	for search, want := range cases {
		t.Run(search, func(t *testing.T) {
			if got := found(t, search); !slices.Equal(got, want) {
				t.Errorf("searching %q returned %v, want %v", search, got, want)
			}
		})
	}
}

// The text index cannot match a partial word, so the scan has to pick those up
// or searching as you type stops working.
func TestPartialWordsFallBackToScanning(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	cases := map[string][]string{
		"Trat":   {"Trattoria Roma"},
		"taglia": {"Trattoria Roma"},
		"Sush":   {"Corner Diner", "Sushi Palace"},
	}

	for search, want := range cases {
		t.Run(search, func(t *testing.T) {
			if got := found(t, search); !slices.Equal(got, want) {
				t.Errorf("searching %q returned %v, want %v", search, got, want)
			}
		})
	}
}

// The search term reaches the text index as typed, so the conventions people
// expect from a search box work. These are the two that change what a query
// means, and they are pinned here because passing the term through unaltered is
// a deliberate choice rather than an oversight.
func TestSearchSupportsPhrasesAndExclusion(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	// Only one description contains the words in this order.
	if got := found(t, `"night sushi"`); !slices.Equal(got, []string{"Corner Diner"}) {
		t.Errorf(`phrase search returned %v, want only Corner Diner`, got)
	}
	// Sushi Palace has "counter" in its description, so excluding it leaves the
	// other restaurant mentioning sushi.
	if got := found(t, "sushi -counter"); !slices.Equal(got, []string{"Corner Diner"}) {
		t.Errorf("excluding a word returned %v, want only Corner Diner", got)
	}
}

// A hyphen inside a word is not exclusion: the tokenizer splits on it, so a
// hyphenated term searches for its parts rather than against one of them.
func TestHyphenInsideAWordIsNotExclusion(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	if got := found(t, "gluten-free"); !slices.Equal(got, []string{"Trattoria Roma"}) {
		t.Errorf("searching %q returned %v, want the gluten-free restaurant", "gluten-free", got)
	}
}

// A term the index cannot match anything with must not come back as a match for
// everything. It goes to the substring scan and is matched literally.
func TestPunctuationOnlySearchIsMatchedLiterally(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	cases := map[string][]string{
		// The one restaurant whose text contains a hyphen: "gluten-free".
		"-": {"Trattoria Roma"},
		// Nothing here contains a quote, or two hyphens together.
		`"`:  {},
		"--": {},
	}

	for search, want := range cases {
		t.Run(search, func(t *testing.T) {
			if got := found(t, search); !slices.Equal(got, want) {
				t.Errorf("searching %q returned %v, want %v", search, got, want)
			}
		})
	}
}

// Search has to combine with the other filters rather than replace them.
func TestSearchCombinesWithTheOtherFilters(t *testing.T) {
	requireMongo(t)
	seedSearchable(t)

	page, err := FindRestaurants(context.Background(), RestaurantQuery{
		Search:  "sushi",
		Cuisine: "Japanese",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Restaurants) != 1 || page.Restaurants[0].Name != "Sushi Palace" {
		t.Errorf("searching sushi within Japanese returned %d results, want only Sushi Palace", len(page.Restaurants))
	}

	// The same term with a cuisine that excludes every match returns nothing.
	page, err = FindRestaurants(context.Background(), RestaurantQuery{
		Search:  "sushi",
		Cuisine: "Italian",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Restaurants) != 0 {
		t.Errorf("the cuisine filter was ignored: got %d results", len(page.Restaurants))
	}
}
