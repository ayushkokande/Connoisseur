package models

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Migrate brings existing data up to the current schema. It is idempotent and
// safe to run on every startup: each step is a no-op once applied.
//
// Reviews used to be linked by an array of comment IDs on the restaurant
// document. That reference now lives on the review instead, so the array has to
// be unrolled before anything can read reviews by restaurant.
//
// A restaurant may also only be reviewed once per author now, and a username may
// only be claimed once regardless of case; older data satisfies neither, so
// duplicates are settled here. That is why those unique indexes are created at
// the end of this function rather than in Init: building one against data that
// still contains duplicates fails, and an index build that fails during startup
// takes the whole application down.
func Migrate(ctx context.Context) error {
	linked, err := linkCommentsToRestaurants(ctx)
	if err != nil {
		return fmt.Errorf("linking legacy comments: %w", err)
	}

	deduped, err := removeDuplicateReviews(ctx)
	if err != nil {
		return fmt.Errorf("removing duplicate reviews: %w", err)
	}

	renamed, err := normalizeUsernames(ctx)
	if err != nil {
		return fmt.Errorf("normalizing usernames: %w", err)
	}

	refreshed, err := refreshAllRatings(ctx)
	if err != nil {
		return fmt.Errorf("refreshing rating summaries: %w", err)
	}

	if err := createUniqueReviewIndex(ctx); err != nil {
		return fmt.Errorf("enforcing one review per author: %w", err)
	}

	if err := createUniqueUsernameIndex(ctx); err != nil {
		return fmt.Errorf("enforcing unique usernames: %w", err)
	}

	if linked > 0 || deduped > 0 || renamed > 0 || refreshed > 0 {
		slog.Info("migrated legacy data",
			"comments_linked", linked,
			"duplicate_reviews_removed", deduped,
			"users_renamed", renamed,
			"restaurants_refreshed", refreshed,
		)
	}
	return nil
}

