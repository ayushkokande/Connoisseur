package web

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"

	"github.com/ayushkokande/Connoisseur/internal/models"
)

// OAuthConfig describes the provider people sign in through. The endpoints are
// fields rather than constants so a test can point them at a stand-in provider
// and exercise the real flow.
type OAuthConfig struct {
	// ProviderName labels the identities created, and is stored on the account.
	// Changing it orphans every account already signed up under the old one.
	ProviderName string
	ClientID     string
	ClientSecret string
	// RedirectURL must match what the provider has registered, exactly.
	RedirectURL string
	AuthURL     string
	TokenURL    string
	// UserInfoURL returns the signed-in identity. Reading it is a second call
	// rather than a decode of the ID token: the answer comes straight from the
	// provider over the back channel, so it needs no signature checking, and
	// there is no JWT verification to get subtly wrong.
	UserInfoURL string
	Scopes      []string
}

// GoogleOAuth returns the provider configuration for Google, given credentials.
func GoogleOAuth(clientID, clientSecret, redirectURL string) OAuthConfig {
	return OAuthConfig{
		ProviderName: "google",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthURL:      "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		UserInfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		// Only what is needed to tell one person from another. Asking for more
		// would mean holding more.
		Scopes: []string{"openid", "email"},
	}
}

// Configured reports whether sign-in can work at all.
func (c OAuthConfig) Configured() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURL != ""
}

func (c OAuthConfig) oauth2Config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Scopes:       c.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  c.AuthURL,
			TokenURL: c.TokenURL,
		},
	}
}

// Session keys holding the half-finished sign-in. They are cleared as soon as
// the flow ends, successfully or not.
const (
	oauthStateKey     = "oauthState"
	oauthVerifierKey  = "oauthVerifier"
	pendingSubjectKey = "pendingSubject"
	pendingEmailKey   = "pendingEmail"
)

// auth carries the provider configuration into the sign-in handlers.
type auth struct {
	oauth OAuthConfig
}

// signInStart sends the visitor to the provider.
func (a *auth) signInStart(w http.ResponseWriter, r *http.Request) {
	if !a.oauth.Configured() {
		logger(r).Error("sign-in attempted with no provider configured")
		RenderError(w, r, http.StatusInternalServerError, "Signing in is not available right now.")
		return
	}

	// state ties the callback to this browser's session. Without it anyone could
	// feed a victim a callback URL carrying their own authorisation code and
	// have the victim signed in as them.
	state, err := randomToken()
	if err != nil {
		logger(r).Error("generating sign-in state", "error", err)
		RenderError(w, r, http.StatusInternalServerError, "Signing in is not available right now.")
		return
	}
	// The verifier proves the code is being redeemed by whoever asked for it,
	// so an intercepted code is not enough on its own.
	verifier := oauth2.GenerateVerifier()

	session := getSession(r)
	session.Values[oauthStateKey] = state
	session.Values[oauthVerifierKey] = verifier
	saveSession(w, r, session)

	http.Redirect(w, r, a.oauth.oauth2Config().AuthCodeURL(state,
		oauth2.AccessTypeOnline,
		oauth2.S256ChallengeOption(verifier),
	), http.StatusFound)
}

// signInCallback completes the flow the provider redirected back from.
func (a *auth) signInCallback(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	state, _ := session.Values[oauthStateKey].(string)
	verifier, _ := session.Values[oauthVerifierKey].(string)

	// Whatever happens next, this attempt is spent.
	delete(session.Values, oauthStateKey)
	delete(session.Values, oauthVerifierKey)
	saveSession(w, r, session)

	if state == "" || r.URL.Query().Get("state") != state {
		logger(r).Warn("sign-in callback with a state that does not match this session")
		flash(w, r, "error", "That sign-in link did not come from here. Please try again.")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if reason := r.URL.Query().Get("error"); reason != "" {
		logger(r).Info("sign-in declined at the provider", "reason", reason)
		flash(w, r, "error", "Sign-in was cancelled.")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	identity, err := a.exchange(r.Context(), r.URL.Query().Get("code"), verifier)
	if err != nil {
		logger(r).Error("completing sign-in", "error", err)
		flash(w, r, "error", "Something went wrong signing you in. Please try again.")
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	user, err := models.FindUserByIdentity(r.Context(), identity)
	switch {
	case err == nil:
		logIn(w, r, user)
		http.Redirect(w, r, "/restaurants", http.StatusFound)

	case errors.Is(err, models.ErrNoSuchUser):
		// First time here. The provider does not supply a name anyone would want
		// shown against their reviews, so one is asked for. The identity is held
		// in the session, never in the form, so the browser cannot claim to be
		// somebody else on the way back.
		session := getSession(r)
		session.Values[pendingSubjectKey] = identity.Subject
		session.Values[pendingEmailKey] = identity.Email
		saveSession(w, r, session)
		http.Redirect(w, r, "/signup", http.StatusFound)

	default:
		logger(r).Error("looking up identity", "error", err)
		flash(w, r, "error", "Something went wrong signing you in. Please try again.")
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

// exchange redeems the authorisation code and asks the provider who it belongs
// to.
func (a *auth) exchange(ctx context.Context, code, verifier string) (models.Identity, error) {
	if code == "" {
		return models.Identity{}, fmt.Errorf("the provider returned no authorisation code")
	}

	token, err := a.oauth.oauth2Config().Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return models.Identity{}, fmt.Errorf("exchanging the authorisation code: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.oauth.UserInfoURL, nil)
	if err != nil {
		return models.Identity{}, err
	}
	token.SetAuthHeader(req)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return models.Identity{}, fmt.Errorf("reading the signed-in identity: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return models.Identity{}, fmt.Errorf("the provider answered %d for the signed-in identity", resp.StatusCode)
	}

	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&claims); err != nil {
		return models.Identity{}, fmt.Errorf("decoding the signed-in identity: %w", err)
	}
	if claims.Subject == "" {
		return models.Identity{}, fmt.Errorf("the provider returned no subject to identify the account by")
	}

	return models.Identity{
		Provider: a.oauth.ProviderName,
		Subject:  claims.Subject,
		Email:    claims.Email,
	}, nil
}

// randomToken returns an unguessable value for the state parameter.
func randomToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
