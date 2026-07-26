package models

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"go.mongodb.org/mongo-driver/v2/bson"
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

func (q RestaurantQuery) filter() bson.M {
	filter := bson.M{}
	if q.Search != "" {
		// Substring matching, so a partial word like "trat" still finds
		// "Trattoria". QuoteMeta is essential: without it the user's input is
		// executed as a regular expression, which invites both errors and
		// pathological backtracking. The tradeoff is that this cannot use an
		// index and scans the collection; a text index would be faster but
		// would only match whole words.
		pattern := regexp.QuoteMeta(q.Search)
		filter["$or"] = []bson.M{
			{"name": bson.M{"$regex": pattern, "$options": "i"}},
			{"cuisine": bson.M{"$regex": pattern, "$options": "i"}},
			{"description": bson.M{"$regex": pattern, "$options": "i"}},
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
func FindRestaurants(ctx context.Context, q RestaurantQuery) (*RestaurantPage, error) {
	q.Normalize()
	filter := q.filter()

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

// DistinctCuisines lists the cuisines currently in use, sorted, for the filter
// menu. Cuisine is free text, so the menu reflects what users have entered
// rather than a fixed vocabulary.
func DistinctCuisines(ctx context.Context) ([]string, error) {
	var values []string
	if err := restaurants.Distinct(ctx, "cuisine", bson.M{}).Decode(&values); err != nil {
		return nil, err
	}
	values = slices.DeleteFunc(values, func(s string) bool { return strings.TrimSpace(s) == "" })
	slices.Sort(values)
	return values, nil
}