// removeDuplicateReviews keeps each author's most recent review of a restaurant
// and deletes their earlier ones, since the newest is the opinion they would
// expect to find when they go to edit it. Restaurants that lose a review have
// their summary recomputed.
func removeDuplicateReviews(ctx context.Context) (int, error) {
	cursor, err := comments.Aggregate(ctx, []bson.M{
		{"$sort": bson.M{"createdAt": -1, "_id": -1}},
		{"$group": bson.M{
			"_id":          bson.M{"restaurantId": "$restaurantId", "authorId": "$author.id"},
			"ids":          bson.M{"$push": "$_id"},
			"restaurantId": bson.M{"$first": "$restaurantId"},
		}},
		{"$match": bson.M{"$expr": bson.M{"$gt": bson.A{bson.M{"$size": "$ids"}, 1}}}},
	})
	if err != nil {
		return 0, err
	}

	var groups []struct {
		IDs          []bson.ObjectID `bson:"ids"`
		RestaurantID bson.ObjectID   `bson:"restaurantId"`
	}
	if err := cursor.All(ctx, &groups); err != nil {
		return 0, err
	}

	removed := 0
	for _, group := range groups {
		superseded := group.IDs[1:]
		result, err := comments.DeleteMany(ctx, bson.M{"_id": bson.M{"$in": superseded}})
		if err != nil {
			return removed, err
		}
		removed += int(result.DeletedCount)

		if err := RefreshRating(ctx, group.RestaurantID); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

// createUniqueReviewIndex enforces one review per author per restaurant. Creating
// an index that already exists is a no-op, so this is safe to repeat.
func createUniqueReviewIndex(ctx context.Context) error {
	_, err := comments.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "restaurantId", Value: 1}, {Key: "author.id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

// normalizeUsernames gives every user the lowercased name that uniqueness is
// now judged on, and settles the collisions that appear once case stops
// mattering: "Admin" and "admin" used to be two accounts and can no longer be.
//
// The earliest registration keeps the name — ObjectIDs lead with a timestamp,
// so ascending _id is registration order — and later ones are renamed with a
// numeric suffix. Renaming rather than deleting is deliberate: an account owns
// restaurants and reviews, and removing it to satisfy an index would take real
// content with it. The people renamed can still log in, under a name the log
// records.
func normalizeUsernames(ctx context.Context) (int, error) {
	// Backfill first, so the grouping below sees every user.
	if _, err := users.UpdateMany(ctx,
		bson.M{"usernameLower": bson.M{"$exists": false}},
		mongo.Pipeline{{{Key: "$set", Value: bson.M{"usernameLower": bson.M{"$toLower": "$username"}}}}},
	); err != nil {
		return 0, err
	}

	cursor, err := users.Aggregate(ctx, []bson.M{
		{"$sort": bson.M{"_id": 1}},
		{"$group": bson.M{
			"_id":     "$usernameLower",
			"members": bson.M{"$push": bson.M{"id": "$_id", "username": "$username"}},
		}},
		{"$match": bson.M{"$expr": bson.M{"$gt": bson.A{bson.M{"$size": "$members"}, 1}}}},
	})
	if err != nil {
		return 0, err
	}

	var groups []struct {
		Members []struct {
			ID       bson.ObjectID `bson:"id"`
			Username string        `bson:"username"`
		} `bson:"members"`
	}
	if err := cursor.All(ctx, &groups); err != nil {
		return 0, err
	}
	if len(groups) == 0 {
		return 0, nil
	}

	// Only loaded once something actually collides, which for most databases is
	// never, because it holds every name in use at once.
	taken, err := takenUsernames(ctx)
	if err != nil {
		return 0, err
	}

	renamed := 0
	for _, group := range groups {
		for _, member := range group.Members[1:] {
			name := freeUsername(member.Username, taken)
			if err := renameUser(ctx, member.ID, name); err != nil {
				return renamed, err
			}
			slog.Warn("renamed a user whose name collided once case was ignored",
				"user_id", member.ID.Hex(),
				"from", member.Username,
				"to", name,
			)
			renamed++
		}
	}
	return renamed, nil
}

// takenUsernames returns every lowercased name currently in use.
func takenUsernames(ctx context.Context) (map[string]bool, error) {
	cursor, err := users.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"usernameLower": 1}))
	if err != nil {
		return nil, err
	}
	var found []struct {
		UsernameLower string `bson:"usernameLower"`
	}
	if err := cursor.All(ctx, &found); err != nil {
		return nil, err
	}

	taken := make(map[string]bool, len(found))
	for _, user := range found {
		taken[user.UsernameLower] = true
	}
	return taken, nil
}

// freeUsername appends the smallest numeric suffix that leaves base unused,
// shortening base if the suffix would push it past the length limit. The result
// is added to taken, so repeated calls do not hand out the same name twice.
func freeUsername(base string, taken map[string]bool) string {
	for suffix := 2; ; suffix++ {
		tail := "_" + strconv.Itoa(suffix)

		// Counted in runes, because legacy names predate the rule restricting
		// usernames to ASCII and cutting one mid-rune would corrupt it.
		trimmed := []rune(base)
		if len(trimmed)+len(tail) > maxUsernameLen {
			trimmed = trimmed[:maxUsernameLen-len(tail)]
		}

		candidate := string(trimmed) + tail
		if lower := normalizeUsername(candidate); !taken[lower] {
			taken[lower] = true
			return candidate
		}
	}
}

// renameUser changes a user's name, including the copies denormalized onto
// everything they have written. Without that the rename would leave their
// restaurants and reviews crediting a name that no longer belongs to anyone.
func renameUser(ctx context.Context, id bson.ObjectID, name string) error {
	if _, err := users.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"username": name, "usernameLower": normalizeUsername(name)}},
	); err != nil {
		return err
	}

	for _, collection := range []*mongo.Collection{restaurants, comments} {
		if _, err := collection.UpdateMany(ctx,
			bson.M{"author.id": id},
			bson.M{"$set": bson.M{"author.username": name}},
		); err != nil {
			return err
		}
	}
	return nil
}

