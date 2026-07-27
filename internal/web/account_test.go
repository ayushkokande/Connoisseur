package web

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

// loggedIn reports whether the browser still has a working session.
func (b *browser) loggedIn() bool {
	b.t.Helper()
	body, _ := b.get("/restaurants")
	return strings.Contains(body, "Signed in as")
}

// signOutEverywhere submits the account page's session-clearing form.
func (b *browser) signOutEverywhere() {
	b.t.Helper()
	resp := b.post("/account", "/account/sessions", url.Values{})
	resp.Body.Close()
}

// Signing out everywhere is what someone reaches for when they think a session
// has been taken, so it has to end that session rather than only their own.
func TestSignOutEverywhereEndsOtherSessions(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("session_owner")

	// A second signed-in session for the same account, standing in for whoever
	// else is holding one.
	user, err := models.FindUserByIdentity(context.Background(), models.Identity{
		Provider: "test", Subject: currentSubject(t, owner),
	})
	if err != nil {
		t.Fatal(err)
	}
	intruder := newBrowser(t)
	if landed := intruder.signIn(user.Subject, user.Email); landed != "/restaurants" {
		t.Fatalf("the second session landed on %s, want /restaurants", landed)
	}
	if !intruder.loggedIn() {
		t.Fatal("the second session was never established")
	}

	owner.signOutEverywhere()

	if intruder.loggedIn() {
		t.Error("the other session survived signing out everywhere")
	}
	if !owner.loggedIn() {
		t.Error("the browser that asked was signed out too")
	}
}

// Deleting an account keeps what it wrote, because other people's reviews of a
// restaurant would otherwise disappear along with whoever added it.
func TestDeleteAccountKeepsContentUnderThePlaceholder(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	leaver := newBrowser(t)
	leaver.register("account_leaver")
	restaurantID := leaver.createRestaurant("Abandoned Bistro")
	leaver.createRating(restaurantID, 4, "My own review of my own place.")

	// Somebody else's review of the same restaurant, which must survive intact.
	other := newBrowser(t)
	other.register("account_stayer")
	other.createRating(restaurantID, 2, "Someone else's opinion.")

	leaver.deleteAccount("account_leaver")

	if leaver.loggedIn() {
		t.Error("the account was deleted but the session still works")
	}

	restaurant, err := models.FindRestaurantByID(ctx, mustID(t, restaurantID))
	if err != nil {
		t.Fatalf("the restaurant should still exist: %v", err)
	}
	if restaurant.Author.Username != models.DeletedUsername {
		t.Errorf("the restaurant credits %q, want %q", restaurant.Author.Username, models.DeletedUsername)
	}

	reviews, err := models.FindCommentsByRestaurant(ctx, mustID(t, restaurantID))
	if err != nil {
		t.Fatal(err)
	}
	if len(reviews) != 2 {
		t.Fatalf("%d reviews remain, want 2", len(reviews))
	}
	for _, review := range reviews {
		switch review.Text {
		case "My own review of my own place.":
			if review.Author.Username != models.DeletedUsername {
				t.Errorf("the leaver's review credits %q, want %q", review.Author.Username, models.DeletedUsername)
			}
		case "Someone else's opinion.":
			if review.Author.Username != "account_stayer" {
				t.Errorf("another person's review was renamed to %q", review.Author.Username)
			}
		default:
			t.Errorf("unexpected review %q", review.Text)
		}
	}

	body, _ := newBrowser(t).get("/restaurants/" + restaurantID)
	if strings.Contains(body, "account_leaver") {
		t.Error("the deleted account's name still appears on the page")
	}
}

// The identity is freed along with the account, so signing in again is a fresh
// start rather than a resurrection.
func TestDeletedAccountSigningInAgainStartsOver(t *testing.T) {
	requireMongo(t)

	subject := newSubject()
	leaver := newBrowser(t)
	if landed := leaver.signIn(subject, "returning@example.com"); landed != "/signup" {
		t.Fatalf("a new identity landed on %s, want /signup", landed)
	}
	resp := leaver.post("/signup", "/signup", url.Values{"username": {"returning_user"}})
	resp.Body.Close()

	leaver.deleteAccount("returning_user")

	// The same person signing in again is treated as new, and the old name is
	// free for them to take back.
	returning := newBrowser(t)
	if landed := returning.signIn(subject, "returning@example.com"); landed != "/signup" {
		t.Errorf("signing in after deletion landed on %s, want /signup", landed)
	}
	resp = returning.post("/signup", "/signup", url.Values{"username": {"returning_user"}})
	resp.Body.Close()
	if !returning.loggedIn() {
		t.Error("the freed username could not be taken again")
	}
}

// Deleting is irreversible, so it should take more than one button press.
func TestDeleteAccountRequiresTheUsername(t *testing.T) {
	requireMongo(t)
	ctx := context.Background()

	user := newBrowser(t)
	user.register("account_keeper")

	user.deleteAccount("not-my-username")

	if !user.loggedIn() {
		t.Error("a refused deletion signed the user out")
	}
	if _, err := models.FindUserByIdentity(ctx, models.Identity{
		Provider: "test", Subject: currentSubject(t, user),
	}); errors.Is(err, models.ErrNoSuchUser) {
		t.Error("the account was deleted without the username being confirmed")
	}
}

// The account page is a signed-in page, and reaching it while logged out would
// mean rendering a template that reads the current user.
func TestAccountPageRequiresLogin(t *testing.T) {
	requireMongo(t)

	anon := newBrowser(t).noRedirects()
	resp := anon.getResponse("/account")
	defer resp.Body.Close()

	if location := resp.Header.Get("Location"); location != "/login" {
		t.Errorf("an anonymous visitor was sent to %q, want /login", location)
	}
}
