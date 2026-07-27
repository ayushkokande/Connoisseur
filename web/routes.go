package web

import (
	"net/http"
	"net/netip"

	"github.com/gorilla/csrf"
)

// Config describes how the application handler is assembled.
type Config struct {
	// PublicDir is the directory static assets are served from.
	PublicDir string
	// CSRFSecret keys the CSRF tokens.
	CSRFSecret string
	// SecureCookies must be false when serving plain HTTP locally, otherwise
	// the CSRF cookie is never sent back by the browser.
	SecureCookies bool
	// AuthRateLimit throttles login and registration per client address. The
	// zero value applies DefaultAuthRateLimit.
	AuthRateLimit RateLimit
	// WriteRateLimit throttles creating restaurants and reviews per client
	// address. The zero value applies DefaultWriteRateLimit.
	WriteRateLimit RateLimit
	// OAuth is the provider people sign in through. Without credentials, sign-in
	// reports itself unavailable rather than half-working.
	OAuth OAuthConfig
	// TrustedProxies are the networks whose X-Forwarded-For header is believed
	// when working out which client a request came from. Leave it empty when
	// this server is reached directly. Setting it wrongly is not cosmetic:
	// behind an unconfigured proxy every visitor counts against one shared rate
	// limit, and trusting a network that is not a proxy lets anything on it
	// claim any address it likes.
	TrustedProxies []netip.Prefix
}

// Routes builds the application handler, including CSRF protection, method
// override, auth throttling and static files.
func Routes(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// The flow is throttled, not the page that links into it: serving the
	// sign-in page costs nothing and is not how anything is attacked.
	authLimit := newRateLimiter(cfg.AuthRateLimit.orDefault(DefaultAuthRateLimit), cfg.TrustedProxies)

	// Creating content is throttled separately and far more loosely. Nothing is
	// being guessed here, so the limit only has to be tight enough to stop a
	// script filling the listing while leaving a person adding a few
	// restaurants in a sitting unaffected.
	write := newRateLimiter(cfg.WriteRateLimit.orDefault(DefaultWriteRateLimit), cfg.TrustedProxies)

	// Auth. Signing in is the provider's business; what is kept here is the
	// session, the username someone picks on their first visit, and the way out.
	signIn := &auth{oauth: cfg.OAuth}
	mux.HandleFunc("GET /{$}", landing)
	mux.HandleFunc("GET /login", loginForm)
	mux.HandleFunc("GET /auth/start", authLimit.protect(signIn.signInStart))
	mux.HandleFunc("GET /auth/callback", authLimit.protect(signIn.signInCallback))
	mux.HandleFunc("GET /signup", signIn.signUpForm)
	mux.HandleFunc("POST /signup", authLimit.protect(signIn.signUpCreate))
	mux.HandleFunc("POST /logout", logout)

	// Account management.
	mux.HandleFunc("GET /account", isLoggedIn(accountForm))
	mux.HandleFunc("POST /account/sessions", authLimit.protect(isLoggedIn(accountSignOutEverywhere)))
	mux.HandleFunc("DELETE /account", authLimit.protect(isLoggedIn(accountDelete)))

	// Restaurants
	mux.HandleFunc("GET /restaurants", restaurantsIndex)
	mux.HandleFunc("POST /restaurants", write.protect(isLoggedIn(restaurantsCreate)))
	mux.HandleFunc("GET /restaurants/new", isLoggedIn(restaurantsNewForm))
	mux.HandleFunc("GET /restaurants/{id}", restaurantsShow)
	mux.HandleFunc("GET /restaurants/{id}/edit", checkRestaurantOwnership(restaurantsEditForm))
	mux.HandleFunc("PUT /restaurants/{id}", checkRestaurantOwnership(restaurantsUpdate))
	mux.HandleFunc("DELETE /restaurants/{id}", checkRestaurantOwnership(restaurantsDelete))

	// Comments (nested under a restaurant)
	mux.HandleFunc("GET /restaurants/{id}/comments/new", isLoggedIn(commentsNewForm))
	mux.HandleFunc("POST /restaurants/{id}/comments", write.protect(isLoggedIn(commentsCreate)))
	mux.HandleFunc("GET /restaurants/{id}/comments/{comment_id}/edit", checkCommentOwnership(commentsEditForm))
	mux.HandleFunc("PUT /restaurants/{id}/comments/{comment_id}", checkCommentOwnership(commentsUpdate))
	mux.HandleFunc("DELETE /restaurants/{id}/comments/{comment_id}", checkCommentOwnership(commentsDelete))

	// Static assets
	mux.Handle("GET /stylesheets/", http.FileServer(http.Dir(cfg.PublicDir)))

	// 404 for everything else
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		RenderError(w, r, http.StatusNotFound, "That page does not exist.")
	})

	// MethodOverride runs inside CSRF protection: the override turns a POST into
	// a PUT/DELETE, and every one of those is a state-changing method that CSRF
	// checks anyway, so the token is required either way.
	protect := csrf.Protect(
		[]byte(cfg.CSRFSecret),
		csrf.Secure(cfg.SecureCookies),
		csrf.Path("/"),
		csrf.SameSite(csrf.SameSiteLaxMode),
		csrf.ErrorHandler(http.HandlerFunc(csrfFailed)),
	)

	var handler http.Handler = protect(MethodOverride(withCurrentUser(mux)))
	if !cfg.SecureCookies {
		handler = markPlaintext(handler)
	}

	// The health check sits outside CSRF protection so that a probe polling it
	// every few seconds is not handed a fresh CSRF cookie each time.
	root := http.NewServeMux()
	root.HandleFunc("GET "+healthPath, healthz)
	root.Handle("/", handler)

	// SecurityHeaders wraps everything, so the static files and the responses
	// produced by CSRF rejections and 404s are covered too.
	return RequestLogger(cfg.TrustedProxies, SecurityHeaders(cfg.SecureCookies, root))
}

// markPlaintext tells gorilla/csrf that requests arrive over plain HTTP. Without
// it the library assumes HTTPS and rejects the http:// Origin header the browser
// sends, so every form submission fails in local development.
func markPlaintext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, csrf.PlaintextHTTPRequest(r))
	})
}

// csrfFailed replaces the default bare 403 with a flash + redirect, which is
// what a user hitting a stale form actually needs.
func csrfFailed(w http.ResponseWriter, r *http.Request) {
	logger(r).Warn("csrf rejected",
		"method", r.Method,
		"path", r.URL.Path,
		"reason", csrf.FailureReason(r),
	)
	flash(w, r, "error", "Your session expired or the form was invalid. Please try again.")
	redirectBack(w, r)
}
