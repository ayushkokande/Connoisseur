const Restaurant = require("../models/restaurant");
const Comment = require("../models/comment");

const middleware = {};

middleware.isLoggedIn = (req, res, next) => {
	if (req.isAuthenticated()) {
		return next();
	}
	req.flash("error", "You need to be logged in to do that!");
	res.redirect("/login");
};

middleware.checkRestaurantOwnership = (req, res, next) => {
	if (!req.isAuthenticated()) {
		req.flash("error", "You need to be logged in to do that!");
		return res.redirect("back");
	}
	Restaurant.findById(req.params.id, (err, restaurant) => {
		if (err || !restaurant) {
			req.flash("error", "Restaurant not found!");
			return res.redirect("back");
		}
		if (!restaurant.author.id.equals(req.user._id)) {
			req.flash("error", "You don't have permission to do that!");
			return res.redirect("back");
		}
		next();
	});
};

middleware.checkCommentOwnership = (req, res, next) => {
	if (!req.isAuthenticated()) {
		req.flash("error", "You need to be logged in to do that!");
		return res.redirect("back");
	}
	Comment.findById(req.params.comment_id, (err, comment) => {
		if (err || !comment) {
			req.flash("error", "Review not found!");
			return res.redirect("back");
		}
		if (!comment.author.id.equals(req.user._id)) {
			req.flash("error", "You don't have permission to do that!");
			return res.redirect("back");
		}
		next();
	});
};

module.exports = middleware;
