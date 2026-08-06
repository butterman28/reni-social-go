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

// 👇 NEW: Struct to hold Query Parameters for Postman
type PostmanQuery struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type PostmanUrl struct {
	Raw   string         `json:"raw"`
	Host  []string       `json:"host"`
	Path  []string       `json:"path"`
	Query []PostmanQuery `json:"query,omitempty"` // 👈 Added Query array
}

type PostmanRequest struct {
	Method      string          `json:"method"`
	Header      []PostmanHeader `json:"header"`
	Url         PostmanUrl      `json:"url"`
	Description string          `json:"description,omitempty"`
}

type PostmanItem struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Request     *PostmanRequest `json:"request,omitempty"`
	Item        []PostmanItem   `json:"item,omitempty"`
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
	collection.Info.Schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

	//Added the Pro-Tip table at the very top!
	collection.Info.Description = "## 💡 Quick Setup Pro-Tip (Do This First!)\n\n" +
		"Before testing any endpoints, click on the **Reni Social API** collection name in the left sidebar, go to the **Variables** tab, and set up these initial values so you don't have to type them manually every time:\n\n" +
		"| Variable | Initial Value | Notes |\n" +
		"| :--- | :--- | :--- |\n" +
		"| `workspace_id` | `Your current workspace UUID |\n" +
		"| `session_id` | *(your current redis session token)* | Get this from the `/api/auth/signin` response |\n" +
		"| `token` | `dummy_token` | The middleware just checks for the word \"Bearer\" |\n" +
		"| `attempt_id` | *(leave blank for now)* | Will be populated after triggering a publish |\n\n" +
		"---\n\n" +
		"# Reni Social API\n\n" +
		"**Authentication:**\n" +
		"Protected routes (under `/api/in`) require two headers:\n" +
		"1. `Authorization: Bearer {{token}}` (Fiber middleware just checks for the word \"Bearer\")\n" +
		"2. `X-Session-Id: {{session_id}}` (The actual session token from the `/api/auth/signin` response)\n\n" +
		"**Workflows:**\n" +
		"Check the **📚 API Workflows & Guides** folder below for detailed frontend integration guides, including the Real-Time Async Publishing flow."

	collection.Item = append(collection.Item, createWorkflowGuide())

	for _, route := range app.GetRoutes(true) {
		if route.Method == "HEAD" || route.Method == "OPTIONS" {
			continue
		}

		pathSegments := strings.Split(strings.Trim(route.Path, "/"), "/")
		cleanPath := make([]string, 0)
		for _, seg := range pathSegments {
			if strings.HasPrefix(seg, ":") {
				cleanPath = append(cleanPath, "{{"+strings.TrimPrefix(seg, ":")+"}}")
			} else {
				cleanPath = append(cleanPath, seg)
			}
		}

		// ──────────────────────────────────────────────────────
		// 👇 SMART QUERY PARAMETER INJECTION
		// ──────────────────────────────────────────────────────
		var queries []PostmanQuery
		rawURL := "http://127.0.0.1:1040" + route.Path
		var queryParts []string

		// 1. workspace_id is required for almost all /posts endpoints
		if strings.HasPrefix(route.Path, "/api/in/posts") {
			queries = append(queries, PostmanQuery{
				Key:         "workspace_id",
				Value:       "{{workspace_id}}",
				Description: "Required: The UUID of the workspace this post belongs to.",
			})
			queryParts = append(queryParts, "workspace_id={{workspace_id}}")
		}

		// 2. Extra filters for GET /posts (List)
		if route.Path == "/api/in/posts" && route.Method == "GET" {
			queries = append(queries,
				PostmanQuery{Key: "status", Value: "draft", Description: "Optional: Filter by status (draft, scheduled, publishing, published, failed)"},
				PostmanQuery{Key: "page", Value: "0", Description: "Optional: Page number for pagination"},
				PostmanQuery{Key: "limit", Value: "20", Description: "Optional: Number of results per page"},
			)
			queryParts = append(queryParts, "status=draft", "page=0", "limit=20")
		}

		// 3. attempt_id is required for GET /results (Real-time polling)
		if strings.HasSuffix(route.Path, "/results") && route.Method == "GET" {
			queries = append(queries, PostmanQuery{
				Key:         "attempt_id",
				Value:       "{{attempt_id}}",
				Description: "Required: The tracking ID returned by the /publish endpoint to poll real-time status.",
			})
			queryParts = append(queryParts, "attempt_id={{attempt_id}}")
		}

		// 4. connection_id for X (Twitter) read endpoints
		if strings.Contains(route.Path, "/x/") && route.Method == "GET" && !strings.HasSuffix(route.Path, "/connections") {
			queries = append(queries, PostmanQuery{
				Key:         "connection_id",
				Value:       "{{connection_id}}",
				Description: "Optional: The specific X connection ID to use (defaults to first available if omitted).",
			})
			queryParts = append(queryParts, "connection_id={{connection_id}}")
		}

		// Append query string to the raw URL
		if len(queryParts) > 0 {
			rawURL += "?" + strings.Join(queryParts, "&")
		}

		// ──────────────────────────────────────────────────────
		// Headers & Descriptions
		// ──────────────────────────────────────────────────────
		headers := []PostmanHeader{
			{Key: "Content-Type", Value: "application/json", Type: "text"},
			{Key: "ngrok-skip-browser-warning", Value: "true", Type: "text"},
		}

		if strings.HasPrefix(route.Path, "/api/in") {
			headers = append(headers,
				PostmanHeader{Key: "Authorization", Value: "Bearer {{token}}", Type: "text"},
				PostmanHeader{Key: "X-Session-Id", Value: "{{session_id}}", Type: "text"},
			)
		}

		var reqDescription string
		if strings.Contains(route.Path, "/publish") && route.Method == "POST" {
			reqDescription = "### ⚡ Async Publish Trigger\n\nThis endpoint does **not** block. It initiates the publish process and immediately returns an `attempt_id`.\n\n**Next Step:** Use the returned `attempt_id` to poll the `/results` endpoint.\n\n*(See the \"📚 API Workflows & Guides\" folder for the full frontend integration guide).*"
		}
		if strings.Contains(route.Path, "/results") && route.Method == "GET" {
			reqDescription = "### 📊 Real-Time Status Polling\n\nPass the `?attempt_id={{attempt_id}}` query parameter to track the exact status of a specific publish click.\n\n**Statuses:**\n- `processing`: Platform is still working (show spinner)\n- `published`: Success (show checkmark)\n- `failed`: Error (show `fail_reason`)"
		}

		item := PostmanItem{
			Name: fmt.Sprintf("[%s] %s", route.Method, route.Path),
			Request: &PostmanRequest{
				Method:      route.Method,
				Header:      headers,
				Description: reqDescription,
				Url: PostmanUrl{
					Raw:   rawURL,      // 👈 Now includes ?workspace_id=...
					Host:  []string{"http://127.0.0.1:1040"},
					Path:  cleanPath,
					Query: queries,     // 👈 Populates the Postman "Params" tab!
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

func createWorkflowGuide() PostmanItem {
	md := "# 🚀 Real-Time Publishing Workflow\n\n" +
		"When a user clicks \"Publish\", the backend processes platforms asynchronously (especially Instagram/TikTok videos, which can take 10–30 seconds to process on Meta's servers). Instead of blocking the UI with a long loading spinner, we use an **Idempotency Key + Smart Polling** approach.\n\n" +
		"## The 3-Step Flow\n\n" +
		"### 1️⃣ Trigger the Publish\n" +
		"**Endpoint:** `POST /api/in/posts/in/:id/publish?workspace_id=:workspace_id`\n\n" +
		"Instead of returning \"post published\", the API now immediately returns a tracking ID.\n\n" +
		"**Response:**\n" +
		"```json\n" +
		"{\n" +
		"    \"message\": \"publishing_initiated\",\n" +
		"    \"attempt_id\": \"8f4e2d1c-9b3a-4e5f-8c7d-6e5f4a3b2c1d\"\n" +
		"}\n" +
		"```\n" +
		"**Action:** Save the `attempt_id` in your component state. This is your \"receipt\" for this specific button click.\n\n" +
		"---\n\n" +
		"### 2️⃣ Poll for Real-Time Status\n" +
		"**Endpoint:** `GET /api/in/posts/in/:id/results?attempt_id=:attempt_id`\n\n" +
		"**Action:** Start a `setInterval` (every 2 seconds) calling this endpoint using the `attempt_id` from Step 1. Because we pass the `attempt_id`, the API will ignore any old failed attempts and only return the status of the *current* publish job.\n\n" +
		"**Response:**\n" +
		"```json\n" +
		"{\n" +
		"    \"results\": [\n" +
		"        {\n" +
		"            \"platform\": \"linkedin\",\n" +
		"            \"status\": \"published\",\n" +
		"            \"platform_post_id\": \"urn:li:share:7123...\",\n" +
		"            \"fail_reason\": \"\"\n" +
		"        },\n" +
		"        {\n" +
		"            \"platform\": \"meta_ig\",\n" +
		"            \"status\": \"processing\",\n" +
		"            \"container_id\": \"17954...\",\n" +
		"            \"fail_reason\": \"\"\n" +
		"        }\n" +
		"    ]\n" +
		"}\n" +
		"```\n" +
		"**UI Mapping:**\n" +
		"* `status === 'processing'` ➡️ Show a spinner for that specific platform.\n" +
		"* `status === 'published'` ➡️ Show a green checkmark.\n" +
		"* `status === 'failed'` ➡️ Show a red X and display the `fail_reason`.\n\n" +
		"---\n\n" +
		"### 3️⃣ Stop Polling (Terminal State)\n" +
		"**Action:** Check if **all** platforms in the `results` array have reached a terminal state (`published` or `failed`). Once they have, clear the interval.\n\n" +
		"```javascript\n" +
		"const allDone = results.every(r => r.status === 'published' || r.status === 'failed');\n" +
		"if (allDone) {\n" +
		"    clearInterval(pollingInterval);\n" +
		"}\n" +
		"```\n"

	return PostmanItem{
		Name:        "📚 API Workflows & Guides",
		Description: "Documentation for complex multi-step workflows in the Reni Social API.",
		Item: []PostmanItem{
			{
				Name: "Real-Time Publishing Flow (Frontend Guide)",
				Request: &PostmanRequest{
					Method: "GET",
					Header: []PostmanHeader{},
					Url: PostmanUrl{
						Raw:  "http://127.0.0.1:1040/health",
						Host: []string{"http://127.0.0.1:1040"},
						Path: []string{"health"},
					},
					Description: md,
				},
			},
		},
	}
}