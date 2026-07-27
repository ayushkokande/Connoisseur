package models

import (
	"context"
	"errors"
	"testing"
)

// An identity that has signed in before reaches its own account, and one that
// has not is reported as new so the caller knows to ask for a username.
func TestFindUserByIdentity(t *testing.T) {
	requireMongo(t)
	requireUniqueIdentityIndex(t)
	ctx := context.Background()

	identity := Identity{Provider: "test", Subject: "subject-1", Email: "someone@example.com"}
	created, err := CreateUser(ctx, identity, "identity_subject")
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindUserByIdentity(ctx, identity)
	if err != nil {
		t.Fatalf("finding a known identity: %v", err)
	}
	if found.ID != created.ID {
		t.Error("a known identity reached a different account")
	}

	unknown := Identity{Provider: "test", Subject: "subject-2"}
	if _, err := FindUserByIdentity(ctx, unknown); !errors.Is(err, ErrNoSuchUser) {
		t.Errorf("an unknown identity returned %v, want ErrNoSuchUser", err)
	}
}

// The subject identifies the account, not the address. Someone whose email
// changes at the provider must land on the same account, and the address on file
// should follow rather than go stale.
func TestIdentityIsKeyedOnSubjectNotEmail(t *testing.T) {
	requireMongo(t)
	requireUniqueIdentityIndex(t)
	ctx := context.Background()

	created, err := CreateUser(ctx,
		Identity{Provider: "test", Subject: "stable-subject", Email: "before@example.com"},
		"email_changer")
	if err != nil {
		t.Fatal(err)
	}

	found, err := FindUserByIdentity(ctx,
		Identity{Provider: "test", Subject: "stable-subject", Email: "after@example.com"})
	if err != nil {
		t.Fatalf("finding the identity after its email changed: %v", err)
	}
	if found.ID != created.ID {
		t.Error("a changed email reached a different account")
	}
	if found.Email != "after@example.com" {
		t.Errorf("the stored email is %q, want it to follow the provider", found.Email)
	}

	// The same address under a different subject is a different person.
	if _, err := FindUserByIdentity(ctx,
		Identity{Provider: "test", Subject: "other-subject", Email: "after@example.com"},
	); !errors.Is(err, ErrNoSuchUser) {
		t.Error("a matching email handed over somebody else's account")
	}
}

// A name may only be claimed once however it is capitalised, or "Admin" and
// "admin" are two accounts and either can be mistaken for the other.
func TestUsernamesAreClaimedRegardlessOfCase(t *testing.T) {
	requireMongo(t)
	requireUniqueUsernameIndex(t)
	requireUniqueIdentityIndex(t)
	ctx := context.Background()

	if _, err := CreateUser(ctx, Identity{Provider: "test", Subject: "case-1"}, "Connoisseur"); err != nil {
		t.Fatal(err)
	}

	for i, attempt := range []string{"connoisseur", "CONNOISSEUR", "ConNoIsSeUr"} {
		identity := Identity{Provider: "test", Subject: "case-taken-" + string(rune('a'+i))}
		if _, err := CreateUser(ctx, identity, attempt); !errors.Is(err, ErrUsernameTaken) {
			t.Errorf("claiming %q returned %v, want ErrUsernameTaken", attempt, err)
		}
	}
}

// Submitting the username form twice, or two callbacks racing, must not leave
// one identity with two accounts.
func TestCreateUserIsIdempotentForOneIdentity(t *testing.T) {
	requireMongo(t)
	requireUniqueUsernameIndex(t)
	requireUniqueIdentityIndex(t)
	ctx := context.Background()

	identity := Identity{Provider: "test", Subject: "double-submit"}
	first, err := CreateUser(ctx, identity, "double_submitter")
	if err != nil {
		t.Fatal(err)
	}

	second, err := CreateUser(ctx, identity, "another_name_entirely")
	if err != nil {
		t.Fatalf("a repeated sign-up returned %v, want the existing account", err)
	}
	if second.ID != first.ID {
		t.Error("a repeated sign-up created a second account for one identity")
	}
	if second.Username != "double_submitter" {
		t.Errorf("the account was renamed to %q by the repeat", second.Username)
	}
}

func TestCreateUserRejectsBadUsernames(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	for _, username := range []string{"", "ab", "bad name!", DeletedUsername} {
		identity := Identity{Provider: "test", Subject: "bad-" + username}
		if _, err := CreateUser(ctx, identity, username); err == nil {
			t.Errorf("username %q was accepted", username)
		}
	}
}

// The placeholder must be a name the validation rules reject, or someone could
// register it and appear to have written every deleted account's content.
func TestDeletedUsernameIsUnregisterable(t *testing.T) {
	if usernamePattern.MatchString(DeletedUsername) {
		t.Errorf("%q matches the username pattern, so it can be registered", DeletedUsername)
	}
	if err := validateUsername(DeletedUsername); err == nil {
		t.Errorf("%q passes username validation", DeletedUsername)
	}
}

// Signing out everywhere has to raise the version, since that is what
// invalidates the sessions already handed out.
func TestSignOutEverywhereRaisesTheCredentialVersion(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	user := newUser(t, "version_subject")
	before := user.CredentialVersion

	if err := SignOutEverywhere(ctx, user.ID); err != nil {
		t.Fatalf("signing out everywhere: %v", err)
	}

	after, err := FindUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CredentialVersion <= before {
		t.Errorf("the credential version went from %d to %d, want it to rise", before, after.CredentialVersion)
	}
}

// The rename runs before the account is removed. The other order would free the
// username while it still sat on the old content, so whoever registered it next
// would appear to have written it.
func TestDeleteUserRenamesContentBeforeFreeingTheName(t *testing.T) {
	requireMongo(t)
	requireUniqueUsernameIndex(t)
	requireUniqueIdentityIndex(t)
	ctx := context.Background()

	user := newUser(t, "leaving_author")

	restaurant := &Restaurant{
		Name:        "Left Behind Bistro",
		Image:       "https://example.com/photo.jpg",
		Cuisine:     "Italian",
		PriceRange:  "$$",
		Description: "Written before the account went.",
		Author:      Author{ID: user.ID, Username: user.Username},
	}
	if err := CreateRestaurant(ctx, restaurant); err != nil {
		t.Fatal(err)
	}

	if err := DeleteUser(ctx, user.ID); err != nil {
		t.Fatalf("deleting user: %v", err)
	}

	reloaded := reload(t, restaurant.ID)
	if reloaded.Author.Username != DeletedUsername {
		t.Errorf("the restaurant credits %q, want %q", reloaded.Author.Username, DeletedUsername)
	}
	// The ID is kept, so the content stays uneditable rather than falling to
	// whoever happens to match a zeroed one.
	if reloaded.Author.ID != user.ID {
		t.Error("the author ID was cleared, so ownership is no longer well defined")
	}

	// Someone taking the freed name inherits nothing.
	successor := newUser(t, "leaving_author")
	if successor.ID == user.ID {
		t.Fatal("the new account reused the old ID")
	}
	if again := reload(t, restaurant.ID); again.Author.Username == successor.Username {
		t.Error("the old content is credited to whoever took the name next")
	}
}
