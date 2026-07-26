package web

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/ayushkokande/Connoisseur/models"
)

func TestTemplatesRender(t *testing.T) {
	if err := InitTemplates("../templates"); err != nil {
		t.Fatal(err)
	}

	user := &models.User{ID: bson.NewObjectID(), Username: "gordon"}
	restaurant := &models.Restaurant{
		ID:          bson.NewObjectID(),
		Name:        "Test Bistro",
		Image:       "https://example.com/img.jpg",
		Cuisine:     "French",
		PriceRange:  "$$",
		Description: "A test restaurant.",
		CreatedAt:   time.Now(),
		Author:      models.Author{ID: user.ID, Username: user.Username},
	}
	comment := models.Comment{
		ID:        bson.NewObjectID(),
		Text:      "Great food!",
		CreatedAt: time.Now(),
		Author:    models.Author{ID: user.ID, Username: user.Username},
	}

	// A second page of results, so the pagination controls are exercised too.
	indexQuery := models.RestaurantQuery{Search: "bistro", Page: 2}
	indexQuery.Normalize()
	indexResults := &models.RestaurantPage{
		Restaurants: []models.Restaurant{*restaurant},
		Total:       int64(models.DefaultPerPage + 1),
		Page:        2,
		PerPage:     models.DefaultPerPage,
		TotalPages:  2,
	}

	pageData := map[string]map[string]any{
		"landing":       nil,
		"auth/login":    nil,
		"auth/register": nil,
		"restaurants/index": {
			"Results":     indexResults,
			"Query":       indexQuery,
			"Cuisines":    []string{"French", "Italian"},
			"PriceRanges": models.PriceRanges(),
			"SortOptions": sortOptions,
			"DefaultSort": models.SortNewest,
			"Pages":       pageLinks(indexQuery, indexResults),
			"PrevURL":     adjacentPageURL(indexQuery, indexResults, -1),
			"NextURL":     adjacentPageURL(indexQuery, indexResults, 1),
		},
		"restaurants/show": {"Restaurant": restaurant, "Comments": []models.Comment{comment}},
		"restaurants/new":  nil,
		"restaurants/edit": {"Restaurant": restaurant},
		"comments/new":     {"Restaurant": restaurant},
		"comments/edit":    {"RestaurantID": restaurant.ID.Hex(), "Comment": &comment},
	}

	for page, data := range pageData {
		tmpl, ok := templates[page]
		if !ok {
			t.Errorf("template %q not registered", page)
			continue
		}
		var buf bytes.Buffer
		vd := viewData{
			CurrentUser:  user,
			FlashSuccess: []string{"ok"},
			FlashError:   []string{"err"},
			Data:         data,
		}
		if err := tmpl.ExecuteTemplate(&buf, "layout.html", vd); err != nil {
			t.Errorf("rendering %q: %v", page, err)
			continue
		}
		if !strings.Contains(buf.String(), "</html>") {
			t.Errorf("rendering %q: output missing closing html tag", page)
		}
	}
}
