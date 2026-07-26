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
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/ayushkokande/Connoisseur/models"
)

// These are integration tests: they need a MongoDB reachable at TEST_DATABASE_URL
// (default mongodb://localhost:27017) and they wipe the connoisseur_test database.
// They skip, rather than fail, when no MongoDB is available.

var (
	server         *httptest.Server
	mongoAvailable bool
	testDB         *mongo.Database
)

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
		if err := InitTemplates("../templates"); err != nil {
			panic(err)
		}
		server = httptest.NewServer(Routes(Config{
			PublicDir:     "../public",
			CSRFSecret:    "test-csrf-secret-32-bytes-long!!!",
			SecureCookies: false,
			// The suite registers and logs in dozens of times from one address,
			// so the shared server gets a limit far looser than production's.
			// The real limit is exercised by TestAuthRateLimitBlocksGuessing,
			// which builds a server of its own.
			AuthRateLimit: RateLimit{Every: time.Millisecond, Burst: 100000},
		}))
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
	for _, name := range []string{"users", "restaurants", "comments"} {
		if _, err := testDB.Collection(name).DeleteMany(ctx, map[string]any{}); err != nil {
			t.Fatalf("clearing %s: %v", name, err)
		}
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

// register creates an account and leaves the browser logged in as that user.
func (b *browser) register(username string) {
	b.t.Helper()
	resp := b.post("/register", "/register", url.Values{
		"username": {username},
		"password": {"correct-horse-battery"},
	})
	defer resp.Body.Close()
	if !strings.HasSuffix(resp.Request.URL.Path, "/restaurants") {
		b.t.Fatalf("register %q did not land on /restaurants, got %s", username, resp.Request.URL.Path)
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

	anon := newBrowser(t)
	// A logged-out visitor gets no /restaurants/new form, so take the token from
	// the login page: the request must still be rejected on authentication.
	resp := anon.post("/login", "/restaurants", url.Values{
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

func TestRegistrationValidationRejectsWeakCredentials(t *testing.T) {
	requireMongo(t)

	cases := map[string]url.Values{
		"short password":   {"username": {"validname"}, "password": {"short"}},
		"short username":   {"username": {"ab"}, "password": {"correct-horse-battery"}},
		"illegal username": {"username": {"bad name!"}, "password": {"correct-horse-battery"}},
	}

	for name, form := range cases {
		t.Run(name, func(t *testing.T) {
			b := newBrowser(t)
			resp := b.post("/register", "/register", form)
			defer resp.Body.Close()

			if resp.Request.URL.Path != "/register" {
				t.Errorf("weak credentials (%s) were accepted, landed on %s", name, resp.Request.URL.Path)
			}
		})
	}
}

func TestDuplicateUsernameIsRejected(t *testing.T) {
	requireMongo(t)

	first := newBrowser(t)
	first.register("taken_name")

	second := newBrowser(t)
	resp := second.post("/register", "/register", url.Values{
		"username": {"taken_name"},
		"password": {"another-good-password"},
	})
	defer resp.Body.Close()

	if resp.Request.URL.Path != "/register" {
		t.Errorf("duplicate username was accepted, landed on %s", resp.Request.URL.Path)
	}
}
