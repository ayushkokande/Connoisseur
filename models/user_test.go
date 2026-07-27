package models

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// The dummy hash only disguises the timing of an unknown username if it costs
// the same to check as a real one, and bcrypt bakes its cost into the hash. If
// bcrypt.DefaultCost ever moves, the constant has to be regenerated.
func TestDummyHashMatchesDefaultCost(t *testing.T) {
	cost, err := bcrypt.Cost([]byte(dummyPasswordHash))
	if err != nil {
		t.Fatalf("the dummy hash is not a valid bcrypt hash: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Errorf("the dummy hash costs %d, want %d; regenerate it", cost, bcrypt.DefaultCost)
	}
}

func TestAuthenticateUser(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	if _, err := RegisterUser(ctx, "auth_subject", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}

	t.Run("correct credentials", func(t *testing.T) {
		user, err := AuthenticateUser(ctx, "auth_subject", "correct-horse-battery")
		if err != nil {
			t.Fatalf("authenticating with the right password: %v", err)
		}
		if user.Username != "auth_subject" {
			t.Errorf("authenticated as %q, want %q", user.Username, "auth_subject")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		if _, err := AuthenticateUser(ctx, "auth_subject", "not-the-password"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("got %v, want ErrInvalidCredentials", err)
		}
	})

	t.Run("unknown username", func(t *testing.T) {
		if _, err := AuthenticateUser(ctx, "no_such_person", "correct-horse-battery"); !errors.Is(err, ErrInvalidCredentials) {
			t.Errorf("got %v, want ErrInvalidCredentials", err)
		}
	})
}

// A name may only be claimed once however it is capitalised, or "Admin" and
// "admin" are two accounts and either can be mistaken for the other.
func TestUsernamesAreClaimedRegardlessOfCase(t *testing.T) {
	requireMongo(t)
	requireUniqueUsernameIndex(t)
	ctx := context.Background()

	if _, err := RegisterUser(ctx, "Connoisseur", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}

	for _, attempt := range []string{"connoisseur", "CONNOISSEUR", "ConNoIsSeUr"} {
		if _, err := RegisterUser(ctx, attempt, "another-good-password"); !errors.Is(err, ErrUsernameTaken) {
			t.Errorf("registering %q returned %v, want ErrUsernameTaken", attempt, err)
		}
	}
}

// Logging in should not depend on remembering the capitalisation, while the
// display name keeps it.
func TestLoginIgnoresCaseButDisplayNameKeepsIt(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	created, err := RegisterUser(ctx, "MixedCase", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if created.Username != "MixedCase" {
		t.Errorf("stored display name is %q, want %q", created.Username, "MixedCase")
	}

	for _, attempt := range []string{"MixedCase", "mixedcase", "MIXEDCASE"} {
		user, err := AuthenticateUser(ctx, attempt, "correct-horse-battery")
		if err != nil {
			t.Fatalf("logging in as %q: %v", attempt, err)
		}
		if user.ID != created.ID {
			t.Errorf("logging in as %q reached a different account", attempt)
		}
		if user.Username != "MixedCase" {
			t.Errorf("logging in as %q displays %q, want %q", attempt, user.Username, "MixedCase")
		}
	}
}

// Rejecting an unknown username must cost about what rejecting a known one
// costs. Without the dummy hash the unknown path skips bcrypt entirely and
// returns in microseconds against tens of milliseconds, which tells an attacker
// exactly which accounts exist.
func TestUnknownUsernameCostsAsMuchAsAWrongPassword(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	if _, err := RegisterUser(ctx, "timing_subject", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}

	// The fastest of several runs, because scheduling noise only ever makes a
	// run slower. That makes this a lower bound on each path's real cost and
	// keeps the comparison stable on a loaded CI machine.
	fastest := func(username string) time.Duration {
		best := time.Duration(1<<63 - 1)
		for range 3 {
			start := time.Now()
			if _, err := AuthenticateUser(ctx, username, "wrong-password-entirely"); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("authenticating %q: got %v, want ErrInvalidCredentials", username, err)
			}
			if elapsed := time.Since(start); elapsed < best {
				best = elapsed
			}
		}
		return best
	}

	known := fastest("timing_subject")
	unknown := fastest("no_such_person")

	// Half the known-username cost is a wide margin: the gap this guards
	// against is three orders of magnitude, not a factor of two.
	if unknown < known/2 {
		t.Errorf("an unknown username was rejected in %v against %v for a known one, "+
			"which is a usable oracle for enumerating accounts", unknown, known)
	}
}

// The placeholder must be a name the validation rules reject, or someone could
// register it and appear to have written every deleted account's content.
func TestDeletedUsernameIsUnregisterable(t *testing.T) {
	if usernamePattern.MatchString(DeletedUsername) {
		t.Errorf("%q matches the username pattern, so it can be registered", DeletedUsername)
	}
	if err := validateCredentials(DeletedUsername, "correct-horse-battery"); err == nil {
		t.Errorf("%q passes credential validation", DeletedUsername)
	}
}

// A password change has to raise the version, since that is what invalidates
// sessions issued against the old one.
func TestChangePasswordRaisesTheCredentialVersion(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	user, err := RegisterUser(ctx, "version_subject", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	before := user.CredentialVersion

	if err := ChangePassword(ctx, user.ID, "correct-horse-battery", "a-brand-new-password"); err != nil {
		t.Fatalf("changing password: %v", err)
	}

	after, err := FindUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CredentialVersion <= before {
		t.Errorf("the credential version went from %d to %d, want it to rise", before, after.CredentialVersion)
	}

	// A wrong current password changes nothing, version included.
	if err := ChangePassword(ctx, user.ID, "not-the-password", "another-new-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("changing with the wrong current password returned %v, want ErrInvalidCredentials", err)
	}
	unchanged, err := FindUserByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.CredentialVersion != after.CredentialVersion {
		t.Error("a refused password change still raised the version")
	}
}

// The rename runs before the account is removed. The other order would free the
// username while it still sat on the old content, so whoever registered it next
// would appear to have written it.
func TestDeleteUserRenamesContentBeforeFreeingTheName(t *testing.T) {
	requireMongo(t)
	requireUniqueUsernameIndex(t)
	ctx := context.Background()

	user, err := RegisterUser(ctx, "leaving_author", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}

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

	if err := DeleteUser(ctx, user.ID, "correct-horse-battery"); err != nil {
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

	// Someone registering the freed name inherits nothing.
	successor, err := RegisterUser(ctx, "leaving_author", "another-good-password")
	if err != nil {
		t.Fatalf("re-registering the freed name: %v", err)
	}
	if successor.ID == user.ID {
		t.Fatal("the new account reused the old ID")
	}
	if again := reload(t, restaurant.ID); again.Author.Username == successor.Username {
		t.Error("the old content is credited to whoever took the name next")
	}
}
