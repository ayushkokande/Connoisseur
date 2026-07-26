package main

import (
	"context"
	"log/slog"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/models"
)

var seedData = []models.Restaurant{
	{
		Name:        "Trattoria del Ponte",
		Image:       "https://images.unsplash.com/photo-1414235077428-338989a2e8c0?auto=format&fit=crop&w=800&q=80",
		Cuisine:     "Italian",
		PriceRange:  "$$$",
		Description: "Handmade pasta and wood-fired pizza in a candlelit dining room. The tagliatelle al ragù is a must.",
	},
	{
		Name:        "Sakura House",
		Image:       "https://images.unsplash.com/photo-1579871494447-9811cf80d66c?auto=format&fit=crop&w=800&q=80",
		Cuisine:     "Japanese",
		PriceRange:  "$$",
		Description: "Intimate sushi counter with daily omakase. Fish flown in fresh, seating limited, reservations recommended.",
	},
	{
		Name:        "La Taqueria Roja",
		Image:       "https://images.unsplash.com/photo-1565299585323-38d6b0865b47?auto=format&fit=crop&w=800&q=80",
		Cuisine:     "Mexican",
		PriceRange:  "$",
		Description: "No-frills counter serving al pastor carved straight from the trompo. Cash only, worth the line.",
	},
	{
		Name:        "The Brass Fig",
		Image:       "https://images.unsplash.com/photo-1550966871-3ed3cdb5ed0c?auto=format&fit=crop&w=800&q=80",
		Cuisine:     "New American",
		PriceRange:  "$$$$",
		Description: "Seasonal tasting menus built around local farms. Elegant room, exceptional wine pairings.",
	},
}

// seedReviews are attached to each seeded restaurant in turn, so the sample
// data exercises ratings and averages rather than showing every listing as
// unrated.
var seedReviews = [][]struct {
	Rating int
	Text   string
}{
	{
		{5, "The tagliatelle al ragù is worth the trip on its own."},
		{4, "Lovely room and attentive service, though the wine list is pricey."},
	},
	{
		{5, "Best omakase in the city. Book weeks ahead."},
		{5, "Every course was a surprise. Sit at the counter."},
		{3, "Excellent fish, but the seats are cramped."},
	},
	{
		{4, "Al pastor straight off the trompo. Bring cash."},
	},
	{
		{5, "A genuinely memorable tasting menu."},
		{2, "Beautiful plates, tiny portions, eye-watering bill."},
	},
}

func seedDB() {
	ctx := context.Background()

	if err := models.DeleteAllRestaurants(ctx); err != nil {
		slog.Error("seed: deleting restaurants", "error", err)
		return
	}
	if err := models.DeleteAllComments(ctx); err != nil {
		slog.Error("seed: deleting comments", "error", err)
		return
	}
	slog.Info("seed: cleared existing restaurants and comments")

	critic := models.Author{ID: bson.NewObjectID(), Username: "connoisseur"}

	for i, seed := range seedData {
		restaurant := seed
		if err := models.CreateRestaurant(ctx, &restaurant); err != nil {
			slog.Error("seed: creating restaurant", "name", restaurant.Name, "error", err)
			continue
		}

		if i < len(seedReviews) {
			for _, review := range seedReviews[i] {
				if _, err := models.CreateComment(ctx, restaurant.ID, review.Rating, review.Text, critic); err != nil {
					slog.Error("seed: creating review", "restaurant", restaurant.Name, "error", err)
				}
			}
		}
		slog.Info("seed: added restaurant", "name", restaurant.Name)
	}
}
