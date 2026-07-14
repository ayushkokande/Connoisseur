package web

import (
	"log"
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/shivamdubey91/connoisseur/models"
)

func commentsNewForm(w http.ResponseWriter, r *http.Request) {
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
	render(w, r, "comments/new", map[string]any{"Restaurant": restaurant})
}

func commentsCreate(w http.ResponseWriter, r *http.Request) {
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
	user := CurrentUser(r)
	comment, err := models.CreateComment(r.Context(), r.PostFormValue("text"), models.Author{
		ID:       user.ID,
		Username: user.Username,
	})
	if err != nil {
		flashFailure(w, r, err, "creating comment", "Something went wrong adding your review.")
		http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex()+"/comments/new", http.StatusFound)
		return
	}
	if err := models.AddCommentToRestaurant(r.Context(), restaurant.ID, comment.ID); err != nil {
		log.Printf("linking comment: %v", err)
	}
	flash(w, r, "success", "Review added!")
	http.Redirect(w, r, "/restaurants/"+restaurant.ID.Hex(), http.StatusFound)
}

func commentsEditForm(w http.ResponseWriter, r *http.Request) {
	commentID, err := bson.ObjectIDFromHex(r.PathValue("comment_id"))
	if err != nil {
		flash(w, r, "error", "Review not found!")
		redirectBack(w, r)
		return
	}
	comment, err := models.FindCommentByID(r.Context(), commentID)
	if err != nil {
		flash(w, r, "error", "Review not found!")
		redirectBack(w, r)
		return
	}
	render(w, r, "comments/edit", map[string]any{
		"RestaurantID": r.PathValue("id"),
		"Comment":      comment,
	})
}

func commentsUpdate(w http.ResponseWriter, r *http.Request) {
	commentID, err := bson.ObjectIDFromHex(r.PathValue("comment_id"))
	if err != nil {
		flash(w, r, "error", "Review not found!")
		redirectBack(w, r)
		return
	}
	if err := models.UpdateCommentText(r.Context(), commentID, r.PostFormValue("text")); err != nil {
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
		log.Printf("deleting comment: %v", err)
		flash(w, r, "error", "Something went wrong deleting your review.")
		redirectBack(w, r)
		return
	}
	restaurantOID, err := bson.ObjectIDFromHex(r.PathValue("id"))
	if err == nil {
		if err := models.RemoveCommentFromRestaurant(r.Context(), restaurantOID, commentID); err != nil {
			log.Printf("unlinking comment: %v", err)
		}
	}
	flash(w, r, "success", "Review deleted!")
	http.Redirect(w, r, "/restaurants/"+r.PathValue("id"), http.StatusFound)
}
