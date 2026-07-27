package models

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type User struct {
	ID       bson.ObjectID `bson:"_id,omitempty"`
	Username string        `bson:"username"`
	// UsernameLower is what uniqueness is enforced on, so that a name cannot be
	// claimed twice in different cases. Username keeps the capitalisation the
	// person chose, which is what gets displayed.
	UsernameLower string `bson:"usernameLower"`

	// Provider and Subject are the identity signed in with. Subject is the
	// provider's own immutable identifier for the account — not the email, which
	// people change and providers reassign. Together they are what a sign-in
	// looks up by, and what uniqueness of an identity is enforced on.
	Provider string `bson:"provider"`
	Subject  string `bson:"subject"`
	// Email is kept for support and for telling two accounts apart by hand. It
	// is deliberately not used to find an account: a provider that let an
	// address be reassigned would otherwise hand over somebody else's account.
	Email string `bson:"email"`

	// CredentialVersion rises when the account signs out everywhere. A session
	// records the version it was issued against, so raising it invalidates every
	// session already handed out. Checking it costs nothing extra: the user is
	// already loaded once per request.
	CredentialVersion int `bson:"credentialVersion"`
}

// Identity is who a provider says is signing in.
type Identity struct {
	Provider string
	Subject  string
	Email    string
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
	ErrUsernameTaken = errors.New("that username is already taken")
	// ErrNoSuchUser is returned when an identity has never signed in before, so
	// the caller knows to ask for a username rather than to fail.
	ErrNoSuchUser = errors.New("no account for that identity")
)

// FindUserByIdentity returns the account an identity signs in to, or
// ErrNoSuchUser when it has never signed in before.
func FindUserByIdentity(ctx context.Context, identity Identity) (*User, error) {
	var user User
	err := users.FindOne(ctx, bson.M{
		"provider": identity.Provider,
		"subject":  identity.Subject,
	}).Decode(&user)

	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNoSuchUser
	}
	if err != nil {
		return nil, err
	}

	// The address on file follows the provider, so support is not looking at a
	// value that stopped being true years ago. It identifies nothing, so
	// changing it grants nothing.
	if identity.Email != "" && identity.Email != user.Email {
		if _, err := users.UpdateOne(ctx,
			bson.M{"_id": user.ID},
			bson.M{"$set": bson.M{"email": identity.Email}},
		); err != nil {
			return nil, err
		}
		user.Email = identity.Email
	}
	return &user, nil
}

// CreateUser registers a new account for an identity under a chosen username.
//
// Both uniqueness rules are enforced by indexes rather than by looking first,
// so two sign-ins racing on the same name — or the same identity submitting the
// form twice — cannot both pass a check and then both insert.
func CreateUser(ctx context.Context, identity Identity, username string) (*User, error) {
	if err := validateUsername(username); err != nil {
		return nil, err
	}

	user := &User{
		ID:            bson.NewObjectID(),
		Username:      username,
		UsernameLower: normalizeUsername(username),
		Provider:      identity.Provider,
		Subject:       identity.Subject,
		Email:         identity.Email,
	}
	if _, err := users.InsertOne(ctx, user); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			// Either the name went in the meantime or this identity already has
			// an account. The second case is the double submission, so hand back
			// the account rather than an error.
			if existing, findErr := FindUserByIdentity(ctx, identity); findErr == nil {
				return existing, nil
			}
			return nil, ErrUsernameTaken
		}
		return nil, err
	}
	return user, nil
}

// SignOutEverywhere invalidates every session already issued for an account,
// which is what someone reaches for when they think a session has been taken.
func SignOutEverywhere(ctx context.Context, id bson.ObjectID) error {
	_, err := users.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$inc": bson.M{"credentialVersion": 1}})
	return err
}

// DeleteUser removes an account and leaves the restaurants and reviews it wrote
// credited to DeletedUsername.
func DeleteUser(ctx context.Context, id bson.ObjectID) error {
	// The content is renamed before the account goes, and the order is not
	// arbitrary. Deleting first would free the username for anyone to register
	// while their name still sat on the old content, so whoever took it next
	// would appear to have written it. Failing the other way around only leaves
	// an account whose content already reads as deleted, which a retry settles.
	if err := anonymizeAuthoredContent(ctx, id); err != nil {
		return err
	}

	_, err := users.DeleteOne(ctx, bson.M{"_id": id})
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
