package main

import (
	"context"
	"log"

	"github.com/shivamdubey91/connoisseur/models"
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

func seedDB() {
	ctx := context.Background()

	if err := models.DeleteAllRestaurants(ctx); err != nil {
		log.Println(err)
		return
	}
	log.Println("Deleted all restaurants from the database!")

	if err := models.DeleteAllComments(ctx); err != nil {
		log.Println(err)
		return
	}

	for _, seed := range seedData {
		restaurant := seed
		if err := models.CreateRestaurant(ctx, &restaurant); err != nil {
			log.Println(err)
			continue
		}
		log.Println("Added seed restaurant: " + restaurant.Name)
	}
}
