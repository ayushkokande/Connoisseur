package models

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
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

// ErrAlreadyReviewed is returned when an author tries to review a restaurant
// they have already reviewed. Callers are expected to offer to edit the
// existing review instead.
var ErrAlreadyReviewed = errors.New("models: author has already reviewed this restaurant")

// CreateComment stores a review and refreshes its restaurant's rating summary.
// An author may only review a restaurant once; a second attempt returns
// ErrAlreadyReviewed.
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
	// The one-review-per-author rule is enforced by a unique index rather than
	// by checking first, so two simultaneous submissions cannot both pass the
	// check and then both insert.
	if _, err := comments.InsertOne(ctx, comment); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrAlreadyReviewed
		}
		return nil, err
	}
	return comment, RefreshRating(ctx, restaurantID)
}

// FindCommentByAuthor returns an author's review of a restaurant, or nil when
// they have not reviewed it.
func FindCommentByAuthor(ctx context.Context, restaurantID, authorID bson.ObjectID) (*Comment, error) {
	var comment Comment
	err := comments.FindOne(ctx, bson.M{
		"restaurantId": restaurantID,
		"author.id":    authorID,
	}).Decode(&comment)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func FindCommentByID(ctx context.Context, id bson.ObjectID) (*Comment, error) {
	var comment Comment
	if err := comments.FindOne(ctx, bson.M{"_id": id}).Decode(&comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// reviewOrder is oldest first, with _id breaking ties. Timestamps are stored to
// the millisecond, so without a tiebreaker two reviews written in the same
// millisecond have no defined order — and an undefined order across a page
// boundary lets one appear on two pages or on none.
var reviewOrder = bson.D{{Key: "createdAt", Value: 1}, {Key: "_id", Value: 1}}

// FindCommentsByRestaurant returns all of a restaurant's reviews, oldest first.
// Anything rendering them wants FindCommentsPage instead; this is for callers
// that genuinely need the whole set.
func FindCommentsByRestaurant(ctx context.Context, restaurantID bson.ObjectID) ([]Comment, error) {
	cursor, err := comments.Find(ctx,
		bson.M{"restaurantId": restaurantID},
		options.Find().SetSort(reviewOrder))
	if err != nil {
		return nil, err
	}
	var results []Comment
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

const (
	// DefaultReviewsPerPage shows a useful number of reviews without pushing the
	// rest of the restaurant page out of reach.
	DefaultReviewsPerPage = 10
	maxReviewsPerPage     = 50
)

// CommentPage is one page of a restaurant's reviews plus the totals needed to
// render pagination controls.
type CommentPage struct {
	Comments   []Comment
	Total      int64
	Page       int
	PerPage    int
	TotalPages int
}

// HasPrev reports whether a previous page exists.
func (p CommentPage) HasPrev() bool { return p.Page > 1 }

// HasNext reports whether a further page exists.
func (p CommentPage) HasNext() bool { return p.Page < p.TotalPages }

// FindCommentsPage returns one page of a restaurant's reviews, oldest first.
// Out-of-range arguments are clamped rather than rejected, so a bookmarked link
// to a page that no longer exists still shows reviews.
func FindCommentsPage(ctx context.Context, restaurantID bson.ObjectID, page, perPage int) (*CommentPage, error) {
	if perPage < 1 || perPage > maxReviewsPerPage {
		perPage = DefaultReviewsPerPage
	}
	if page < 1 {
		page = 1
	}

	filter := bson.M{"restaurantId": restaurantID}
	total, err := comments.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	totalPages := int((total + int64(perPage) - 1) / int64(perPage))
	if totalPages < 1 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	cursor, err := comments.Find(ctx, filter, options.Find().
		SetSort(reviewOrder).
		SetSkip(int64((page-1)*perPage)).
		SetLimit(int64(perPage)))
	if err != nil {
		return nil, err
	}
	var results []Comment
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	return &CommentPage{
		Comments:   results,
		Total:      total,
		Page:       page,
		PerPage:    perPage,
		TotalPages: totalPages,
	}, nil
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
