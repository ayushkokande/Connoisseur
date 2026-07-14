const Restaurant = require("./models/restaurant");
const Comment = require("./models/comment");

const data = [
	{
		name: "Trattoria del Ponte",
		image: "https://images.unsplash.com/photo-1414235077428-338989a2e8c0?auto=format&fit=crop&w=800&q=80",
		cuisine: "Italian",
		priceRange: "$$$",
		description: "Handmade pasta and wood-fired pizza in a candlelit dining room. The tagliatelle al ragù is a must."
	},
	{
		name: "Sakura House",
		image: "https://images.unsplash.com/photo-1579871494447-9811cf80d66c?auto=format&fit=crop&w=800&q=80",
		cuisine: "Japanese",
		priceRange: "$$",
		description: "Intimate sushi counter with daily omakase. Fish flown in fresh, seating limited, reservations recommended."
	},
	{
		name: "La Taqueria Roja",
		image: "https://images.unsplash.com/photo-1565299585323-38d6b0865b47?auto=format&fit=crop&w=800&q=80",
		cuisine: "Mexican",
		priceRange: "$",
		description: "No-frills counter serving al pastor carved straight from the trompo. Cash only, worth the line."
	},
	{
		name: "The Brass Fig",
		image: "https://images.unsplash.com/photo-1550966871-3ed3cdb5ed0c?auto=format&fit=crop&w=800&q=80",
		cuisine: "New American",
		priceRange: "$$$$",
		description: "Seasonal tasting menus built around local farms. Elegant room, exceptional wine pairings."
	}
];

function seedDb() {
	Restaurant.deleteMany({}, (err) => {
		if (err) {
			return console.log(err.message);
		}
		console.log("Deleted all restaurants from the database!");

		Comment.deleteMany({}, (err) => {
			if (err) {
				return console.log(err.message);
			}

			data.forEach((seed) => {
				Restaurant.create(seed, (err, restaurant) => {
					if (err) {
						console.log(err);
					} else {
						console.log("Added seed restaurant: " + restaurant.name);
					}
				});
			});
		});
	});
}

module.exports = seedDb;
