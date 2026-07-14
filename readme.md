# Connoisseur

> A restaurant review web application built with Go and MongoDB. Discover, review, and share the best restaurants around you.

## Features

* Authentication:

  * User sign-up and login with username and password (bcrypt-hashed)

* Authorization:

  * Users cannot manage restaurants or reviews without being authenticated

  * Users cannot edit or delete restaurants or reviews created by other users

* Manage restaurant listings with full CRUD functionality:

  * Browse, create, edit, and delete restaurants and reviews

  * Restaurant details include cuisine type, price range, and photos

* Flash messages responding to user interactions

* Responsive web design with Bootstrap

## Getting Started

### Prerequisites

* [Go](https://go.dev/) (1.22 or later)
* [MongoDB](https://www.mongodb.com/) running locally, or a connection string to a hosted instance

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

Environment variables:

| Variable | Purpose | Default |
| --- | --- | --- |
| `DATABASE_URL` | MongoDB connection string | `mongodb://localhost:27017` |
| `DATABASE_NAME` | MongoDB database name | `connoisseur` |
| `SESSION_SECRET` | Key used to sign session cookies | random per run in development; **required** in production |
| `CSRF_SECRET` | Key used to sign CSRF tokens | random per run in development; **required** in production |
| `APP_ENV` | Set to `production` to require the secrets above and mark cookies `Secure` | unset |
| `PORT` | Port the server listens on | `3000` |
| `SEED_DB` | Set to `true` to reset and seed the database on startup | unset |

In development the two secrets are generated randomly at startup, which logs
everyone out on restart but means no known key is ever baked into the source. In
production the server refuses to start without them.

`APP_ENV=production` also marks the session and CSRF cookies `Secure`, so they
are only sent over HTTPS — do not set it when serving plain HTTP or logging in
will silently fail.

### Run the tests

```sh
go test ./...
```

The handler tests are integration tests: they need a MongoDB reachable at
`TEST_DATABASE_URL` (default `mongodb://localhost:27017`) and they wipe the
`connoisseur_test` database. They skip rather than fail when no MongoDB is
available.

## Security

* Passwords are hashed with bcrypt; the 72-byte bcrypt input limit is enforced
  at registration rather than silently truncating.
* All state-changing requests require a CSRF token, submitted as a hidden field
  in every form.
* Session cookies are `HttpOnly`, `SameSite=Lax`, `Secure` in production, and
  expire after a week. Logging in clears any prior session state; logging out
  expires the cookie.
* Logout is a POST, so it cannot be triggered by a cross-site link.
* User input is validated in the model layer: username shape and length,
  password length, field lengths, an allowlisted price range, and image URLs
  restricted to absolute `http`/`https` (which rejects `javascript:` and
  `data:` URLs).

## Project Structure

```
Connoisseur/
├── main.go             # App entry point: config, DB connection, server startup
├── seeds.go            # Optional database seeder (enabled with SEED_DB=true)
├── models/
│   ├── db.go           # Collection handles and indexes
│   ├── restaurant.go   # Restaurant model (name, image, cuisine, price range, ...)
│   ├── comment.go      # Review model
│   ├── user.go         # User model (bcrypt registration/authentication)
│   └── validate.go     # Input validation rules
├── web/
│   ├── routes.go       # Route table, CSRF protection
│   ├── restaurants.go  # RESTful restaurant handlers
│   ├── comments.go     # Nested review handlers
│   ├── auth.go         # Landing, register, login, logout
│   ├── middleware.go   # Auth & ownership middleware, method override
│   ├── session.go      # Cookie sessions, current user, flash messages
│   ├── errors.go       # Error-to-flash mapping
│   └── render.go       # Template parsing and rendering helpers
├── templates/          # html/template views
└── public/             # Static assets (stylesheets)
```

## Routes

### Restaurants

| Name | Path | Verb | Description |
| --- | --- | --- | --- |
| Index | `/restaurants` | GET | List all restaurants |
| New | `/restaurants/new` | GET | Form to add a restaurant * |
| Create | `/restaurants` | POST | Add a new restaurant * |
| Show | `/restaurants/:id` | GET | Details for one restaurant |
| Edit | `/restaurants/:id/edit` | GET | Form to edit a restaurant ** |
| Update | `/restaurants/:id` | PUT | Update a restaurant ** |
| Destroy | `/restaurants/:id` | DELETE | Delete a restaurant and its reviews ** |

### Reviews

| Name | Path | Verb | Description |
| --- | --- | --- | --- |
| New | `/restaurants/:id/comments/new` | GET | Form to add a review * |
| Create | `/restaurants/:id/comments` | POST | Add a review * |
| Edit | `/restaurants/:id/comments/:comment_id/edit` | GET | Form to edit a review ** |
| Update | `/restaurants/:id/comments/:comment_id` | PUT | Update a review ** |
| Destroy | `/restaurants/:id/comments/:comment_id` | DELETE | Delete a review ** |

### Auth

| Path | Verb | Description |
| --- | --- | --- |
| `/` | GET | Landing page |
| `/register` | GET / POST | Sign-up form and handler |
| `/login` | GET / POST | Login form and handler |
| `/logout` | POST | Log out |

\* requires login &nbsp;&nbsp; \** requires login and ownership

HTML forms submit PUT/DELETE via a `_method` override parameter, mirroring the classic method-override pattern. Every non-GET request carries a CSRF token.

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
* [x/crypto/bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt)

## License

#### [MIT](./LICENSE)
