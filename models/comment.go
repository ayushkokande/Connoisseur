package models

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Comment is a review of a restaurant. It points at its restaurant rather than
// the restaurant holding a list of comment IDs: with the reference on this side,
// writing a review is a single insert that cannot half-succeed and leave an
// orphan behind.
type Comment struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	RestaurantID bson.ObjectID `bson:"restaurantId"`
	// Rating is 1 to 5 stars, or 0 for reviews written before ratings existed.
	// Unrated reviews are excluded from a restaurant's average.
	Rating    int       `bson:"rating"`
	Text      string    `bson:"text"`
	CreatedAt time.Time `bson:"createdAt"`
	Author    Author    `bson:"author"`
}

// IsRated reports whether this review carries a star rating.
func (c Comment) IsRated() bool { return c.Rating >= minRating }

// Stars renders the rating as filled and empty stars for display.
func (c Comment) Stars() string { return Stars(c.Rating) }

// CreateComment stores a review and refreshes its restaurant's rating summary.
func CreateComment(ctx context.Context, restaurantID bson.ObjectID, rating int, text string, author Author) (*Comment, error) {
	text, err := validateCommentText(text)
	if err != nil {
		return nil, err
	}
	if err := validateRating(rating); err != nil {
		return nil, err
	}

	comment := &Comment{
		ID:           bson.NewObjectID(),
		RestaurantID: restaurantID,
		Rating:       rating,
		Text:         text,
		CreatedAt:    time.Now(),
		Author:       author,
	}
	if _, err := comments.InsertOne(ctx, comment); err != nil {
		return nil, err
	}
	return comment, RefreshRating(ctx, restaurantID)
}

func FindCommentByID(ctx context.Context, id bson.ObjectID) (*Comment, error) {
	var comment Comment
	if err := comments.FindOne(ctx, bson.M{"_id": id}).Decode(&comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// FindCommentsByRestaurant returns a restaurant's reviews, oldest first.
func FindCommentsByRestaurant(ctx context.Context, restaurantID bson.ObjectID) ([]Comment, error) {
	cursor, err := comments.Find(ctx,
		bson.M{"restaurantId": restaurantID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var results []Comment
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// UpdateComment rewrites a review's rating and text, then refreshes its
// restaurant's rating summary.
func UpdateComment(ctx context.Context, id bson.ObjectID, rating int, text string) error {
	text, err := validateCommentText(text)
	if err != nil {
		return err
	}
	if err := validateRating(rating); err != nil {
		return err
	}

	var updated Comment
	err = comments.FindOneAndUpdate(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"rating": rating, "text": text}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&updated)
	if err != nil {
		return err
	}
	return RefreshRating(ctx, updated.RestaurantID)
}

// DeleteComment removes a review and refreshes its restaurant's rating summary.
func DeleteComment(ctx context.Context, id bson.ObjectID) error {
	var deleted Comment
	if err := comments.FindOneAndDelete(ctx, bson.M{"_id": id}).Decode(&deleted); err != nil {
		return err
	}
	return RefreshRating(ctx, deleted.RestaurantID)
}

// DeleteCommentsByRestaurant removes every review of a restaurant. It is used
// when the restaurant itself is deleted, so there is no summary left to refresh.
func DeleteCommentsByRestaurant(ctx context.Context, restaurantID bson.ObjectID) error {
	_, err := comments.DeleteMany(ctx, bson.M{"restaurantId": restaurantID})
	return err
}
