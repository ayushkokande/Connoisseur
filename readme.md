# Connoisseur

> A restaurant review web application built with Go and MongoDB. Discover, review, and share the best restaurants around you.

## Features

* Authentication:

  * Sign in with a Google account; there is no password to store or lose

  * A username is chosen once, on first sign-in, and is what appears against
    your restaurants and reviews

* Authorization:

  * Users cannot manage restaurants or reviews without being authenticated

  * Users cannot edit or delete restaurants or reviews created by other users

* Manage restaurant listings with full CRUD functionality:

  * Browse, create, edit, and delete restaurants and reviews

  * Restaurant details include cuisine type, price range, and photos

  * Reviews carry a one to five star rating, and each restaurant shows its
    average and review count

  * One review per person per restaurant, editable afterwards, so a listing
    cannot be pushed up the rankings by the same person reviewing repeatedly

* Browse the directory:

  * Free text search across name, cuisine and description

  * Filter by cuisine, price range and minimum rating; sort by newest, oldest,
    name or top rated

  * Paginated results with shareable, filter-preserving URLs, and paginated
    reviews on each restaurant

* Account management: sign out of every session at once, or delete your account
  and leave what you wrote credited to `[deleted_user]`

* Flash messages responding to user interactions

* Responsive web design with Bootstrap

## Getting Started

### Prerequisites

