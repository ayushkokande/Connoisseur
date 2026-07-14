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

Optional environment variables:

| Variable | Purpose | Default |
| --- | --- | --- |
| `DATABASE_URL` | MongoDB connection string | `mongodb://localhost:27017` |
| `DATABASE_NAME` | MongoDB database name | `connoisseur` |
| `SESSION_SECRET` | Secret used to sign session cookies | dev-only fallback |
| `PORT` | Port the server listens on | `3000` |
| `SEED_DB` | Set to `true` to reset and seed the database on startup | unset |

### Run the tests

```sh
go test ./...
```

## Project Structure

```
Connoisseur/
├── main.go             # App entry point: config, DB connection, server startup
├── seeds.go            # Optional database seeder (enabled with SEED_DB=true)
├── models/
│   ├── db.go           # Collection handles and indexes
│   ├── restaurant.go   # Restaurant model (name, image, cuisine, price range, ...)
│   ├── comment.go      # Review model
│   └── user.go         # User model (bcrypt registration/authentication)
├── web/
│   ├── routes.go       # Route table
│   ├── restaurants.go  # RESTful restaurant handlers
│   ├── comments.go     # Nested review handlers
│   ├── auth.go         # Landing, register, login, logout
│   ├── middleware.go   # Auth & ownership middleware, method override
│   ├── session.go      # Cookie sessions, current user, flash messages
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
| `/logout` | GET | Log out |

\* requires login &nbsp;&nbsp; \** requires login and ownership

HTML forms submit PUT/DELETE via a `_method` override parameter, mirroring the classic method-override pattern.

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
* [x/crypto/bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt)

## License

#### [MIT](./LICENSE)
