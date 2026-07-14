# Connoisseur

> A restaurant review web application built with Node.js, Express, and MongoDB. Discover, review, and share the best restaurants around you.

## Features

* Authentication:

  * User sign-up and login with username and password

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

* [Node.js](https://nodejs.org/) (v12 or later)
* [MongoDB](https://www.mongodb.com/) running locally, or a connection string to a hosted instance

### Clone this repository

```sh
git clone https://github.com/ayushkokande/Connoisseur.git
cd Connoisseur
```

### Install dependencies

```sh
npm install
```

### Run the app

```sh
npm start
```

The app runs at [http://localhost:3000](http://localhost:3000) and connects to `mongodb://localhost/connoisseur` by default.

Optional environment variables:

| Variable | Purpose | Default |
| --- | --- | --- |
| `DATABASE_URL` | MongoDB connection string | `mongodb://localhost/connoisseur` |
| `SESSION_SECRET` | Secret used to sign session cookies | dev-only fallback |
| `PORT` | Port the server listens on | `3000` |
| `SEED_DB` | Set to `true` to reset and seed the database on startup | unset |

For development with auto-reload:

```sh
npm run dev
```

## Project Structure

```
Connoisseur/
├── index.js            # App entry point: Express setup, DB connection, Passport config
├── seeds.js            # Optional database seeder (enabled with SEED_DB=true)
├── middleware/
│   └── index.js        # Auth & ownership middleware
├── models/
│   ├── restaurant.js   # Restaurant schema (name, image, cuisine, price range, ...)
│   ├── comment.js      # Review schema
│   └── user.js         # User schema (passport-local-mongoose)
├── routes/
│   ├── restaurants.js  # RESTful restaurant routes
│   ├── comments.js     # Nested review routes
│   └── auth.js         # Landing, register, login, logout
├── views/              # EJS templates
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

## Built with

### Front-end

* [HTML](https://developer.mozilla.org/en-US/docs/Web/HTML)
* [CSS](https://developer.mozilla.org/en-US/docs/Web/CSS)
* [JavaScript](https://developer.mozilla.org/en-US/docs/Learn/JavaScript/First_steps/What_is_JavaScript)
* [ejs](http://ejs.co/)
* [Bootstrap](https://getbootstrap.com/)

### Back-end

* [express](https://expressjs.com/)
* [mongoDB](https://www.mongodb.com/)
* [mongoose](http://mongoosejs.com/)
* [passport](http://www.passportjs.org/)
* [passport-local](https://github.com/jaredhanson/passport-local#passport-local)
* [express-session](https://github.com/expressjs/session#express-session)
* [method-override](https://github.com/expressjs/method-override#method-override)
* [moment](https://momentjs.com/)
* [connect-flash](https://github.com/jaredhanson/connect-flash#connect-flash)

## License

#### [MIT](./LICENSE)
