package models

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

// cuisines returns the filter menu, failing the test if it cannot be built.
func cuisines(t *testing.T) []string {
	t.Helper()
	values, err := DistinctCuisines(context.Background())
	if err != nil {
		t.Fatalf("listing cuisines: %v", err)
	}
	return values
}

// A restaurant added through the application has to appear in the filter menu
// at once. Waiting out the cache would show a menu that does not list the
// cuisine the visitor just created.
func TestCuisineMenuReflectsWritesImmediately(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	if got := cuisines(t); len(got) != 0 {
		t.Fatalf("started with %v, want an empty menu", got)
	}

	restaurant := newRestaurant(t, "Cached Bistro") // cuisine "Testing"
	if got := cuisines(t); !slices.Contains(got, "Testing") {
		t.Errorf("menu is %v, want it to include the cuisine just added", got)
	}

	// Retitling a restaurant's cuisine has to move the menu too.
	updated := *restaurant
	updated.Cuisine = "Renamed"
	if err := UpdateRestaurant(ctx, restaurant.ID, &updated); err != nil {
		t.Fatal(err)
	}
	got := cuisines(t)
	if slices.Contains(got, "Testing") {
		t.Errorf("menu is %v, want the old cuisine gone", got)
	}
	if !slices.Contains(got, "Renamed") {
		t.Errorf("menu is %v, want the new cuisine listed", got)
	}

	// And deleting the last restaurant of a cuisine has to empty it.
	if _, err := DeleteRestaurant(ctx, restaurant.ID); err != nil {
		t.Fatal(err)
	}
	if got := cuisines(t); len(got) != 0 {
		t.Errorf("menu is %v after the last restaurant was deleted, want empty", got)
	}
}

// A cached value handed straight to callers could be sorted or truncated by one
// of them and change what every other request sees.
func TestCuisineMenuCallersCannotMutateTheCache(t *testing.T) {
	requireMongo(t)

	newRestaurant(t, "First Bistro")

	first := cuisines(t)
	if len(first) == 0 {
		t.Fatal("no cuisines to work with")
	}
	first[0] = "Vandalised"

	if second := cuisines(t); slices.Contains(second, "Vandalised") {
		t.Errorf("menu is %v; a caller's change reached the cache", second)
	}
}

// The cache is read by every request rendering the index, and written by every
// restaurant write. Racing them must not corrupt it.
func TestCuisineMenuIsSafeUnderConcurrentUse(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	newRestaurant(t, "Concurrent Bistro")

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				if _, err := DistinctCuisines(ctx); err != nil {
					t.Errorf("listing cuisines: %v", err)
					return
				}
			}
		}()
	}
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 25 {
				invalidateCuisines()
			}
		}()
	}
	wg.Wait()
}

// Serving a cached menu forever would leave one process showing another's stale
// data, so the entry has to expire on its own as well as on a write.
func TestCuisineMenuExpires(t *testing.T) {
	requireMongo(t)

	newRestaurant(t, "Expiring Bistro")
	if got := cuisines(t); len(got) == 0 {
		t.Fatal("nothing cached to expire")
	}

	// Stand in for another process having written: change the data underneath
	// the cache, then age the entry out rather than sleeping for the TTL.
	if _, err := restaurants.DeleteMany(context.Background(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if got := cuisines(t); len(got) == 0 {
		t.Error("the cache stopped serving before it expired, so it is not caching")
	}

	cuisineCache.mu.Lock()
	cuisineCache.expires = time.Now().Add(-time.Second)
	cuisineCache.mu.Unlock()

	if got := cuisines(t); len(got) != 0 {
		t.Errorf("menu is %v after expiry, want it rebuilt from the emptied collection", got)
	}
}
