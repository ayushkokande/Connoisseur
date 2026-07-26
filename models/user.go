package models

import (
	"context"
	"errors"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	Username     string        `bson:"username"`
	PasswordHash []byte        `bson:"passwordHash"`
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
		ID:           bson.NewObjectID(),
		Username:     username,
		PasswordHash: hash,
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
	err := users.FindOne(ctx, bson.M{"username": username}).Decode(&user)
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

func FindUserByID(ctx context.Context, id bson.ObjectID) (*User, error) {
	var user User
	if err := users.FindOne(ctx, bson.M{"_id": id}).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
}
