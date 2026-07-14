package models

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Comment struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	Text      string        `bson:"text"`
	CreatedAt time.Time     `bson:"createdAt"`
	Author    Author        `bson:"author"`
}

func CreateComment(ctx context.Context, text string, author Author) (*Comment, error) {
	comment := &Comment{
		ID:        bson.NewObjectID(),
		Text:      text,
		CreatedAt: time.Now(),
		Author:    author,
	}
	if _, err := comments.InsertOne(ctx, comment); err != nil {
		return nil, err
	}
	return comment, nil
}

func FindCommentByID(ctx context.Context, id bson.ObjectID) (*Comment, error) {
	var comment Comment
	if err := comments.FindOne(ctx, bson.M{"_id": id}).Decode(&comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

func FindCommentsByIDs(ctx context.Context, ids []bson.ObjectID) ([]Comment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	cursor, err := comments.Find(ctx, bson.M{"_id": bson.M{"$in": ids}},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}))
	if err != nil {
		return nil, err
	}
	var results []Comment
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func UpdateCommentText(ctx context.Context, id bson.ObjectID, text string) error {
	_, err := comments.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"text": text}})
	return err
}

func DeleteComment(ctx context.Context, id bson.ObjectID) error {
	_, err := comments.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func DeleteComments(ctx context.Context, ids []bson.ObjectID) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := comments.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}})
	return err
}
