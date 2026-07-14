const express = require("express"),
	  mongoose = require("mongoose"),
	  passport = require("passport"),
	  flash = require("connect-flash"),
	  LocalStrategy = require("passport-local"),
	  methodOverride = require("method-override"),
	  User = require("./models/user"),
	  seedDb = require("./seeds");

const restaurantRoutes = require("./routes/restaurants"),
	  commentRoutes = require("./routes/comments"),
	  authRoutes = require("./routes/auth");

const databaseUrl = process.env.DATABASE_URL || "mongodb://localhost/connoisseur";

mongoose.connect(databaseUrl, {
	useNewUrlParser: true,
	useCreateIndex: true,
	useUnifiedTopology: true,
	useFindAndModify: false
})
	.then(() => console.log("Successfully connected to the Connoisseur database!"))
	.catch(err => console.log("ERROR: " + err.message));

const app = express();
app.set("view engine", "ejs");
app.use(express.urlencoded({ extended: true }));
app.use(express.static(__dirname + "/public"));
app.use(methodOverride("_method"));
app.use(flash());

app.locals.moment = require("moment");

app.use(require("express-session")({
	secret: process.env.SESSION_SECRET || "connoisseur-dev-secret",
	saveUninitialized: false,
	resave: false
}));
app.use(passport.initialize());
app.use(passport.session());

passport.use(new LocalStrategy(User.authenticate()));
passport.serializeUser(User.serializeUser());
passport.deserializeUser(User.deserializeUser());

app.use((req, res, next) => {
	res.locals.currentUser = req.user;
	res.locals.flashSuccessMessage = req.flash("success");
	res.locals.flashErrorMessage = req.flash("error");
	next();
});

app.use("/restaurants", restaurantRoutes);
app.use("/restaurants/:id/comments", commentRoutes);
app.use(authRoutes);

if (process.env.SEED_DB === "true") {
	seedDb();
}

app.use((req, res) => res.status(404).send("Error 404 - Page not found..."));

const port = process.env.PORT || 3000;
app.listen(port, () => {
	console.log("Connoisseur server has started and is listening on port: " + port);
});
