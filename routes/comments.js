const express = require("express");
const router = express.Router({ mergeParams: true });
const Restaurant = require("../models/restaurant");
const Comment = require("../models/comment");
const middleware = require("../middleware");

router.get("/new", middleware.isLoggedIn, (req, res) => {
	Restaurant.findById(req.params.id, (err, restaurant) => {
		if (err || !restaurant) {
			req.flash("error", "Restaurant not found!");
			return res.redirect("/restaurants");
		}
		res.render("comments/new", { restaurant: restaurant });
	});
});

router.post("/", middleware.isLoggedIn, (req, res) => {
	Restaurant.findById(req.params.id, (err, restaurant) => {
		if (err || !restaurant) {
			req.flash("error", "Restaurant not found!");
			return res.redirect("/restaurants");
		}
		Comment.create({ text: req.body.text }, (err, comment) => {
			if (err) {
				req.flash("error", "Something went wrong adding your review.");
				return res.redirect("/restaurants/" + restaurant._id);
			}
			comment.author.id = req.user._id;
			comment.author.username = req.user.username;
			comment.save();

			restaurant.comments.push(comment);
			restaurant.save();

			req.flash("success", "Review added!");
			res.redirect("/restaurants/" + restaurant._id);
		});
	});
});

router.get("/:comment_id/edit", middleware.checkCommentOwnership, (req, res) => {
	Comment.findById(req.params.comment_id, (err, comment) => {
		if (err || !comment) {
			req.flash("error", "Review not found!");
			return res.redirect("back");
		}
		res.render("comments/edit", { restaurant_id: req.params.id, comment: comment });
	});
});

router.put("/:comment_id", middleware.checkCommentOwnership, (req, res) => {
	Comment.findByIdAndUpdate(req.params.comment_id, { text: req.body.text }, (err) => {
		if (err) {
			req.flash("error", "Something went wrong updating your review.");
			return res.redirect("back");
		}
		req.flash("success", "Review updated!");
		res.redirect("/restaurants/" + req.params.id);
	});
});

router.delete("/:comment_id", middleware.checkCommentOwnership, (req, res) => {
	Comment.findByIdAndRemove(req.params.comment_id, (err) => {
		if (err) {
			req.flash("error", "Something went wrong deleting your review.");
			return res.redirect("back");
		}
		Restaurant.findByIdAndUpdate(req.params.id, { $pull: { comments: req.params.comment_id } }, (err) => {
			if (err) {
				console.log(err);
			}
			req.flash("success", "Review deleted!");
			res.redirect("/restaurants/" + req.params.id);
		});
	});
});

module.exports = router;
