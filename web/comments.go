package web

import (
	"net/http"
	"strconv"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/models"
)

func commentsNewForm(w http.ResponseWriter, r *http.Request) {
	restaurant, ok := loadRestaurant(w, r)
	if !ok {
		return
	}
	render(w, r, "comments/new", map[string]any{
		"Restaurant":    restaurant,
		"RatingChoices": models.RatingChoices(),
	})
}

func commentsCreate(w http.ResponseWriter, r *http.Request) {
	restaurant, ok := loadRestaurant(w, r)
	if !ok {
		return
	}
	user := CurrentUser(r)
	_, err := models.CreateComment(r.Context(),
		restaurant.ID,
		ratingValue(r.PostFormValue("rating")),
		r.PostFormValue("text"),
		models.Author{ID: user.ID, Username: user.Username},
	)
	if err != nil {
		flashFailure(w, r, err, "creating comment", "Something went wrong adding your review.")
		http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex()+"/comments/new", http.StatusFound)
		return
	}
	flash(w, r, "success", "Review added!")
	http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex(), http.StatusFound)
}

// ratingValue parses a submitted star rating. Anything unparseable becomes 0,
// which the model layer rejects with a message the user can act on.
func ratingValue(raw string) int {
	rating, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return rating
}

func commentsEditForm(w http.ResponseWriter, r *http.Request) {
	// The ownership middleware has already read this comment.
	comment, ok := commentFromContext(r)
	if !ok {
		flash(w, r, "error", "Review not found!")
		redirectBack(w, r)
		return
	}
	render(w, r, "comments/edit", map[string]any{
		"RestaurantID":  r.PathValue("id"),
		"Comment":       comment,
		"RatingChoices": models.RatingChoices(),
	})
}

func commentsUpdate(w http.ResponseWriter, r *http.Request) {
	commentID, err := bson.ObjectIDFromHex(r.PathValue("comment_id"))
	if err != nil {
		flash(w, r, "error", "Review not found!")
		redirectBack(w, r)
		return
	}
	rating := ratingValue(r.PostFormValue("rating"))
	if err := models.UpdateComment(r.Context(), commentID, rating, r.PostFormValue("text")); err != nil {
		flashFailure(w, r, err, "updating comment", "Something went wrong updating your review.")
		redirectBack(w, r)
		return
	}
	flash(w, r, "success", "Review updated!")
	http.Redirect(w, r, "/restaurants/"+r.PathValue("id"), http.StatusFound)
}

func commentsDelete(w http.ResponseWriter, r *http.Request) {
	commentID, err := bson.ObjectIDFromHex(r.PathValue("comment_id"))
	if err != nil {
		flash(w, r, "error", "Review not found!")
		redirectBack(w, r)
		return
	}
	if err := models.DeleteComment(r.Context(), commentID); err != nil {
		logger(r).Error("deleting comment", "comment_id", commentID.Hex(), "error", err)
		flash(w, r, "error", "Something went wrong deleting your review.")
		redirectBack(w, r)
		return
	}
	flash(w, r, "success", "Review deleted!")
	http.Redirect(w, r, "/restaurants/"+r.PathValue("id"), http.StatusFound)
}
