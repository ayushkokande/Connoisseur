package models

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Sort orders offered on the restaurant index.
const (
	SortNewest = "newest"
	SortOldest = "oldest"
	SortName   = "name"
	SortRating = "rating"
)

const (
	// DefaultPerPage fills a three-column grid four rows deep.
	DefaultPerPage = 12
	maxPerPage     = 48
	maxSearchLen   = 100
)

// RestaurantQuery describes a filtered, sorted, paginated view of the
// restaurant collection. The zero value is a valid request for the first page
// of everything, newest first.
type RestaurantQuery struct {
	Search     string
	Cuisine    string
	PriceRange string
	// MinRating keeps only restaurants averaging at least this many stars.
	// Zero means no rating filter.
	MinRating int
	Sort      string
	Page      int
	PerPage   int
}

// Normalize clamps user-supplied values to something the query layer can
// safely act on. It is idempotent, so callers can normalize before building
// links and rely on FindRestaurants normalizing again.
func (q *RestaurantQuery) Normalize() {
	q.Search = strings.TrimSpace(q.Search)
	if utf8.RuneCountInString(q.Search) > maxSearchLen {
		q.Search = string([]rune(q.Search)[:maxSearchLen])
	}
	q.Cuisine = strings.TrimSpace(q.Cuisine)

	// An unknown price range is dropped rather than rejected: a stale bookmark
	// should still show results.
	if !isValidPriceRange(q.PriceRange) {
		q.PriceRange = ""
	}
	if q.MinRating < minRating || q.MinRating > maxRating {
		q.MinRating = 0
	}
	if !slices.Contains([]string{SortNewest, SortOldest, SortName, SortRating}, q.Sort) {
		q.Sort = SortNewest
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PerPage < 1 || q.PerPage > maxPerPage {
		q.PerPage = DefaultPerPage
	}
}

// IsFiltered reports whether anything narrows the result set, which lets the
// UI tell "nothing matched your search" apart from "nothing here yet".
func (q RestaurantQuery) IsFiltered() bool {
	return q.Search != "" || q.Cuisine != "" || q.PriceRange != "" || q.MinRating > 0
}

// searchMode selects how a search term is matched.
type searchMode int

const (
	// searchWords matches whole words through the text index.
	searchWords searchMode = iota
	// searchSubstring matches anywhere within a field by scanning.
	searchSubstring
)

func (q RestaurantQuery) filter(mode searchMode) bson.M {
	filter := bson.M{}
	if q.Search != "" {
		if mode == searchWords {
			// Served by the text index rather than by scanning the collection.
			// The term is passed through as typed, so the conventions people
			// already expect from a search box work: a quoted run matches as a
			// phrase, and a leading hyphen on a word excludes it. A hyphen
			// inside a word is not exclusion — the tokenizer splits on it — so
			// "gluten-free" searches for both words rather than against one.
			filter["$text"] = bson.M{"$search": q.Search}
		} else {
			// Substring matching, so a partial word like "trat" still finds
			// "Trattoria". QuoteMeta is essential: without it the user's input
			// is executed as a regular expression, which invites both errors and
			// pathological backtracking. This cannot use an index and scans the
			// collection, which is why it is the fallback rather than the
			// first attempt.
			pattern := regexp.QuoteMeta(q.Search)
			filter["$or"] = []bson.M{
				{"name": bson.M{"$regex": pattern, "$options": "i"}},
				{"cuisine": bson.M{"$regex": pattern, "$options": "i"}},
				{"description": bson.M{"$regex": pattern, "$options": "i"}},
			}
		}
	}
	if q.Cuisine != "" {
		filter["cuisine"] = q.Cuisine
	}
	if q.PriceRange != "" {
		filter["priceRange"] = q.PriceRange
	}
	if q.MinRating > 0 {
		filter["avgRating"] = bson.M{"$gte": q.MinRating}
	}
	return filter
}

// sortOrder always ends with _id. Timestamps are stored to the millisecond and
// names are not unique, so without a tiebreaker documents that compare equal
// have no defined order — and an undefined order across a skip/limit boundary
// lets a restaurant appear on two pages or on none.
func (q RestaurantQuery) sortOrder() bson.D {
	switch q.Sort {
	case SortOldest:
		return bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}
	case SortName:
		return bson.D{{Key: "name", Value: 1}, {Key: "_id", Value: 1}}
	case SortRating:
		// Review count breaks ties so that a single five-star review does not
		// outrank a restaurant with fifty of them.
		return bson.D{
			{Key: "avgRating", Value: -1},
			{Key: "reviewCount", Value: -1},
			{Key: "_id", Value: -1},
		}
	default:
		return bson.D{{Key: "createdAt", Value: -1}, {Key: "_id", Value: -1}}
	}
}