* [Go](https://go.dev/) 1.25 or later
* [MongoDB](https://www.mongodb.com/) running locally, or a connection string to a hosted instance

Alternatively, [Docker](https://www.docker.com/) alone is enough — see
[Run with Docker](#run-with-docker).

### Clone this repository

```sh
git clone https://github.com/ayushkokande/Connoisseur.git
cd Connoisseur
```

### Run the app

```sh
go run .
```

The app runs at [http://localhost:3000](http://localhost:3000) and connects to `mongodb://localhost:27017` (database `connoisseur`) by default.

### Run with Docker

`docker-compose.yml` brings up the app together with its own MongoDB, so nothing
needs to be installed locally:

```sh
docker compose up --build
```

Add `SEED_DB=true` to start from the sample restaurants:

```sh
SEED_DB=true docker compose up --build
```

MongoDB's port is deliberately not published, so the stack will not collide with
a MongoDB already running on the host. Data persists in the `mongo-data` volume;
`docker compose down -v` discards it.

### Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `DATABASE_URL` | MongoDB connection string | `mongodb://localhost:27017` |
| `DATABASE_NAME` | MongoDB database name | `connoisseur` |
| `SESSION_SECRET` | Key used to sign session cookies | random per run in development; **required** in production |
| `CSRF_SECRET` | Key used to sign CSRF tokens | random per run in development; **required** in production |
| `APP_ENV` | Set to `production` to require the secrets above, mark cookies `Secure` and emit JSON logs | unset |
| `LOG_LEVEL` | `debug`, `info`, `warn` or `error` | `info` |
| `PORT` | Port the server listens on | `3000` |
| `SEED_DB` | Set to `true` to reset and seed the database on startup | unset |
| `GOOGLE_CLIENT_ID` | OAuth client ID; without it nobody can sign in | unset |
| `GOOGLE_CLIENT_SECRET` | OAuth client secret | unset |
| `OAUTH_REDIRECT_URL` | Where Google sends the browser back | `http://localhost:$PORT/auth/callback` |
| `TRUSTED_PROXIES` | Comma-separated CIDR blocks or addresses whose `X-Forwarded-For` is believed | unset |

In development the two secrets are generated randomly at startup, which logs
everyone out on restart but means no known key is ever baked into the source. In
production the server refuses to start without them.

`APP_ENV=production` also marks the session and CSRF cookies `Secure`, so they
are only sent over HTTPS — do not set it when serving plain HTTP or logging in
will silently fail.

#### Setting up sign-in

Sign-in needs a Google OAuth client. In the
[Google Cloud console](https://console.cloud.google.com/apis/credentials),
create an **OAuth client ID** of type *Web application* and add the callback as
an authorised redirect URI — exactly, including the scheme and port:

```
http://localhost:3000/auth/callback
```

Then:

```sh
export GOOGLE_CLIENT_ID=...
export GOOGLE_CLIENT_SECRET=...
go run .
```

Without them the site runs and can be browsed, but signing in reports itself
unavailable rather than half-working. Only `openid` and `email` are requested,
which is the least that distinguishes one person from another.

#### Running behind a reverse proxy

Rate limits are counted per client address. A request that arrives through a
proxy carries the *proxy's* address, so if the app sits behind nginx, a load
balancer or a platform router and `TRUSTED_PROXIES` is left unset, **every
visitor shares one rate-limit budget** — a handful of failed logins from one
person throttles everybody.

Set it to the network the proxy connects from:

```sh
TRUSTED_PROXIES=10.0.0.0/8,172.16.0.0/12
```

`X-Forwarded-For` is then read, but only for connections that actually come from
one of those networks, and only back as far as the last hop that is not itself
trusted. Anything a client prepended to the header before that point is ignored.
Leave the variable unset when the app is reached directly: believing the header
unconditionally would let anyone hand themselves a fresh budget on every request
just by varying it.

Do not list a network that is not a proxy — anything on it could then claim to
be any address it liked.

### Run the tests

```sh
go test ./...
```

The handler tests are integration tests: they need a MongoDB reachable at
`TEST_DATABASE_URL` (default `mongodb://localhost:27017`) and they wipe the
`connoisseur_test` database. They **skip rather than fail** when no MongoDB is
available, so a green run without one has not exercised them. If you do not have
MongoDB installed, start a throwaway instance first:

```sh
docker run -d --name connoisseur-test-mongo -p 27017:27017 mongo:8
```

GitHub Actions runs `gofmt`, `go vet`, `go test -race` against a MongoDB service
container, checks total coverage against a floor, and builds the Docker image on
every push and pull request. See
[`.github/workflows/ci.yml`](.github/workflows/ci.yml).

## Operations

### Health checks

`GET /healthz` pings MongoDB and answers with JSON:

```json
{"status":"ok","database":"ok"}
```

It returns `200` while the database is reachable and `503` when it is not, so a
load balancer or orchestrator can pull the instance out of rotation. The
endpoint sits outside CSRF protection, so probes are not handed a cookie on
every poll. The Docker image wires it into a `HEALTHCHECK`.

### Logging

Logs are structured via `log/slog` — human-readable text in development, JSON
when `APP_ENV=production`:

```
time=2026-07-27T09:32:23.671Z level=INFO msg=request request_id=6263cf74 method=GET path=/restaurants status=200 bytes=4085 duration_ms=1 client_ip=203.0.113.7
```

Every request is assigned an ID that is returned in the `X-Request-Id` response
header and attached to each log line emitted while handling it, so a
user-reported failure can be traced back to its logs. Successful health checks
are not logged, to keep frequent probes from burying everything else.

`client_ip` is the visitor rather than the connection, so behind a proxy it means
something. It is resolved from the same `TRUSTED_PROXIES` setting the rate limits
use, so a throttling warning can be matched against the requests around it.

## Security

* Sign-in is delegated to Google, so this application never sees, stores or can
  leak a password.
* The OAuth flow carries a `state` parameter tied to the browser's session and a
  PKCE challenge. The first stops someone completing a sign-in of their own and
  handing the callback URL to a victim, who would otherwise end up signed in as
  them; the second stops an intercepted authorisation code being redeemed by
  anyone but whoever asked for it. Both are checked, and each is tested with the
  other stood down so neither hides a gap in the other.
* An account is identified by the provider's own subject, never by email
  address. A provider that let an address be reassigned would otherwise hand
  over somebody else's account.
* All state-changing requests require a CSRF token, submitted as a hidden field
  in every form.
* Session cookies are encrypted as well as signed, so their contents are not
  readable by anything holding the cookie. The signing and encryption keys are
  derived from `SESSION_SECRET` with HKDF, which keeps them independent and
  accepts a secret of any length.
* Session cookies are `HttpOnly`, `SameSite=Lax`, `Secure` in production, and
  expire after a week. Logging in clears any prior session state; logging out
  expires the cookie.
* Logout is a POST, so it cannot be triggered by a cross-site link.
* User input is validated in the model layer: username shape and length,
  field lengths, an allowlisted price range, and image URLs
  restricted to absolute `http`/`https` (which rejects `javascript:` and
  `data:` URLs).
* Usernames are unique regardless of case, so `Admin` and `admin` cannot be two
  accounts. Reviews are attributed by display name, so allowing both would let
  one account be mistaken for another.
* Login and registration are rate limited per client address: eight attempts
  back to back, then one every fifteen seconds. Exceeding it returns `429` with
  `Retry-After`. The login form itself is not throttled, so a throttled visitor
  can still come back. See [running behind a reverse proxy](#running-behind-a-reverse-proxy)
  for the configuration this needs when the app is not reached directly.
* Creating restaurants and reviews is rate limited separately and far more
  loosely — twenty back to back, then one every three seconds. Nothing is being
  guessed there, so it only has to stop a script filling the listing. Reading is
  never throttled.
* Signing out everywhere raises a credential version recorded in each session,
  which invalidates every session already handed out — what someone reaches for
  when they think one has been taken.
* Deleting an account asks for the username to be typed back, so something
  irreversible takes more than one button press. The restaurants and reviews it
  wrote stay on the site, credited to `[deleted_user]`, so other people's reviews
  of a restaurant do not disappear with whoever added it. That placeholder
  contains brackets, which usernames may not, so no real account can be
  registered under it.
* Every response carries a content security policy naming exactly the sources
  the templates use, with a per-response nonce for the one inline script.
  Framing, plugins, outbound connections and rewriting the form target are all
  refused. `Referrer-Policy`, `X-Content-Type-Options` and `X-Frame-Options` are
  set too, and HSTS when `APP_ENV=production`.
* A review may only be edited or deleted through the URL of the restaurant it
  belongs to, not merely by its author.
* Pages are rendered into a buffer before anything is written, so a template
  failure becomes a `500` rather than a half-written page under a `200`.

### Error pages

404, 429 and the 500 from a failed render are served as ordinary pages inside the
site layout, carrying the status they mean. The error page falls back to plain
text if it cannot itself be rendered — the commonest caller is a handler whose
own template just failed, so answering one render failure with another would
loop.

The static file handler still writes its own plain-text 404 for a missing
stylesheet, which is not a page anyone browses to.

## Project Structure

```
Connoisseur/
├── main.go             # App entry point: config, logging, DB connection, server startup
├── seeds.go            # Optional database seeder (enabled with SEED_DB=true)
├── Dockerfile          # Multi-stage build producing a static binary image
├── docker-compose.yml  # App plus MongoDB for local development
├── models/
│   ├── db.go           # Collection handles, indexes, health ping
│   ├── query.go        # Restaurant search, filtering, sorting and pagination
│   ├── restaurant.go   # Restaurant model (name, image, cuisine, price range, ...)
│   ├── comment.go      # Review model (text plus a 1-5 star rating)
│   ├── migrate.go      # Idempotent startup migration of pre-existing data
│   ├── user.go         # Accounts, keyed on the provider identity signed in with
│   └── validate.go     # Input validation rules
├── web/
│   ├── routes.go       # Route table, handler config, CSRF protection
│   ├── restaurants.go  # RESTful restaurant handlers
│   ├── comments.go     # Nested review handlers
│   ├── auth.go         # Landing, sign-in page, logout
│   ├── oauth.go        # The provider flow: state, PKCE, callback, identity
│   ├── signup.go       # Choosing a username on a first sign-in
│   ├── account.go      # Signing out everywhere, and account deletion
│   ├── middleware.go   # Auth & ownership middleware, method override
│   ├── ratelimit.go    # Per-client token buckets for login and registration
│   ├── clientip.go     # Client address resolution, trusted-proxy handling
│   ├── headers.go      # Content security policy and other response headers
│   ├── context.go      # Per-request user, restaurant and comment caching
│   ├── pagination.go   # Page links and filter-preserving URLs
│   ├── session.go      # Cookie sessions, current user, flash messages
│   ├── errors.go       # Error-to-flash mapping
│   ├── logging.go      # Request IDs and request logging middleware
│   ├── health.go       # /healthz endpoint
│   └── render.go       # Template parsing and rendering helpers
├── templates/          # html/template views
└── public/             # Static assets (stylesheets)
```

## Routes

### Restaurants

| Name | Path | Verb | Description |
| --- | --- | --- | --- |
| Index | `/restaurants` | GET | Search, filter and page through restaurants |
| New | `/restaurants/new` | GET | Form to add a restaurant * |
| Create | `/restaurants` | POST | Add a new restaurant * |
| Show | `/restaurants/:id` | GET | Details for one restaurant |
| Edit | `/restaurants/:id/edit` | GET | Form to edit a restaurant ** |
| Update | `/restaurants/:id` | PUT | Update a restaurant ** |
| Destroy | `/restaurants/:id` | DELETE | Delete a restaurant and its reviews ** |

### Reviews

| Name | Path | Verb | Description |
| --- | --- | --- | --- |
| New | `/restaurants/:id/comments/new` | GET | Form to add a review; redirects to the visitor's existing review if they have one * |
| Create | `/restaurants/:id/comments` | POST | Add a review * |
| Edit | `/restaurants/:id/comments/:comment_id/edit` | GET | Form to edit a review ** |
| Update | `/restaurants/:id/comments/:comment_id` | PUT | Update a review ** |
| Destroy | `/restaurants/:id/comments/:comment_id` | DELETE | Delete a review ** |

### Auth

| Path | Verb | Description |
| --- | --- | --- |
| `/` | GET | Landing page |
| `/login` | GET | Sign-in page |
| `/logout` | POST | Log out |
| `/auth/start` | GET | Begin sign-in at the provider |
| `/auth/callback` | GET | Where the provider sends the browser back |
| `/signup` | GET / POST | Choose a username, on a first sign-in only |
| `/account` | GET | Account settings * |
| `/account/sessions` | POST | Sign out everywhere * |
| `/account` | DELETE | Delete account * |

### Operations

| Path | Verb | Description |
| --- | --- | --- |
| `/healthz` | GET | Liveness and database reachability check |

\* requires login &nbsp;&nbsp; \** requires login and ownership

The index accepts these query parameters, all optional:

| Parameter | Purpose | Values |
| --- | --- | --- |
| `q` | Free text search over name, cuisine and description | any text, up to 100 characters |
| `cuisine` | Exact cuisine match | any cuisine present in the data |
| `price` | Exact price range match | `$`, `$$`, `$$$`, `$$$$` |
| `rating` | Minimum average rating | `1` to `5` |
| `sort` | Result order | `newest` (default), `rating`, `oldest`, `name` |
| `page` | 1-based page number | defaults to `1`, clamped to the last page |

A restaurant's own page takes `page` too, for its reviews.

Unrecognized values are ignored rather than rejected, so a stale bookmark still
returns results.

Search is answered from a MongoDB text index across name, cuisine and
description, and falls back to a case-insensitive substring scan when the index
matches nothing — so whole words are served from the index while a partial word
like `trat` still finds `Trattoria`. The fallback also covers a database where
the index has not been built, since `$text` refuses to run without it. Terms
reach the index as typed, so a quoted run matches as a phrase and a leading
hyphen excludes a word; a hyphen *inside* a word is not exclusion, because the
tokenizer splits on it. On the substring path the term is escaped before it
reaches MongoDB, so regex metacharacters are matched literally.

HTML forms submit PUT/DELETE via a `_method` override parameter, mirroring the classic method-override pattern. Every non-GET request carries a CSRF token.

## Ratings

Every review carries a rating from one to five stars. A restaurant document
keeps a `reviewCount` and `avgRating` alongside its own fields, which is what
lets the index sort by rating and filter by a minimum without joining to the
reviews on every page load.

Those two fields are derived data, and they are recomputed from the reviews
themselves after each write rather than adjusted in place. Recomputing costs one
extra query per review, and in exchange a write that fails partway leaves the
summary stale rather than permanently wrong: the next review, or a rerun of the
migration, puts it right. Transactions would be the usual answer, but they
require a replica set, and this runs happily against a standalone `mongod`.

Reviews reference their restaurant rather than the restaurant holding an array
of review IDs. Adding a review is therefore a single insert that cannot
half-succeed.

Both of these differ from how the data used to be stored, so `models.Migrate`
runs at startup: it unrolls the old arrays onto the reviews, discards reviews
whose restaurant no longer exists, and computes summaries that were never there.
It also settles the data that later rules made invalid — extra reviews by one
author of one restaurant, and usernames that collide once case is ignored — and
only then builds the unique indexes that enforce those rules. That ordering
matters, because an index build against data still containing duplicates fails,
and a failed build during startup takes the whole application down. Colliding
users are renamed rather than deleted, since an account owns restaurants and
reviews that deleting it would take with it. It is idempotent and a no-op once
applied, so it is safe on every boot.
Reviews written before ratings existed keep a rating of `0`: they still count as
reviews and still display, but they are left out of the average, and a
restaurant with nothing but those reads as "Not yet rated".

## Built with

### Front-end

* [HTML](https://developer.mozilla.org/en-US/docs/Web/HTML)
* [CSS](https://developer.mozilla.org/en-US/docs/Web/CSS)
* [html/template](https://pkg.go.dev/html/template)
* [Bootstrap](https://getbootstrap.com/)

### Back-end

* [net/http](https://pkg.go.dev/net/http) (Go 1.22 pattern routing)
* [mongoDB](https://www.mongodb.com/)
* [mongo-go-driver](https://github.com/mongodb/mongo-go-driver)
* [gorilla/sessions](https://github.com/gorilla/sessions)
* [gorilla/csrf](https://github.com/gorilla/csrf)
* [x/oauth2](https://pkg.go.dev/golang.org/x/oauth2)
* [x/time/rate](https://pkg.go.dev/golang.org/x/time/rate)

## License

#### [MIT](./LICENSE)
