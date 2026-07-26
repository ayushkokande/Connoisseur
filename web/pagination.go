package web

import (
	"net/url"
	"strconv"

	"github.com/ayushkokande/Connoisseur/models"
)

// pageWindow caps how many numbered page links are rendered at once, so a large
// collection does not produce an unusable strip of hundreds of links.
const pageWindow = 7

// sortOptions drives the sort menu. Values must match the models.Sort constants.
var sortOptions = []struct {
	Value string
	Label string
}{
	{models.SortNewest, "Newest first"},
	{models.SortRating, "Top rated"},
	{models.SortOldest, "Oldest first"},
	{models.SortName, "Name (A–Z)"},
}

// pageLink is one entry in the pagination control.
type pageLink struct {
	Number  int
	URL     string
	Current bool
}

// pageNumber parses a page query parameter, treating anything unparseable as
// the first page.
func pageNumber(raw string) int {
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

// restaurantsURL builds an index URL for the given page, carrying the active
// filters across so paging does not silently reset them. Defaults are omitted
// to keep shareable URLs short.
func restaurantsURL(query models.RestaurantQuery, page int) string {
	values := url.Values{}
	if query.Search != "" {
		values.Set("q", query.Search)
	}
	if query.Cuisine != "" {
		values.Set("cuisine", query.Cuisine)
	}
	if query.PriceRange != "" {
		values.Set("price", query.PriceRange)
	}
	if query.MinRating > 0 {
		values.Set("rating", strconv.Itoa(query.MinRating))
	}
	if query.Sort != "" && query.Sort != models.SortNewest {
		values.Set("sort", query.Sort)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	if len(values) == 0 {
		return "/restaurants"
	}
	return "/restaurants?" + values.Encode()
}

// windowedPageLinks returns the numbered page links to render, windowed around
// the current one. It returns nil when there is only one page. urlFor builds the
// link for a page number, which is all that differs between the paginated
// collections.
func windowedPageLinks(current, totalPages int, urlFor func(int) string) []pageLink {
	if totalPages < 2 {
		return nil
	}

	start := current - pageWindow/2
	if start < 1 {
		start = 1
	}
	end := start + pageWindow - 1
	if end > totalPages {
		end = totalPages
		start = max(end-pageWindow+1, 1)
	}

	links := make([]pageLink, 0, end-start+1)
	for page := start; page <= end; page++ {
		links = append(links, pageLink{
			Number:  page,
			URL:     urlFor(page),
			Current: page == current,
		})
	}
	return links
}

// adjacentURL returns the URL of the page offset away from the current one, or
// "" when there is no such page.
func adjacentURL(current, totalPages, offset int, urlFor func(int) string) string {
	page := current + offset
	if page < 1 || page > totalPages {
		return ""
	}
	return urlFor(page)
}

// pageLinks returns the numbered links for the restaurant index.
func pageLinks(query models.RestaurantQuery, results *models.RestaurantPage) []pageLink {
	return windowedPageLinks(results.Page, results.TotalPages, func(page int) string {
		return restaurantsURL(query, page)
	})
}

// adjacentPageURL returns the restaurant index page offset away from the
// current one, or "" when there is no such page.
func adjacentPageURL(query models.RestaurantQuery, results *models.RestaurantPage, offset int) string {
	return adjacentURL(results.Page, results.TotalPages, offset, func(page int) string {
		return restaurantsURL(query, page)
	})
}

// reviewsURL builds a restaurant page URL showing the given page of reviews.
// The fragment lands the visitor on the reviews rather than back at the top of
// the restaurant, which is what they were reading.
func reviewsURL(restaurantID string, page int) string {
	base := "/restaurants/" + restaurantID
	if page <= 1 {
		return base + "#reviews"
	}
	return base + "?page=" + strconv.Itoa(page) + "#reviews"
}

// reviewPageLinks returns the numbered links for one restaurant's reviews.
func reviewPageLinks(restaurantID string, reviews *models.CommentPage) []pageLink {
	return windowedPageLinks(reviews.Page, reviews.TotalPages, func(page int) string {
		return reviewsURL(restaurantID, page)
	})
}

// adjacentReviewURL returns the review page offset away from the current one,
// or "" when there is no such page.
func adjacentReviewURL(restaurantID string, reviews *models.CommentPage, offset int) string {
	return adjacentURL(reviews.Page, reviews.TotalPages, offset, func(page int) string {
		return reviewsURL(restaurantID, page)
	})
}