// RestaurantPage is one page of results plus the totals needed to render
// pagination controls.
type RestaurantPage struct {
	Restaurants []Restaurant
	Total       int64
	Page        int
	PerPage     int
	TotalPages  int
}

// HasPrev reports whether a previous page exists.
func (p RestaurantPage) HasPrev() bool { return p.Page > 1 }

// HasNext reports whether a further page exists.
func (p RestaurantPage) HasNext() bool { return p.Page < p.TotalPages }

// FindRestaurants returns the page of restaurants matching q.
//
// A search is tried against the text index first and falls back to scanning
// when that finds nothing, or when the index is not there to use. The index
// matches whole words, so it answers most searches without reading the
// collection, while a partial word like "trat" — which it cannot match — still
// finds "Trattoria" through the scan. The fallback costs one indexed count on a
// search that matches nothing, and saves a collection scan on every search that
// does.
func FindRestaurants(ctx context.Context, q RestaurantQuery) (*RestaurantPage, error) {
	q.Normalize()

	mode := searchSubstring
	if q.Search != "" {
		mode = searchWords
	}

	page, err := q.findPage(ctx, mode)
	if mode == searchWords {
		// $text refuses to run at all without its index, where the scan needs
		// nothing. Falling back on that keeps the index an optimization rather
		// than a thing search breaks without.
		var serverErr mongo.ServerError
		if errors.As(err, &serverErr) && serverErr.HasErrorCode(indexNotFoundCode) {
			return q.findPage(ctx, searchSubstring)
		}
		if err == nil && page.Total == 0 {
			return q.findPage(ctx, searchSubstring)
		}
	}
	if err != nil {
		return nil, err
	}
	return page, nil
}

// findPage runs the query with one search strategy. q is expected to be
// normalized already.
func (q RestaurantQuery) findPage(ctx context.Context, mode searchMode) (*RestaurantPage, error) {
	filter := q.filter(mode)

	total, err := restaurants.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	totalPages := int((total + int64(q.PerPage) - 1) / int64(q.PerPage))
	if totalPages < 1 {
		totalPages = 1
	}
	// A page number past the end — a bookmarked link, or a filter that has
	// since narrowed the results — shows the last page instead of a blank grid.
	if q.Page > totalPages {
		q.Page = totalPages
	}

	opts := options.Find().
		SetSort(q.sortOrder()).
		SetSkip(int64((q.Page - 1) * q.PerPage)).
		SetLimit(int64(q.PerPage))

	cursor, err := restaurants.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	var results []Restaurant
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return &RestaurantPage{
		Restaurants: results,
		Total:       total,
		Page:        q.Page,
		PerPage:     q.PerPage,
		TotalPages:  totalPages,
	}, nil
}

// cuisineCacheTTL bounds how long the filter menu can lag the data. Writes
// invalidate the cache outright, so this only matters when another process made
// the change.
const cuisineCacheTTL = time.Minute

var cuisineCache struct {
	mu      sync.RWMutex
	values  []string
	expires time.Time
}

// invalidateCuisines drops the cached menu, so a restaurant added, retitled or
// removed shows up in the filter immediately rather than at the next expiry.
func invalidateCuisines() {
	cuisineCache.mu.Lock()
	cuisineCache.expires = time.Time{}
	cuisineCache.mu.Unlock()
}

// DistinctCuisines lists the cuisines currently in use, sorted, for the filter
// menu. Cuisine is free text, so the menu reflects what users have entered
// rather than a fixed vocabulary.
//
// The result is cached because building it is a collection-wide distinct and
// the restaurant index rendered one on every request. The rebuild deliberately
// runs without holding the lock: a burst arriving exactly at expiry may issue a
// few queries between them, which is still bounded by concurrency rather than
// by traffic, and blocking every reader on one database round trip to avoid
// that is the worse trade.
func DistinctCuisines(ctx context.Context) ([]string, error) {
	cuisineCache.mu.RLock()
	cached, fresh := cuisineCache.values, time.Now().Before(cuisineCache.expires)
	cuisineCache.mu.RUnlock()

	if fresh {
		// Copied, so a caller sorting or filtering the menu cannot reach into
		// the cache and change what everyone else sees.
		return slices.Clone(cached), nil
	}

	var values []string
	if err := restaurants.Distinct(ctx, "cuisine", bson.M{}).Decode(&values); err != nil {
		return nil, err
	}
	values = slices.DeleteFunc(values, func(s string) bool { return strings.TrimSpace(s) == "" })
	slices.Sort(values)

	cuisineCache.mu.Lock()
	cuisineCache.values = values
	cuisineCache.expires = time.Now().Add(cuisineCacheTTL)
	cuisineCache.mu.Unlock()

	return slices.Clone(values), nil
}
