const express = require("express");
const router = express.Router();
const Restaurant = require("../models/restaurant");
const middleware = require("../middleware");

router.get("/", (req, res) => {
	Restaurant.find({}, (err, restaurants) => {
		if (err) {
			console.log(err);
			return res.redirect("/");
		}
		res.render("restaurants/index", { restaurants: restaurants });
	});
});

router.post("/", middleware.isLoggedIn, (req, res) => {
	const newRestaurant = {
		name: req.body.name,
		image: req.body.image,
		cuisine: req.body.cuisine,
		priceRange: req.body.priceRange,
		description: req.body.description,
		author: {
			id: req.user._id,
			username: req.user.username
		}
	};
	Restaurant.create(newRestaurant, (err, restaurant) => {
		if (err) {
			req.flash("error", "Something went wrong creating the restaurant.");
			return res.redirect("/restaurants");
		}
		req.flash("success", "Restaurant added successfully!");
		res.redirect("/restaurants/" + restaurant._id);
	});
});

router.get("/new", middleware.isLoggedIn, (req, res) => {
	res.render("restaurants/new");
});

router.get("/:id", (req, res) => {
	Restaurant.findById(req.params.id).populate("comments").exec((err, restaurant) => {
		if (err || !restaurant) {
			req.flash("error", "Restaurant not found!");
			return res.redirect("/restaurants");
		}
		res.render("restaurants/show", { restaurant: restaurant });
	});
});

router.get("/:id/edit", middleware.checkRestaurantOwnership, (req, res) => {
	Restaurant.findById(req.params.id, (err, restaurant) => {
		if (err || !restaurant) {
			req.flash("error", "Restaurant not found!");
			return res.redirect("/restaurants");
		}
		res.render("restaurants/edit", { restaurant: restaurant });
	});
});

router.put("/:id", middleware.checkRestaurantOwnership, (req, res) => {
	const updated = {
		name: req.body.name,
		image: req.body.image,
		cuisine: req.body.cuisine,
		priceRange: req.body.priceRange,
		description: req.body.description
	};
	Restaurant.findByIdAndUpdate(req.params.id, updated, (err) => {
		if (err) {
			req.flash("error", "Something went wrong updating the restaurant.");
			return res.redirect("/restaurants");
		}
		req.flash("success", "Restaurant updated!");
		res.redirect("/restaurants/" + req.params.id);
	});
});

router.delete("/:id", middleware.checkRestaurantOwnership, (req, res) => {
	Restaurant.findByIdAndRemove(req.params.id, (err, restaurant) => {
		if (err || !restaurant) {
			req.flash("error", "Something went wrong deleting the restaurant.");
			return res.redirect("/restaurants");
		}
		const Comment = require("../models/comment");
		Comment.deleteMany({ _id: { $in: restaurant.comments } }, (err) => {
			if (err) {
				console.log(err);
			}
			req.flash("success", "Restaurant deleted!");
			res.redirect("/restaurants");
		});
	});
});

module.exports = router;
