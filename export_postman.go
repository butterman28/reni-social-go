package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reni-social/internal/routes"

	"github.com/gofiber/fiber/v2"
)

type PostmanHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

type PostmanUrl struct {
	Raw  string   `json:"raw"`
	Host []string `json:"host"`
	Path []string `json:"path"`
}

type PostmanRequest struct {
	Method string          `json:"method"`
	Header []PostmanHeader `json:"header"`
	Url    PostmanUrl      `json:"url"`
}

type PostmanItem struct {
	Name    string         `json:"name"`
	Request PostmanRequest `json:"request"`
}

type PostmanCollection struct {
	Info struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schema      string `json:"schema"`
	} `json:"info"`
	Item []PostmanItem `json:"item"`
}

func main() {
	app := fiber.New()
	routes.Setup(app)

	collection := PostmanCollection{}
	collection.Info.Name = "Reni Social API"
	collection.Info.Description = "Auto-generated Postman Collection for Reni Social Go Backend"
	collection.Info.Schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

	for _, route := range app.GetRoutes(true) {
		// Ignore HEAD and OPTIONS requests
		if route.Method == "HEAD" || route.Method == "OPTIONS" {
			continue
		}

		// Convert Fiber route parameters (:id) to Postman format
		pathSegments := strings.Split(strings.Trim(route.Path, "/"), "/")
		cleanPath := make([]string, 0)
		for _, seg := range pathSegments {
			if strings.HasPrefix(seg, ":") {
				cleanPath = append(cleanPath, "{{"+strings.TrimPrefix(seg, ":")+"}}")
			} else {
				cleanPath = append(cleanPath, seg)
			}
		}

		// Setup default headers
		headers := []PostmanHeader{
			{Key: "Content-Type", Value: "application/json", Type: "text"},
			{Key: "ngrok-skip-browser-warning", Value: "true", Type: "text"},
		}

		// Add Auth headers for protected routes
		if strings.HasPrefix(route.Path, "/api/in") {
			headers = append(headers,
				PostmanHeader{Key: "Authorization", Value: "Bearer {{token}}", Type: "text"},
				PostmanHeader{Key: "X-Session-Id", Value: "{{session_id}}", Type: "text"},
			)
		}

		item := PostmanItem{
			Name: fmt.Sprintf("[%s] %s", route.Method, route.Path),
			Request: PostmanRequest{
				Method: route.Method,
				Header: headers,
				Url: PostmanUrl{
					Raw:  "http://127.0.0.1:1040" + route.Path,
					Host: []string{"http://127.0.0.1:1040"},
					Path: cleanPath,
				},
			},
		}

		collection.Item = append(collection.Item, item)
	}

	file, err := json.MarshalIndent(collection, "", "  ")
	if err != nil {
		fmt.Printf("Error generating collection: %v\n", err)
		return
	}

	err = os.WriteFile("reni_social_api.postman_collection.json", file, 0644)
	if err != nil {
		fmt.Printf("Error writing file: %v\n", err)
		return
	}

	fmt.Println("✅ Packaged! Saved to reni_social_api.postman_collection.json")
}