package models

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID       bson.ObjectID `bson:"_id,omitempty"`
	Username string        `bson:"username"`
	// UsernameLower is what uniqueness is enforced on and what logging in looks
	// up by, so that a name cannot be claimed twice in different cases and
	// nobody has to remember how they capitalised it. Username keeps the
	// capitalisation they chose, which is what gets displayed.
	UsernameLower string `bson:"usernameLower"`
	PasswordHash  []byte `bson:"passwordHash"`
	// CredentialVersion rises whenever the password changes. A session records
	// the version it was issued against, so changing a password invalidates the
	// sessions held anywhere else — which is the whole point of changing it
	// after a compromise. Checking it costs nothing extra: the user is already
	// loaded once per request.
	CredentialVersion int `bson:"credentialVersion"`
}

// DeletedUsername replaces the name on content whose author has deleted their
// account. The brackets are what make it safe: usernames may only contain
// letters, numbers and underscores, so no real account can ever be created
// under this name and be mistaken for the placeholder.
const DeletedUsername = "[deleted_user]"

// normalizeUsername reduces a username to the form uniqueness is judged on.
func normalizeUsername(username string) string {
	return strings.ToLower(username)
}

var (
	ErrUsernameTaken      = errors.New("a user with the given username is already registered")
	ErrInvalidCredentials = errors.New("username or password is incorrect")
)

// RegisterUser validates the credentials, hashes the password and creates the user.
func RegisterUser(ctx context.Context, username, password string) (*User, error) {
	if err := validateCredentials(username, password); err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &User{
		ID:            bson.NewObjectID(),
		Username:      username,
		UsernameLower: normalizeUsername(username),
		PasswordHash:  hash,
	}
	if _, err := users.InsertOne(ctx, user); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	return user, nil
}

// dummyPasswordHash is compared against when the username does not exist, so
// that authentication costs the same whether or not it does. Without it the
// unknown-username path skips bcrypt and returns in microseconds while a known
// username spends tens of milliseconds hashing, which is a reliable oracle for
// enumerating registered accounts.
//
// It hashes a random string that was thrown away, and the result of comparing
// against it is discarded regardless, so there is no harm in it being public.
// Its cost has to track bcrypt.DefaultCost for the timings to match, which
// TestDummyHashMatchesDefaultCost checks.
const dummyPasswordHash = "$2a$10$AQVY8W1rx8ACRtIPf3fjw.zpcBazg0KIq/831nozdLBhpgB5YAmRi"

// AuthenticateUser checks the username/password pair.
func AuthenticateUser(ctx context.Context, username, password string) (*User, error) {
	var user User
	err := users.FindOne(ctx, bson.M{"usernameLower": normalizeUsername(username)}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			// Hash anyway, so that the reply takes as long as a real check.
			bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte(password))
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)) != nil {
		return nil, ErrInvalidCredentials
	}
	return &user, nil
}

// ChangePassword replaces a user's password, given their current one. Requiring
// the current password means a stolen session cannot be used to take the account
// over outright.
func ChangePassword(ctx context.Context, id bson.ObjectID, current, next string) error {
	user, err := FindUserByID(ctx, id)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(current)) != nil {
		return ErrInvalidCredentials
	}
	if err := validatePassword(next); err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	// Raising the version in the same write logs out every other session, so a
	// password changed because someone else had it actually removes them.
	_, err = users.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{
			"$set": bson.M{"passwordHash": hash},
			"$inc": bson.M{"credentialVersion": 1},
		})
	return err
}

// DeleteUser removes an account, given its password, and leaves the restaurants
// and reviews it wrote credited to DeletedUsername.
func DeleteUser(ctx context.Context, id bson.ObjectID, password string) error {
	user, err := FindUserByID(ctx, id)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword(user.PasswordHash, []byte(password)) != nil {
		return ErrInvalidCredentials
	}

	// The content is renamed before the account goes, and the order is not
	// arbitrary. Deleting first would free the username for anyone to register
	// while their name still sat on the old content, so whoever took it next
	// would appear to have written it. Failing the other way around only leaves
	// an account whose content already reads as deleted, which a retry settles.
	if err := anonymizeAuthoredContent(ctx, id); err != nil {
		return err
	}

	_, err = users.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// anonymizeAuthoredContent renames an author across everything they wrote. The
// author ID is left alone: it no longer resolves to anyone, so the content
// becomes uneditable, and keeping it distinct stops every deleted account's work
// collapsing into one identity.
func anonymizeAuthoredContent(ctx context.Context, id bson.ObjectID) error {
	for _, collection := range []*mongo.Collection{restaurants, comments} {
		if _, err := collection.UpdateMany(ctx,
			bson.M{"author.id": id},
			bson.M{"$set": bson.M{"author.username": DeletedUsername}},
		); err != nil {
			return err
		}
	}
	return nil
}

func FindUserByID(ctx context.Context, id bson.ObjectID) (*User, error) {
	var user User
	if err := users.FindOne(ctx, bson.M{"_id": id}).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
}
