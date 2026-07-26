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

// pageLinks returns the numbered page links to render, windowed around the
// current page. It returns nil when there is only one page.
func pageLinks(query models.RestaurantQuery, results *models.RestaurantPage) []pageLink {
	if results.TotalPages < 2 {
		return nil
	}

	start := results.Page - pageWindow/2
	if start < 1 {
		start = 1
	}
	end := start + pageWindow - 1
	if end > results.TotalPages {
		end = results.TotalPages
		start = max(end-pageWindow+1, 1)
	}

	links := make([]pageLink, 0, end-start+1)
	for page := start; page <= end; page++ {
		links = append(links, pageLink{
			Number:  page,
			URL:     restaurantsURL(query, page),
			Current: page == results.Page,
		})
	}
	return links
}

// adjacentPageURL returns the URL of the page offset away from the current one,
// or "" when there is no such page.
func adjacentPageURL(query models.RestaurantQuery, results *models.RestaurantPage, offset int) string {
	page := results.Page + offset
	if page < 1 || page > results.TotalPages {
		return ""
	}
	return restaurantsURL(query, page)
}
