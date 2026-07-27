package web

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeProvider stands in for the identity provider, speaking enough of the
// protocol for the real sign-in code to run against it unchanged: an
// authorisation endpoint that redirects back with a code, a token endpoint that
// exchanges it, and a userinfo endpoint that says who signed in.
//
// Using one of these rather than stubbing the handlers means the state
// parameter, the PKCE exchange and the callback are all exercised for real.
type fakeProvider struct {
	server *httptest.Server

	mu sync.Mutex
	// subject is who the next sign-in will be. Tests set it before signing in;
	// the provider has no other way to tell one browser from another.
	subject string
	email   string
	// tokens maps an issued access token to the subject it was issued for, so
	// userinfo answers for the right person even across interleaved sign-ins.
	tokens map[string]identityClaims
	// challenges maps an authorisation code to the PKCE challenge it was issued
	// against, so redeeming it requires the verifier that produced it.
	challenges map[string]string
	// skipPKCE models a provider that does not implement PKCE. A test sets it to
	// isolate a protection that PKCE would otherwise mask.
	skipPKCE bool
	// denyWith, when set, makes the authorisation endpoint refuse.
	denyWith string
	// issued counts authorisation codes handed out.
	issued int
}

type identityClaims struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	p := &fakeProvider{
		tokens:     map[string]identityClaims{},
		challenges: map[string]string{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth", p.authorize)
	mux.HandleFunc("/token", p.token)
	mux.HandleFunc("/userinfo", p.userinfo)

	p.server = httptest.NewServer(mux)
	t.Cleanup(p.server.Close)
	return p
}

// config returns the provider configuration pointing at this stand-in.
func (p *fakeProvider) config(clientID, redirectURL string) OAuthConfig {
	return OAuthConfig{
		ProviderName: "test",
		ClientID:     clientID,
		ClientSecret: "test-client-secret",
		RedirectURL:  redirectURL,
		AuthURL:      p.server.URL + "/auth",
		TokenURL:     p.server.URL + "/token",
		UserInfoURL:  p.server.URL + "/userinfo",
		Scopes:       []string{"openid", "email"},
	}
}

// signInAs sets who the next sign-in will be.
func (p *fakeProvider) signInAs(subject, email string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.subject, p.email = subject, email
}

// withoutPKCE runs fn against a provider that does not implement PKCE, so a
// protection PKCE would otherwise mask can be tested on its own.
func (p *fakeProvider) withoutPKCE(t *testing.T, fn func()) {
	t.Helper()
	p.mu.Lock()
	p.skipPKCE = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.skipPKCE = false
		p.mu.Unlock()
	}()
	fn()
}

// deny makes the next authorisation attempt come back refused, the way it does
// when someone presses cancel.
func (p *fakeProvider) deny(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.denyWith = reason
}

func (p *fakeProvider) authorize(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil {
		http.Error(w, "bad redirect_uri", http.StatusBadRequest)
		return
	}

	p.mu.Lock()
	deny := p.denyWith
	p.denyWith = ""
	subject, email := p.subject, p.email
	p.issued++
	code := "code-" + subject
	if deny == "" {
		p.tokens["token-"+subject] = identityClaims{Subject: subject, Email: email}
		p.challenges[code] = query.Get("code_challenge")
	}
	p.mu.Unlock()

	back := redirect.Query()
	back.Set("state", query.Get("state"))
	if deny != "" {
		back.Set("error", deny)
	} else {
		back.Set("code", code)
	}
	redirect.RawQuery = back.Encode()

	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

func (p *fakeProvider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	code := r.PostFormValue("code")
	subject, ok := strings.CutPrefix(code, "code-")
	if !ok {
		http.Error(w, "unknown code", http.StatusBadRequest)
		return
	}

	p.mu.Lock()
	challenge, issued := p.challenges[code]
	skip := p.skipPKCE
	p.mu.Unlock()
	if !issued {
		http.Error(w, "unknown code", http.StatusBadRequest)
		return
	}

	// A code is redeemable only by whoever asked for it. Verifying this properly,
	// rather than checking the verifier is merely present, is what makes an
	// intercepted code useless on its own.
	if !skip {
		verifier := r.PostFormValue("code_verifier")
		if verifier == "" {
			http.Error(w, "missing code_verifier", http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256([]byte(verifier))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			http.Error(w, "code_verifier does not match the challenge", http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "token-" + subject,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}

func (p *fakeProvider) userinfo(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		http.Error(w, "no bearer token", http.StatusUnauthorized)
		return
	}

	p.mu.Lock()
	claims, known := p.tokens[token]
	p.mu.Unlock()
	if !known {
		http.Error(w, "unknown token", http.StatusUnauthorized)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(claims)
}
