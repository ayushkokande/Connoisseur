package web

import (
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/models"
)

// restaurantID parses the {id} path segment; on failure it flashes and redirects.
func restaurantID(w http.ResponseWriter, r *http.Request) (bson.ObjectID, bool) {
	id, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err != nil {
		flash(w, r, "error", "Restaurant not found!")
		http.Redirect(w, r, "/restaurants", http.StatusFound)
		return bson.ObjectID{}, false
	}
	return id, true
}

// loadRestaurant returns the restaurant named by the {id} path segment, reusing
// the one the ownership middleware already read when there is one. On failure it
// flashes, redirects and reports false.
func loadRestaurant(w http.ResponseWriter, r *http.Request) (*models.Restaurant, bool) {
	if restaurant, ok := restaurantFromContext(r); ok {
		return restaurant, true
	}
	id, ok := restaurantID(w, r)
	if !ok {
		return nil, false
	}
	restaurant, err := models.FindRestaurantByID(r.Context(), id)
	if err != nil {
		flash(w, r, "error", "Restaurant not found!")
		http.Redirect(w, r, "/restaurants", http.StatusFound)
		return nil, false
	}
	return restaurant, true
}

func restaurantsIndex(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	query := models.RestaurantQuery{
		Search:     params.Get("q"),
		Cuisine:    params.Get("cuisine"),
		PriceRange: params.Get("price"),
		MinRating:  ratingValue(params.Get("rating")),
		Sort:       params.Get("sort"),
		Page:       pageNumber(params.Get("page")),
	}
	query.Normalize()

	results, err := models.FindRestaurants(r.Context(), query)
	if err != nil {
		logger(r).Error("listing restaurants", "error", err)
		flash(w, r, "error", "Something went wrong loading restaurants.")
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// An empty cuisine menu is a worse page, not a broken one.
	cuisines, err := models.DistinctCuisines(r.Context())
	if err != nil {
		logger(r).Error("listing cuisines", "error", err)
	}

	render(w, r, "restaurants/index", map[string]any{
		"Results":       results,
		"Query":         query,
		"Cuisines":      cuisines,
		"PriceRanges":   models.PriceRanges(),
		"RatingChoices": models.RatingChoices(),
		"SortOptions":   sortOptions,
		"DefaultSort":   models.SortNewest,
		"Pages":         pageLinks(query, results),
		"PrevURL":       adjacentPageURL(query, results, -1),
		"NextURL":       adjacentPageURL(query, results, 1),
	})
}

func restaurantsNewForm(w http.ResponseWriter, r *http.Request) {
	render(w, r, "restaurants/new", nil)
}

func restaurantsCreate(w http.ResponseWriter, r *http.Request) {
	user := CurrentUser(r)
	restaurant := &models.Restaurant{
		Name:        r.PostFormValue("name"),
		Image:       r.PostFormValue("image"),
		Cuisine:     r.PostFormValue("cuisine"),
		PriceRange:  r.PostFormValue("priceRange"),
		Description: r.PostFormValue("description"),
		Author: models.Author{
			ID:       user.ID,
			Username: user.Username,
		},
	}
	if err := models.CreateRestaurant(r.Context(), restaurant); err != nil {
		flashFailure(w, r, err, "creating restaurant", "Something went wrong creating the restaurant.")
		http.Redirect(w, r, "/restaurants/new", http.StatusFound)
		return
	}
	flash(w, r, "success", "Restaurant added successfully!")
	http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex(), http.StatusFound)
}

func restaurantsShow(w http.ResponseWriter, r *http.Request) {
	restaurant, ok := loadRestaurant(w, r)
	if !ok {
		return
	}
	id := restaurant.ID.Hex()
	reviews, err := models.FindCommentsPage(r.Context(), restaurant.ID,
		pageNumber(r.URL.Query().Get("page")), models.DefaultReviewsPerPage)
	if err != nil {
		logger(r).Error("loading comments", "restaurant_id", id, "error", err)
		reviews = &models.CommentPage{Page: 1, TotalPages: 1}
	}

	render(w, r, "restaurants/show", map[string]any{
		"Restaurant": restaurant,
		"Reviews":    reviews,
		// Looked up rather than picked out of the page above, because the
		// visitor's own review is not necessarily on the page being shown.
		"UserReview": existingReview(r, restaurant.ID),
		"Pages":      reviewPageLinks(id, reviews),
		"PrevURL":    adjacentReviewURL(id, reviews, -1),
		"NextURL":    adjacentReviewURL(id, reviews, 1),
	})
}

func restaurantsEditForm(w http.ResponseWriter, r *http.Request) {
	restaurant, ok := loadRestaurant(w, r)
	if !ok {
		return
	}
	render(w, r, "restaurants/edit", map[string]any{"Restaurant": restaurant})
}

func restaurantsUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := restaurantID(w, r)
	if !ok {
		return
	}
	updated := &models.Restaurant{
		Name:        r.PostFormValue("name"),
		Image:       r.PostFormValue("image"),
		Cuisine:     r.PostFormValue("cuisine"),
		PriceRange:  r.PostFormValue("priceRange"),
		Description: r.PostFormValue("description"),
	}
	if err := models.UpdateRestaurant(r.Context(), id, updated); err != nil {
		flashFailure(w, r, err, "updating restaurant", "Something went wrong updating the restaurant.")
		http.Redirect(w, r, "/restaurants/"+id.Hex()+"/edit", http.StatusFound)
		return
	}
	flash(w, r, "success", "Restaurant updated!")
	http.Redirect(w, r, "/restaurants/"+id.Hex(), http.StatusFound)
}

func restaurantsDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := restaurantID(w, r)
	if !ok {
		return
	}
	restaurant, err := models.DeleteRestaurant(r.Context(), id)
	if err != nil {
		flash(w, r, "error", "Something went wrong deleting the restaurant.")
		http.Redirect(w, r, "/restaurants", http.StatusFound)
		return
	}
	if err := models.DeleteCommentsByRestaurant(r.Context(), restaurant.ID); err != nil {
		logger(r).Error("deleting restaurant comments",
			"restaurant_id", restaurant.ID.Hex(),
			"error", err,
		)
	}
	flash(w, r, "success", "Restaurant deleted!")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}
