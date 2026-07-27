package web

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

// These are integration tests: they need a MongoDB reachable at TEST_DATABASE_URL
// (default mongodb://localhost:27017) and they wipe the connoisseur_test database.
// They skip, rather than fail, when no MongoDB is available.

var (
	server         *httptest.Server
	provider       *fakeProvider
	mongoAvailable bool
	testDB         *mongo.Database
)

// nextSubject hands out a fresh provider subject, so every account created by
// the suite is a different person as far as sign-in is concerned.
var nextSubject atomic.Int64

func newSubject() string {
	return "subject-" + strconv.FormatInt(nextSubject.Add(1), 10)
}

// startServer builds a test server whose OAuth redirect URL points back at
// itself. The listener has to exist before the handler can be built, since the
// redirect URL contains the address the provider will send the browser to.
func startServer(t testing.TB, provider *fakeProvider, cfg Config) *httptest.Server {
	srv := httptest.NewUnstartedServer(nil)
	base := "http://" + srv.Listener.Addr().String()
	cfg.OAuth = provider.config("test-client-id", base+"/auth/callback")
	srv.Config.Handler = Routes(cfg)
	srv.Start()
	if t != nil {
		t.Cleanup(srv.Close)
	}
	return srv
}

func TestMain(m *testing.M) {
	// Request logging would otherwise interleave a line per request into the
	// test output; failures report what they need themselves.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	uri := os.Getenv("TEST_DATABASE_URL")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err == nil && client.Ping(ctx, nil) == nil {
		mongoAvailable = true
		testDB = client.Database("connoisseur_test")
		if err := models.Init(testDB); err != nil {
			panic(err)
		}
		// Migrate installs the index enforcing one review per author, so without
		// it these tests would run against a laxer database than production and
		// would not notice that rule breaking.
		if err := models.Migrate(ctx); err != nil {
			panic(err)
		}
		InitSessions("test-session-secret", false)
		if err := InitTemplates("../../templates"); err != nil {
			panic(err)
		}
		provider = &fakeProvider{
			tokens:     map[string]identityClaims{},
			challenges: map[string]string{},
		}
		providerMux := http.NewServeMux()
		providerMux.HandleFunc("/auth", provider.authorize)
		providerMux.HandleFunc("/token", provider.token)
		providerMux.HandleFunc("/userinfo", provider.userinfo)
		provider.server = httptest.NewServer(providerMux)

		server = startServer(nil, provider, Config{
			PublicDir:     "../../public",
			CSRFSecret:    "test-csrf-secret-32-bytes-long!!!",
			SecureCookies: false,
			// The suite signs in dozens of times from one address, so the shared
			// server gets a limit far looser than production's. The real limit is
			// exercised by tests that build a server of their own.
			AuthRateLimit:  RateLimit{Every: time.Millisecond, Burst: 100000},
			WriteRateLimit: RateLimit{Every: time.Millisecond, Burst: 100000},
		})
	}

	code := m.Run()

	if server != nil {
		server.Close()
	}
	if client != nil {
		_ = client.Disconnect(context.Background())
	}
	os.Exit(code)
}

func requireMongo(t *testing.T) {
	t.Helper()
	if !mongoAvailable {
		t.Skip("no MongoDB available; set TEST_DATABASE_URL to run integration tests")
	}
	// Each test starts from an empty database so ownership checks are unambiguous.
	ctx := context.Background()
	if _, err := testDB.Collection("users").DeleteMany(ctx, map[string]any{}); err != nil {
		t.Fatalf("clearing users: %v", err)
	}
	// Restaurants and comments go through the model layer rather than straight
	// to the collection, so that clearing them also drops the cached cuisine
	// menu the way deleting them through the application does.
	if err := models.DeleteAllRestaurants(ctx); err != nil {
		t.Fatalf("clearing restaurants: %v", err)
	}
	if err := models.DeleteAllComments(ctx); err != nil {
		t.Fatalf("clearing comments: %v", err)
	}
}

var csrfPattern = regexp.MustCompile(`name="gorilla\.csrf\.Token" value="([^"]+)"`)

