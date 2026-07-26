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
