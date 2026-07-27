package web

import (
	"errors"
	"net/http"
	"strconv"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

func commentsNewForm(w http.ResponseWriter, r *http.Request) {
	restaurant, ok := loadRestaurant(w, r)
	if !ok {
		return
	}
	// Someone arriving here with a review already written wants to change it,
	// however they got here, so send them to it rather than to a form that
	// cannot be submitted.
	if existing := existingReview(r, restaurant.ID); existing != nil {
		redirectToReview(w, r, restaurant.ID, existing.ID, "You have already reviewed this restaurant. You can update your review here.")
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

	if errors.Is(err, models.ErrAlreadyReviewed) {
		if existing := existingReview(r, restaurant.ID); existing != nil {
			redirectToReview(w, r, restaurant.ID, existing.ID, "You have already reviewed this restaurant. You can update your review here.")
			return
		}
		// The review exists but could not be read back, so there is nowhere
		// specific to send them.
		flash(w, r, "error", "You have already reviewed this restaurant.")
		http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex(), http.StatusFound)
		return
	}
	if err != nil {
		flashFailure(w, r, err, "creating comment", "Something went wrong adding your review.")
		http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex()+"/comments/new", http.StatusFound)
		return
	}
	flash(w, r, "success", "Review added!")
	http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex(), http.StatusFound)
}

// existingReview returns the current user's review of a restaurant, or nil if
// they have not reviewed it or the lookup failed.
func existingReview(r *http.Request, restaurantID bson.ObjectID) *models.Comment {
	user := CurrentUser(r)
	if user == nil {
		return nil
	}
	review, err := models.FindCommentByAuthor(r.Context(), restaurantID, user.ID)
	if err != nil {
		logger(r).Error("looking up existing review",
			"restaurant_id", restaurantID.Hex(),
			"user_id", user.ID.Hex(),
			"error", err,
		)
		return nil
	}
	return review
}

func redirectToReview(w http.ResponseWriter, r *http.Request, restaurantID, commentID bson.ObjectID, message string) {
	flash(w, r, "error", message)
	http.Redirect(w, r,
		"/restaurants/"+restaurantID.Hex()+"/comments/"+commentID.Hex()+"/edit",
		http.StatusFound)
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
