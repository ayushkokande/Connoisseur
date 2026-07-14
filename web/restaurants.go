package web

import (
	"log"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shivamdubey91/connoisseur/models"
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

func restaurantsIndex(w http.ResponseWriter, r *http.Request) {
	restaurants, err := models.FindAllRestaurants(r.Context())
	if err != nil {
		log.Printf("listing restaurants: %v", err)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	render(w, r, "restaurants/index", map[string]any{"Restaurants": restaurants})
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
	id, ok := restaurantID(w, r)
	if !ok {
		return
	}
	restaurant, err := models.FindRestaurantByID(r.Context(), id)
	if err != nil {
		flash(w, r, "error", "Restaurant not found!")
		http.Redirect(w, r, "/restaurants", http.StatusFound)
		return
	}
	comments, err := models.FindCommentsByIDs(r.Context(), restaurant.Comments)
	if err != nil {
		log.Printf("loading comments: %v", err)
	}
	render(w, r, "restaurants/show", map[string]any{
		"Restaurant": restaurant,
		"Comments":   comments,
	})
}

func restaurantsEditForm(w http.ResponseWriter, r *http.Request) {
	id, ok := restaurantID(w, r)
	if !ok {
		return
	}
	restaurant, err := models.FindRestaurantByID(r.Context(), id)
	if err != nil {
		flash(w, r, "error", "Restaurant not found!")
		http.Redirect(w, r, "/restaurants", http.StatusFound)
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
	if err := models.DeleteComments(r.Context(), restaurant.Comments); err != nil {
		log.Printf("deleting restaurant comments: %v", err)
	}
	flash(w, r, "success", "Restaurant deleted!")
	http.Redirect(w, r, "/restaurants", http.StatusFound)
}
