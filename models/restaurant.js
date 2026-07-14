const mongoose = require("mongoose");

const restaurantSchema = new mongoose.Schema({
	name: String,
	image: String,
	cuisine: String,
	priceRange: String,
	description: String,
	createdAt: { type: Date, default: Date.now },
	author: {
		id: {
			type: mongoose.Schema.Types.ObjectId,
			ref: "User"
		},
		username: String
	},
	comments: [
		{
			type: mongoose.Schema.Types.ObjectId,
			ref: "Comment"
		}
	]
});

module.exports = mongoose.model("Restaurant", restaurantSchema);
