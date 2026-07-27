package web

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/ayushkokande/Connoisseur/models"
)

// changePassword submits the password form.
func (b *browser) changePassword(current, next, confirm string) {
	b.t.Helper()
	resp := b.post("/account", "/account/password?_method=PUT", url.Values{
		"current_password": {current},
		"new_password":     {next},
		"confirm_password": {confirm},
	})
	resp.Body.Close()
}

// loggedIn reports whether the browser still has a working session.
func (b *browser) loggedIn() bool {
	b.t.Helper()
	body, _ := b.get("/restaurants")
	return strings.Contains(body, "Signed in as")
}

func TestChangePassword(t *testing.T) {
	requireMongo(t)

	user := newBrowser(t)
	user.register("password_changer")
	user.changePassword("correct-horse-battery", "a-brand-new-password", "a-brand-new-password")

	// The browser that made the change stays signed in.
	if !user.loggedIn() {
		t.Error("changing a password signed out the browser that changed it")
	}

	// The old password no longer works and the new one does.
	returning := newBrowser(t)
	resp := returning.post("/login", "/login", url.Values{
		"username": {"password_changer"},
		"password": {"correct-horse-battery"},
	})
	resp.Body.Close()
	if returning.loggedIn() {
		t.Error("the old password still works")
	}

	resp = returning.post("/login", "/login", url.Values{
		"username": {"password_changer"},
		"password": {"a-brand-new-password"},
	})
	resp.Body.Close()
	if !returning.loggedIn() {
		t.Error("the new password does not work")
	}
}

// Changing a password is what someone does when they think another person has
// it, so it has to remove that person rather than leave their session working.
func TestChangingPasswordSignsOutOtherSessions(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("password_owner")

	// A second signed-in session for the same account, standing in for whoever
	// else had the password.
	intruder := newBrowser(t)
	resp := intruder.post("/login", "/login", url.Values{
		"username": {"password_owner"},
		"password": {"correct-horse-battery"},
	})
	resp.Body.Close()
	if !intruder.loggedIn() {
		t.Fatal("the second session was never established")
	}

	owner.changePassword("correct-horse-battery", "a-brand-new-password", "a-brand-new-password")

	if intruder.loggedIn() {
		t.Error("the other session survived the password change")
	}
	if !owner.loggedIn() {
		t.Error("the browser that changed the password was signed out too")
	}
}

// A stolen session must not be enough to take the account over outright.
func TestChangePasswordRequiresTheCurrentOne(t *testing.T) {
	requireMongo(t)

	user := newBrowser(t)
	user.register("password_guard")
	user.changePassword("not-the-password", "a-brand-new-password", "a-brand-new-password")

	// The original password still works, so nothing changed.
	returning := newBrowser(t)
	resp := returning.post("/login", "/login", url.Values{
		"username": {"password_guard"},
		"password": {"correct-horse-battery"},
	})
	resp.Body.Close()
	if !returning.loggedIn() {
		t.Error("the password was changed without the current one being given")
	}
}

func TestChangePasswordRejectsBadInput(t *testing.T) {
	requireMongo(t)

	// A distinct account per case: requireMongo clears the database once for the
	// parent, so subtests sharing a name would collide on the second
	// registration.
	cases := map[string]struct{ account, next, confirm string }{
		"mismatched confirmation": {"password_mismatch", "a-brand-new-password", "a-different-password"},
		"too short":               {"password_short", "short", "short"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			user := newBrowser(t)
			user.register(tc.account)
			user.changePassword("correct-horse-battery", tc.next, tc.confirm)

			// The original still works.
			returning := newBrowser(t)
			resp := returning.post("/login", "/login", url.Values{
				"username": {tc.account},
				"password": {"correct-horse-battery"},
			})
			resp.Body.Close()
			if !returning.loggedIn() {
				t.Errorf("%s was accepted", name)
			}
		})
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

	resp := leaver.post("/account", "/account?_method=DELETE", url.Values{
		"password": {"correct-horse-battery"},
	})
	resp.Body.Close()

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

	// The name is gone from the site, and the account with it.
	body, _ := newBrowser(t).get("/restaurants/" + restaurantID)
	if strings.Contains(body, "account_leaver") {
		t.Error("the deleted account's name still appears on the page")
	}
	if _, err := models.AuthenticateUser(ctx, "account_leaver", "correct-horse-battery"); err == nil {
		t.Error("the deleted account can still log in")
	}
}

// The placeholder has to be a name nobody can register, or someone could take it
// and appear to have written every deleted account's content.
func TestDeletedUsernameCannotBeRegistered(t *testing.T) {
	requireMongo(t)

	b := newBrowser(t)
	resp := b.post("/register", "/register", url.Values{
		"username": {models.DeletedUsername},
		"password": {"correct-horse-battery"},
	})
	defer resp.Body.Close()

	if resp.Request.URL.Path != "/register" {
		t.Errorf("the placeholder name was accepted as a username, landing on %s", resp.Request.URL.Path)
	}
}

// Deleting is irreversible, so a stolen session must not be enough to do it.
func TestDeleteAccountRequiresThePassword(t *testing.T) {
	requireMongo(t)

	user := newBrowser(t)
	user.register("account_keeper")

	resp := user.post("/account", "/account?_method=DELETE", url.Values{
		"password": {"not-the-password"},
	})
	resp.Body.Close()

	if !user.loggedIn() {
		t.Error("a failed deletion signed the user out")
	}
	if _, err := models.AuthenticateUser(context.Background(), "account_keeper", "correct-horse-battery"); err != nil {
		t.Errorf("the account was deleted without the password: %v", err)
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
