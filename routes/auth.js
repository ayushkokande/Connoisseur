const express = require("express");
const router = express.Router();
const passport = require("passport");
const User = require("../models/user");

router.get("/", (req, res) => {
	res.render("landing");
});

router.get("/register", (req, res) => {
	res.render("auth/register");
});

router.post("/register", (req, res) => {
	const newUser = new User({ username: req.body.username });
	User.register(newUser, req.body.password, (err, user) => {
		if (err) {
			req.flash("error", err.message);
			return res.redirect("/register");
		}
		passport.authenticate("local")(req, res, () => {
			req.flash("success", "Welcome to Connoisseur, " + user.username + "!");
			res.redirect("/restaurants");
		});
	});
});

router.get("/login", (req, res) => {
	res.render("auth/login");
});

router.post("/login", passport.authenticate("local", {
	successRedirect: "/restaurants",
	failureRedirect: "/login",
	failureFlash: true
}));

router.get("/logout", (req, res) => {
	req.logout();
	req.flash("success", "You have been logged out.");
	res.redirect("/restaurants");
});

module.exports = router;