// browser is an HTTP client with a cookie jar, standing in for one logged-in
// user of the server at base.
type browser struct {
	t      *testing.T
	client *http.Client
	base   string
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	return newBrowserAt(t, server.URL)
}

// newBrowserAt points a browser at a server other than the shared one, for
// tests that need a differently configured handler.
func newBrowserAt(t *testing.T, base string) *browser {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &browser{t: t, client: &http.Client{Jar: jar}, base: base}
}

// get fetches a page and returns its body and the CSRF token embedded in it.
func (b *browser) get(path string) (string, string) {
	b.t.Helper()
	resp, err := b.client.Get(b.base + path)
	if err != nil {
		b.t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body := readAll(b.t, resp)
	token := ""
	if m := csrfPattern.FindStringSubmatch(body); m != nil {
		token = m[1]
	}
	return body, token
}

// noRedirects returns a browser sharing this one's session that reports
// redirects instead of following them, so a test can assert where a handler
// sends the user rather than only where they end up.
func (b *browser) noRedirects() *browser {
	b.t.Helper()
	return &browser{t: b.t, base: b.base, client: &http.Client{
		Jar:           b.client.Jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// getResponse fetches a page and returns the response itself, for tests
// interested in the status or headers rather than the body.
func (b *browser) getResponse(path string) *http.Response {
	b.t.Helper()
	resp, err := b.client.Get(b.base + path)
	if err != nil {
		b.t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// post submits a form, taking a CSRF token from tokenPage. It follows redirects
// and returns the final response.
func (b *browser) post(tokenPage, action string, form url.Values) *http.Response {
	b.t.Helper()
	_, token := b.get(tokenPage)
	if token == "" {
		b.t.Fatalf("no CSRF token found on %s", tokenPage)
	}
	form.Set("gorilla.csrf.Token", token)
	return b.postRaw(action, form)
}

// postRaw submits a form exactly as given, with no token unless the caller set
// one. It sends the same-origin Referer and Origin headers a real browser sends
// on a form submission, which the CSRF layer also checks.
func (b *browser) postRaw(action string, form url.Values) *http.Response {
	b.t.Helper()
	req, err := http.NewRequest(http.MethodPost, b.base+action, strings.NewReader(form.Encode()))
	if err != nil {
		b.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", b.base+"/")
	req.Header.Set("Origin", b.base)

	resp, err := b.client.Do(req)
	if err != nil {
		b.t.Fatalf("POST %s: %v", action, err)
	}
	return resp
}

// deleteAccount submits the account page's deletion form with the given
// confirmation text.
func (b *browser) deleteAccount(confirmation string) {
	b.t.Helper()
	resp := b.post("/account", "/account?_method=DELETE", url.Values{
		"username": {confirmation},
	})
	resp.Body.Close()
}

// currentSubject returns the provider subject of the account a browser is
// signed in as, read back from the database by the displayed username.
func currentSubject(t *testing.T, b *browser) string {
	t.Helper()
	body, _ := b.get("/account")
	match := regexp.MustCompile(`Signed in as ([A-Za-z0-9_]+)`).FindStringSubmatch(body)
	if match == nil {
		t.Fatal("the browser is not signed in")
	}

	var user models.User
	err := testDB.Collection("users").FindOne(context.Background(),
		bson.M{"usernameLower": strings.ToLower(match[1])}).Decode(&user)
	if err != nil {
		t.Fatalf("loading the signed-in user: %v", err)
	}
	return user.Subject
}

func mustID(t *testing.T, hex string) bson.ObjectID {
	t.Helper()
	id, err := bson.ObjectIDFromHex(hex)
	if err != nil {
		t.Fatalf("not a valid object ID: %q", hex)
	}
	return id
}

// countRestaurants reports how many restaurants exist, regardless of paging.
func countRestaurants(t *testing.T) int64 {
	t.Helper()
	page, err := models.FindRestaurants(context.Background(), models.RestaurantQuery{})
	if err != nil {
		t.Fatal(err)
	}
	return page.Total
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

// signIn takes the browser through the whole sign-in flow as the given subject,
// following the redirects a real browser would. It returns the path it lands on:
// /restaurants for an identity that already has an account, /signup for one that
// does not.
func (b *browser) signIn(subject, email string) string {
	b.t.Helper()
	provider.signInAs(subject, email)

	resp, err := b.client.Get(b.base + "/auth/start")
	if err != nil {
		b.t.Fatalf("starting sign-in: %v", err)
	}
	defer resp.Body.Close()
	_ = readAll(b.t, resp)
	return resp.Request.URL.Path
}

// register creates an account under a fresh identity and leaves the browser
// signed in as it, which is what most tests want a user for.
func (b *browser) register(username string) {
	b.t.Helper()

	if landed := b.signIn(newSubject(), username+"@example.com"); landed != "/signup" {
		b.t.Fatalf("a new identity landed on %s, want /signup", landed)
	}

	resp := b.post("/signup", "/signup", url.Values{"username": {username}})
	defer resp.Body.Close()
	if !strings.HasSuffix(resp.Request.URL.Path, "/restaurants") {
		b.t.Fatalf("choosing the username %q did not land on /restaurants, got %s",
			username, resp.Request.URL.Path)
	}
}

// createRestaurant adds a restaurant owned by the logged-in user and returns its ID.
func (b *browser) createRestaurant(name string) string {
	b.t.Helper()
	resp := b.post("/restaurants/new", "/restaurants", url.Values{
		"name":        {name},
		"image":       {"https://example.com/photo.jpg"},
		"cuisine":     {"Italian"},
		"priceRange":  {"$$"},
		"description": {"A lovely place."},
	})
	defer resp.Body.Close()
	path := resp.Request.URL.Path
	id := strings.TrimPrefix(path, "/restaurants/")
	if id == path || id == "" {
		b.t.Fatalf("creating restaurant did not redirect to a restaurant page, got %s", path)
	}
	return id
}

// createComment adds a review to a restaurant and returns the comment ID.
func (b *browser) createComment(restaurantID, text string) string {
	b.t.Helper()
	return b.createRating(restaurantID, 4, text)
}

// createRating adds a review with an explicit star rating.
func (b *browser) createRating(restaurantID string, rating int, text string) string {
	b.t.Helper()
	resp := b.post("/restaurants/"+restaurantID+"/comments/new",
		"/restaurants/"+restaurantID+"/comments",
		url.Values{"rating": {strconv.Itoa(rating)}, "text": {text}})
	defer resp.Body.Close()

	reviews, err := models.FindCommentsByRestaurant(context.Background(), mustID(b.t, restaurantID))
	if err != nil {
		b.t.Fatal(err)
	}
	if len(reviews) == 0 {
		b.t.Fatalf("review was not attached to restaurant %s", restaurantID)
	}
	return reviews[len(reviews)-1].ID.Hex()
}

func TestOwnerCanEditAndDeleteOwnRestaurant(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("owner_one")
	id := owner.createRestaurant("Owner's Bistro")

	resp := owner.post("/restaurants/"+id+"/edit", "/restaurants/"+id+"?_method=PUT", url.Values{
		"name":        {"Renamed Bistro"},
		"image":       {"https://example.com/new.jpg"},
		"cuisine":     {"French"},
		"priceRange":  {"$$$"},
		"description": {"Now under a new name."},
	})
	resp.Body.Close()

	restaurant, err := models.FindRestaurantByID(context.Background(), mustID(t, id))
	if err != nil {
		t.Fatal(err)
	}
	if restaurant.Name != "Renamed Bistro" {
		t.Errorf("owner's update did not apply: name is %q, want %q", restaurant.Name, "Renamed Bistro")
	}

	resp = owner.post("/restaurants/"+id, "/restaurants/"+id+"?_method=DELETE", url.Values{})
	resp.Body.Close()

	if _, err := models.FindRestaurantByID(context.Background(), mustID(t, id)); err == nil {
		t.Error("owner's delete did not remove the restaurant")
	}
}

func TestNonOwnerCannotEditOrDeleteRestaurant(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("owner_two")
	id := owner.createRestaurant("Protected Bistro")

	intruder := newBrowser(t)
	intruder.register("intruder_two")

	// The edit form itself must be refused.
	body, _ := intruder.get("/restaurants/" + id + "/edit")
	if strings.Contains(body, "Save Changes") {
		t.Error("non-owner was served the restaurant edit form")
	}

	resp := intruder.post("/restaurants/"+id, "/restaurants/"+id+"?_method=PUT", url.Values{
		"name":        {"Hijacked"},
		"image":       {"https://example.com/evil.jpg"},
		"cuisine":     {"Evil"},
		"priceRange":  {"$"},
		"description": {"Taken over."},
	})
	resp.Body.Close()

	restaurant, err := models.FindRestaurantByID(context.Background(), mustID(t, id))
	if err != nil {
		t.Fatalf("restaurant should still exist: %v", err)
	}
	if restaurant.Name != "Protected Bistro" {
		t.Errorf("non-owner edited the restaurant: name is now %q", restaurant.Name)
	}

	resp = intruder.post("/restaurants/"+id, "/restaurants/"+id+"?_method=DELETE", url.Values{})
	resp.Body.Close()

	if _, err := models.FindRestaurantByID(context.Background(), mustID(t, id)); err != nil {
		t.Error("non-owner deleted the restaurant")
	}
}

func TestNonOwnerCannotEditOrDeleteComment(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("owner_three")
	restaurantID := owner.createRestaurant("Review Bistro")
	commentID := owner.createComment(restaurantID, "Original review text.")

	intruder := newBrowser(t)
	intruder.register("intruder_three")

	base := "/restaurants/" + restaurantID + "/comments/" + commentID

	resp := intruder.post("/restaurants/"+restaurantID, base+"?_method=PUT",
		url.Values{"text": {"Vandalised review."}})
	resp.Body.Close()

	comment, err := models.FindCommentByID(context.Background(), mustID(t, commentID))
	if err != nil {
		t.Fatalf("comment should still exist: %v", err)
	}
	if comment.Text != "Original review text." {
		t.Errorf("non-owner edited the comment: text is now %q", comment.Text)
	}

	resp = intruder.post("/restaurants/"+restaurantID, base+"?_method=DELETE", url.Values{})
	resp.Body.Close()

	if _, err := models.FindCommentByID(context.Background(), mustID(t, commentID)); err != nil {
		t.Error("non-owner deleted the comment")
	}
}

// A review belongs to one restaurant, and the URL naming a different one is
// wrong however the visitor got there. Owning the review is not enough: acting
// on it through the wrong restaurant's URL edits the review and then redirects
// to a page it never appeared on, reporting a change the visitor cannot see.
func TestOwnCommentCannotBeEditedThroughAnotherRestaurant(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("owner_mismatch")
	reviewed := owner.createRestaurant("Reviewed Bistro")
	other := owner.createRestaurant("Unrelated Bistro")
	commentID := owner.createComment(reviewed, "Original review text.")

	// Same owner, same review, wrong restaurant in the path.
	mismatched := "/restaurants/" + other + "/comments/" + commentID

	resp := owner.post("/restaurants/"+other, mismatched+"?_method=PUT",
		url.Values{"rating": {"1"}, "text": {"Edited through the wrong URL."}})
	resp.Body.Close()

	comment, err := models.FindCommentByID(context.Background(), mustID(t, commentID))
	if err != nil {
		t.Fatalf("comment should still exist: %v", err)
	}
	if comment.Text != "Original review text." {
		t.Errorf("the review was edited through another restaurant's URL: text is now %q", comment.Text)
	}

	// The edit form is refused for the same reason.
	body, _ := owner.get(mismatched + "/edit")
	if strings.Contains(body, "Edited through the wrong URL.") ||
		strings.Contains(body, "Original review text.") {
		t.Error("the review edit form was served under another restaurant's URL")
	}

	resp = owner.post("/restaurants/"+other, mismatched+"?_method=DELETE", url.Values{})
	resp.Body.Close()

	if _, err := models.FindCommentByID(context.Background(), mustID(t, commentID)); err != nil {
		t.Error("the review was deleted through another restaurant's URL")
	}
}

func TestAnonymousCannotCreateRestaurant(t *testing.T) {
	requireMongo(t)

	// Someone part-way through signing up has a session and a CSRF token but no
	// account yet, which is the only anonymous state that can post at all. The
	// request must still be refused on authentication.
	anon := newBrowser(t)
	if landed := anon.signIn(newSubject(), "halfway@example.com"); landed != "/signup" {
		t.Fatalf("a new identity landed on %s, want /signup", landed)
	}

	resp := anon.post("/signup", "/restaurants", url.Values{
		"name":        {"Ghost Bistro"},
		"image":       {"https://example.com/ghost.jpg"},
		"cuisine":     {"Italian"},
		"priceRange":  {"$$"},
		"description": {"Should never exist."},
	})
	defer resp.Body.Close()

	if resp.Request.URL.Path != "/login" {
		t.Errorf("anonymous create was not redirected to /login, landed on %s", resp.Request.URL.Path)
	}
	if n := countRestaurants(t); n != 0 {
		t.Errorf("anonymous visitor created %d restaurant(s)", n)
	}
}

func TestCSRFTokenIsRequired(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("owner_csrf")
	id := owner.createRestaurant("CSRF Bistro")

	// Same logged-in session, valid ownership, but no CSRF token: a forged
	// cross-site delete looks exactly like this.
	resp := owner.postRaw("/restaurants/"+id+"?_method=DELETE", url.Values{})
	defer resp.Body.Close()

	if _, err := models.FindRestaurantByID(context.Background(), mustID(t, id)); err != nil {
		t.Error("delete without a CSRF token succeeded")
	}
}

func TestRestaurantValidationRejectsBadInput(t *testing.T) {
	requireMongo(t)

	owner := newBrowser(t)
	owner.register("owner_valid")

	cases := []struct {
		name string
		form url.Values
	}{
		{"blank name", url.Values{"name": {"   "}, "image": {"https://example.com/a.jpg"}, "cuisine": {"Thai"}, "priceRange": {"$"}, "description": {"ok"}}},
		{"javascript image URL", url.Values{"name": {"X"}, "image": {"javascript:alert(1)"}, "cuisine": {"Thai"}, "priceRange": {"$"}, "description": {"ok"}}},
		{"bogus price range", url.Values{"name": {"X"}, "image": {"https://example.com/a.jpg"}, "cuisine": {"Thai"}, "priceRange": {"free"}, "description": {"ok"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := owner.post("/restaurants/new", "/restaurants", tc.form)
			resp.Body.Close()

			if countRestaurants(t) != 0 {
				t.Errorf("invalid restaurant (%s) was saved", tc.name)
			}
		})
	}
}

// The username someone picks on their first sign-in is the only thing they
// supply about their account, so its rules have to hold at that step.
func TestSignUpValidatesTheChosenUsername(t *testing.T) {
	requireMongo(t)

	for _, username := range []string{"ab", "bad name!", ""} {
		t.Run(username, func(t *testing.T) {
			b := newBrowser(t)
			if landed := b.signIn(newSubject(), "picky@example.com"); landed != "/signup" {
				t.Fatalf("a new identity landed on %s, want /signup", landed)
			}

			resp := b.post("/signup", "/signup", url.Values{"username": {username}})
			defer resp.Body.Close()

			if resp.Request.URL.Path != "/signup" {
				t.Errorf("username %q was accepted, landing on %s", username, resp.Request.URL.Path)
			}
		})
	}
}

// A name already taken has to be refused however it is capitalised, since
// reviews are attributed by display name.
func TestSignUpRefusesATakenUsername(t *testing.T) {
	requireMongo(t)

	first := newBrowser(t)
	first.register("MixedName")

	for _, variant := range []string{"MixedName", "mixedname", "MIXEDNAME"} {
		t.Run(variant, func(t *testing.T) {
			b := newBrowser(t)
			if landed := b.signIn(newSubject(), "other@example.com"); landed != "/signup" {
				t.Fatalf("a new identity landed on %s, want /signup", landed)
			}

			resp := b.post("/signup", "/signup", url.Values{"username": {variant}})
			defer resp.Body.Close()

			if resp.Request.URL.Path != "/signup" {
				t.Errorf("%q was accepted alongside MixedName, landing on %s", variant, resp.Request.URL.Path)
			}
		})
	}

	// The original still shows the capitalisation it was created with.
	body, _ := first.get("/restaurants")
	if !strings.Contains(body, "MixedName") {
		t.Error("the navbar does not show the capitalisation the account was created with")
	}
}