// createUniqueUsernameIndex enforces one account per name, ignoring case. The
// old index, which was on the raw name and so let "Admin" and "admin" coexist,
// is dropped: the new one already guarantees everything it did.
func createUniqueUsernameIndex(ctx context.Context) error {
	if err := dropIndex(ctx, users, legacyUsernameIndexName); err != nil {
		return err
	}
	_, err := users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "usernameLower", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return err
}

const legacyUsernameIndexName = "username_1"

// Dropping an index reports one of these when there is nothing to drop.
// https://github.com/mongodb/mongo/blob/master/src/mongo/base/error_codes.yml
const (
	namespaceNotFoundCode = 26
	indexNotFoundCode     = 27
)

// dropIndex removes an index, treating both an index that is already gone and a
// collection that does not exist yet as success. The second case is a database
// nothing has ever been written to, where insisting on dropping would fail
// startup on the very first run.
func dropIndex(ctx context.Context, collection *mongo.Collection, name string) error {
	err := collection.Indexes().DropOne(ctx, name)

	var serverErr mongo.ServerError
	if errors.As(err, &serverErr) &&
		(serverErr.HasErrorCode(indexNotFoundCode) || serverErr.HasErrorCode(namespaceNotFoundCode)) {
		return nil
	}
	return err
}

// linkCommentsToRestaurants copies each restaurant's embedded comment IDs onto
// the comments themselves, then drops the array. Reviews whose restaurant no
// longer exists were already unreachable and are removed.
func linkCommentsToRestaurants(ctx context.Context) (int, error) {
	cursor, err := restaurants.Find(ctx, bson.M{"comments": bson.M{"$exists": true}})
	if err != nil {
		return 0, err
	}

	var legacy []struct {
		ID       bson.ObjectID   `bson:"_id"`
		Comments []bson.ObjectID `bson:"comments"`
	}
	if err := cursor.All(ctx, &legacy); err != nil {
		return 0, err
	}

	linked := 0
	for _, restaurant := range legacy {
		if len(restaurant.Comments) > 0 {
			result, err := comments.UpdateMany(ctx,
				bson.M{"_id": bson.M{"$in": restaurant.Comments}, "restaurantId": bson.M{"$exists": false}},
				bson.M{"$set": bson.M{"restaurantId": restaurant.ID}})
			if err != nil {
				return linked, err
			}
			linked += int(result.ModifiedCount)
		}

		if _, err := restaurants.UpdateOne(ctx,
			bson.M{"_id": restaurant.ID},
			bson.M{"$unset": bson.M{"comments": ""}}); err != nil {
			return linked, err
		}
	}

	// Anything still unlinked belonged to a restaurant that no longer exists, so
	// it was already unreachable. This runs even when no restaurant carried a
	// legacy array, since orphans can outlive the restaurants that referenced them.
	if _, err := comments.DeleteMany(ctx, bson.M{"restaurantId": bson.M{"$exists": false}}); err != nil {
		return linked, err
	}
	return linked, nil
}

// refreshAllRatings fills in the review count and average for restaurants that
// have never had one computed.
func refreshAllRatings(ctx context.Context) (int, error) {
	cursor, err := restaurants.Find(ctx,
		bson.M{"$or": []bson.M{
			{"reviewCount": bson.M{"$exists": false}},
			{"avgRating": bson.M{"$exists": false}},
		}})
	if err != nil {
		return 0, err
	}

	var stale []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cursor.All(ctx, &stale); err != nil {
		return 0, err
	}

	for _, restaurant := range stale {
		if err := RefreshRating(ctx, restaurant.ID); err != nil {
			return 0, err
		}
	}
	return len(stale), nil
}
