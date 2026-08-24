package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reni-social/internal/routes"

	"github.com/gofiber/fiber/v2"
)

// ──────────────────────────────────────────────────────
// Postman Collection Structs (v2.1.0)
// ──────────────────────────────────────────────────────

type PostmanHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Type  string `json:"type"`
}

type PostmanQuery struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description,omitempty"`
}

type PostmanUrl struct {
	Raw   string         `json:"raw"`
	Host  []string       `json:"host"`
	Path  []string       `json:"path"`
	Query []PostmanQuery `json:"query,omitempty"`
}

type PostmanBody struct {
	Mode    string                 `json:"mode"`
	Raw     string                 `json:"raw"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type PostmanEvent struct {
	Listen string `json:"listen"`
	Script struct {
		Exec []string `json:"exec"`
		Type string   `json:"type"`
	} `json:"script"`
}

type PostmanRequest struct {
	Method      string          `json:"method"`
	Header      []PostmanHeader `json:"header"`
	Body        *PostmanBody    `json:"body,omitempty"`
	Url         PostmanUrl      `json:"url"`
	Description string          `json:"description,omitempty"`
	Event       []PostmanEvent  `json:"event,omitempty"`
}

type PostmanResponse struct {
	Name            string          `json:"name"`
	OriginalRequest *PostmanRequest `json:"originalRequest,omitempty"`
	Status          string          `json:"status"`
	Code            int             `json:"code"`
	Header          []PostmanHeader `json:"header"`
	Body            string          `json:"body"`
}

type PostmanItem struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Request     *PostmanRequest   `json:"request,omitempty"`
	Response    []PostmanResponse `json:"response,omitempty"`
	Item        []PostmanItem     `json:"item,omitempty"`
}

type PostmanCollection struct {
	Info struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Schema      string `json:"schema"`
	} `json:"info"`
	Item []PostmanItem `json:"item"`
}

// ──────────────────────────────────────────────────────
// Main Generator
// ──────────────────────────────────────────────────────

func main() {
	app := fiber.New()
	routes.Setup(app)

	collection := PostmanCollection{}
	collection.Info.Name = "Reni Social API"
	collection.Info.Schema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
	collection.Info.Description = "## 🚀 Quick Setup Pro-Tip (Do This First!)\n\n" +
		"1. Click on the **Reni Social API** collection name in the left sidebar.\n" +
		"2. Go to the **Variables** tab.\n" +
		"3. The test scripts will automatically populate `session_id`, `workspace_id`, and `attempt_id` when you run specific requests!\n\n" +
		"**Authentication:**\n" +
		"Protected routes (under `/api/in`) automatically include:\n" +
		"1. `Authorization: Bearer {{token}}`\n" +
		"2. `X-Session-Id: {{session_id}}`"

	// 1. Inject Full API Documentation (reads from API_DOCUMENTATION.md on disk)
	if docsMarkdown, err := os.ReadFile("API_DOCUMENTATION.md"); err == nil {
		collection.Item = append(collection.Item, createFullAPIDocs(string(docsMarkdown)))
		fmt.Println("📖 Injected API_DOCUMENTATION.md into Postman collection.")
	} else {
		fmt.Println("⚠️ Warning: Could not read API_DOCUMENTATION.md. Make sure it exists in the project root.")
	}

	// 2. Inject Refresh & Re-authing Documentation
	if reauthMarkdown, err := os.ReadFile("docs/refresh and reauthing.md"); err == nil {
		collection.Item = append(collection.Item, createRefreshReauthingDocs(string(reauthMarkdown)))
		fmt.Println("🔄 Injected docs/refresh and reauthing.md into Postman collection.")
	} else {
		fmt.Println("⚠️ Warning: Could not read docs/refresh and reauthing.md. Make sure it exists in docs/.")
	}

	// 3. Add Workflow Guide
	collection.Item = append(collection.Item, createWorkflowGuide())

	// Initialize Folders
	folders := map[string]*PostmanItem{
		"auth":        {Name: "🔐 Auth & Onboarding", Item: []PostmanItem{}},
		"workspaces":  {Name: "🏢 Workspaces & Members", Item: []PostmanItem{}},
		"connections": {Name: "🔗 Platform Connections", Item: []PostmanItem{}},
		"posts":       {Name: "📝 Posts & Publishing", Item: []PostmanItem{}},
		"x_read":      {Name: "🐦 X (Twitter) Read API", Item: []PostmanItem{}},
		"system":      {Name: "⚙️ System & Webhooks", Item: []PostmanItem{}},
	}

	// Parse Routes
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

		// 1. Smart Query Parameters
		rawURL := "http://127.0.0.1:1040" + route.Path
		var queryParts []string
		var queries []PostmanQuery

		if strings.HasPrefix(route.Path, "/api/in/posts") && !strings.HasSuffix(route.Path, "/results") {
			queries = append(queries, PostmanQuery{Key: "workspace_id", Value: "{{workspace_id}}", Description: "Required: Workspace UUID"})
			queryParts = append(queryParts, "workspace_id={{workspace_id}}")
		}
		if route.Path == "/api/in/posts" && route.Method == "GET" {
			queries = append(queries,
				PostmanQuery{Key: "status", Value: "draft", Description: "Filter: draft, scheduled, publishing, published, failed"},
				PostmanQuery{Key: "page", Value: "0", Description: "Page number"},
				PostmanQuery{Key: "limit", Value: "20", Description: "Results per page"},
			)
			queryParts = append(queryParts, "status=draft", "page=0", "limit=20")
		}
		if strings.HasSuffix(route.Path, "/results") && route.Method == "GET" {
			queries = append(queries, PostmanQuery{Key: "attempt_id", Value: "{{attempt_id}}", Description: "Required: Tracking ID from /publish"})
			queryParts = append(queryParts, "attempt_id={{attempt_id}}")
		}
		if strings.Contains(route.Path, "/x/") && route.Method == "GET" && !strings.HasSuffix(route.Path, "/connections") {
			queries = append(queries, PostmanQuery{Key: "connection_id", Value: "{{connection_id}}", Description: "Optional: Specific X connection ID"})
			queryParts = append(queryParts, "connection_id={{connection_id}}")
		}
		if len(queryParts) > 0 {
			rawURL += "?" + strings.Join(queryParts, "&")
		}

		// 2. Headers
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

		// 3. Build Request Item
		item := PostmanItem{
			Name: fmt.Sprintf("[%s] %s", route.Method, route.Path),
			Request: &PostmanRequest{
				Method:      route.Method,
				Header:      headers,
				Body:        getRequestBody(route.Path, route.Method),
				Description: getDescription(route.Path, route.Method),
				Event:       getEvents(route.Path, route.Method),
				Url: PostmanUrl{
					Raw:   rawURL,
					Host:  []string{"http://127.0.0.1:1040"},
					Path:  cleanPath,
					Query: queries,
				},
			},
		}

		// 4. Assign to Folder
		folderKey := getFolderKey(route.Path)
		folders[folderKey].Item = append(folders[folderKey].Item, item)
	}

	// Append folders in logical order
	orderedKeys := []string{"auth", "workspaces", "connections", "posts", "x_read", "system"}
	for _, key := range orderedKeys {
		if folder, ok := folders[key]; ok && len(folder.Item) > 0 {
			collection.Item = append(collection.Item, *folder)
		}
	}

	// Write to file
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

// ──────────────────────────────────────────────────────
// Helpers: Folders, Bodies, Descriptions, Scripts
// ──────────────────────────────────────────────────────

func createFullAPIDocs(markdown string) PostmanItem {
	return PostmanItem{
		Name:        "📖 Full API Documentation",
		Description: "Complete API reference, domain models, and auth flows.",
		Item: []PostmanItem{
			{
				Name: "📚 Read Full API Docs (Markdown)",
				Request: &PostmanRequest{
					Method: "GET",
					Url: PostmanUrl{
						Raw:  "http://127.0.0.1:1040/health",
						Host: []string{"http://127.0.0.1:1040"},
						Path: []string{"health"},
					},
					Description: markdown,
				},
			},
		},
	}
}

func createRefreshReauthingDocs(markdown string) PostmanItem {
	return PostmanItem{
		Name:        "🔄 Background Token Management",
		Description: "Auto-refresh and re-authentication flows for Meta and LinkedIn.",
		Item: []PostmanItem{
			{
				Name: "📚 Read Token Management Docs (Markdown)",
				Request: &PostmanRequest{
					Method: "GET",
					Url: PostmanUrl{
						Raw:  "http://127.0.0.1:1040/health",
						Host: []string{"http://127.0.0.1:1040"},
						Path: []string{"health"},
					},
					Description: markdown,
				},
			},
		},
	}
}

func getFolderKey(path string) string {
	if strings.Contains(path, "/auth/") || path == "/api/auth/signin" {
		return "auth"
	}
	if strings.Contains(path, "/posts") {
		return "posts"
	}
	if strings.Contains(path, "/x/") {
		return "x_read"
	}
	if strings.Contains(path, "/connections") || strings.Contains(path, "/connect/") {
		return "connections"
	}
	if strings.Contains(path, "/workspaces") {
		return "workspaces"
	}
	return "system"
}

func getRequestBody(path, method string) *PostmanBody {
	raw := ""
	switch {
	case path == "/api/auth/register/email" && method == "POST":
		raw = `{ "mail": "user@example.com" }`
	case path == "/api/auth/register/email/activate" && method == "POST":
		raw = `{ "mail": "user@example.com", "authCode": "123456" }`
	case path == "/api/auth/register/complete" && method == "POST":
		raw = "{\n  \"mail\": \"user@example.com\",\n  \"fname\": \"John\",\n  \"lname\": \"Doe\",\n  \"phone\": \"1234567890\",\n  \"pword\": \"Password123!\",\n  \"cpword\": \"Password123!\",\n  \"username\": \"johndoe99\",\n  \"gender\": \"male\"\n}"
	case path == "/api/auth/signin" && method == "POST":
		raw = `{ "mail": "user@example.com", "pword": "Password123!" }`
	case path == "/api/in/workspaces" && method == "POST":
		raw = `{ "name": "My New Workspace" }`
	case strings.HasSuffix(path, "/members") && method == "POST":
		raw = `{ "email": "teammate@example.com", "role": "admin" }`
	case path == "/api/in/posts" && method == "POST":
		raw = "{\n  \"workspace_id\": \"{{workspace_id}}\",\n  \"content\": \"Hello world from Reni Social! 🚀\",\n  \"media_urls\": [],\n  \"platforms\": [\"linkedin\", \"x\"],\n  \"article_url\": \"\",\n  \"article_title\": \"\",\n  \"scheduled_at\": null\n}"
	case strings.Contains(path, "/posts/in/") && method == "PUT":
		raw = "{\n  \"content\": \"Updated post content!\",\n  \"media_urls\": [],\n  \"platforms\": [\"linkedin\"],\n  \"article_url\": null,\n  \"article_title\": null,\n  \"scheduled_at\": null\n}"
	case strings.HasSuffix(path, "/schedule") && method == "POST":
		raw = "{\n  \"scheduled_at\": \"2026-12-31T23:59:59Z\",\n  \"platforms\": [\"linkedin\", \"meta_ig\"]\n}"
	}

	if raw != "" {
		return &PostmanBody{
			Mode: "raw",
			Raw:  raw,
			Options: map[string]interface{}{
				"raw": map[string]string{"language": "json"},
			},
		}
	}
	return nil
}

func getDescription(path, method string) string {
	switch {
	case path == "/api/auth/signin" && method == "POST":
		return "Logs the user in via Reni Central and creates a local session.\n\n**🤖 Auto-Save:** The test script automatically saves the `session_id` to your Collection Variables!"
	case path == "/api/in/workspaces" && method == "POST":
		return "Creates a new workspace and adds the caller as the owner.\n\n**🤖 Auto-Save:** The test script automatically saves the new `workspace_id` to your Collection Variables!"
	case path == "/api/in/posts" && method == "POST":
		return "Creates a new draft or scheduled post.\n\n- `platforms`: Array of strings (`linkedin`, `meta_ig`, `meta_fb`, `x`, `tiktok`)\n- `scheduled_at`: ISO8601 string or `null` for draft."
	case strings.HasSuffix(path, "/publish") && method == "POST":
		return "### 🚀 Async Publish Trigger\n\nThis endpoint does **not** block. It initiates the publish process and immediately returns an `attempt_id`.\n\n**🤖 Auto-Save:** The test script automatically saves the `attempt_id` so you can immediately test the `/results` endpoint!"
	case strings.HasSuffix(path, "/results") && method == "GET":
		return "### ⏱️ Real-Time Status Polling\n\nPass the `?attempt_id={{attempt_id}}` query parameter to track the exact status of a specific publish click.\n\n**Statuses:**\n- `processing`: Platform is still working (show spinner)\n- `published`: Success (show checkmark)\n- `failed`: Error (show `fail_reason`)"
	case strings.Contains(path, "/connect/in/") && method == "POST":
		return "Initiates the OAuth flow for a platform.\n\nReturns a `redirect_url` that the frontend should open in a new window/popup."
	}
	return ""
}

func getEvents(path, method string) []PostmanEvent {
	var events []PostmanEvent

	if path == "/api/auth/signin" && method == "POST" {
		events = append(events, PostmanEvent{
			Listen: "test",
			Script: struct {
				Exec []string `json:"exec"`
				Type string   `json:"type"`
			}{
				Type: "text/javascript",
				Exec: []string{
					"var jsonData = pm.response.json();",
					"if (jsonData.status === true && jsonData.data && jsonData.data.token) {",
					"    pm.collectionVariables.set(\"session_id\", jsonData.data.token);",
					"    pm.collectionVariables.set(\"token\", \"dummy_token\");",
					"    console.log(\"✅ Saved session_id and dummy token to collection variables!\");",
					"}",
				},
			},
		})
	}

	if path == "/api/in/workspaces" && method == "POST" {
		events = append(events, PostmanEvent{
			Listen: "test",
			Script: struct {
				Exec []string `json:"exec"`
				Type string   `json:"type"`
			}{
				Type: "text/javascript",
				Exec: []string{
					"var jsonData = pm.response.json();",
					"if (jsonData.status === true && jsonData.data && jsonData.data.id) {",
					"    pm.collectionVariables.set(\"workspace_id\", jsonData.data.id);",
					"    console.log(\"✅ Saved workspace_id to collection variables!\");",
					"}",
				},
			},
		})
	}

	if strings.HasSuffix(path, "/publish") && method == "POST" {
		events = append(events, PostmanEvent{
			Listen: "test",
			Script: struct {
				Exec []string `json:"exec"`
				Type string   `json:"type"`
			}{
				Type: "text/javascript",
				Exec: []string{
					"var jsonData = pm.response.json();",
					"if (jsonData.attempt_id) {",
					"    pm.collectionVariables.set(\"attempt_id\", jsonData.attempt_id);",
					"    console.log(\"✅ Saved attempt_id to collection variables!\");",
					"}",
				},
			},
		})
	}

	return events
}

func createWorkflowGuide() PostmanItem {
	md := "# 🔄 Real-Time Publishing Workflow\n\n" +
		"When a user clicks \"Publish\", the backend processes platforms asynchronously (especially Instagram/TikTok videos).\n\n" +
		"### How to use this guide:\n" +
		"1. Click **Step 1** below to see the exact Request and Expected Response.\n" +
		"2. Click **Step 2** to see how to poll for the results.\n" +
		"3. Use the **Examples** dropdown in the top right of Postman to see the exact JSON shapes!"

	step1Req := &PostmanRequest{
		Method: "POST",
		Header: []PostmanHeader{
			{Key: "Authorization", Value: "Bearer {{token}}", Type: "text"},
			{Key: "X-Session-Id", Value: "{{session_id}}", Type: "text"},
		},
		Url: PostmanUrl{
			Raw:   "http://127.0.0.1:1040/api/in/posts/in/{{post_id}}/publish?workspace_id={{workspace_id}}",
			Host:  []string{"http://127.0.0.1:1040"},
			Path:  []string{"api", "in", "posts", "in", "{{post_id}}", "publish"},
			Query: []PostmanQuery{{Key: "workspace_id", Value: "{{workspace_id}}", Description: "Required"}},
		},
		Description: "### Step 1: Trigger the Publish\n\nThis endpoint does **not** block. It initiates the publish process and immediately returns an `attempt_id`.\n\n**Frontend Action:** Save the `attempt_id` in your component state and immediately start polling Step 2.",
	}

	step1Body := `{
    "message": "publishing_initiated",
    "attempt_id": "8f4e2d1c-9b3a-4e5f-8c7d-6e5f4a3b2c1d"
}`

	step2Req := &PostmanRequest{
		Method: "GET",
		Header: []PostmanHeader{
			{Key: "Authorization", Value: "Bearer {{token}}", Type: "text"},
			{Key: "X-Session-Id", Value: "{{session_id}}", Type: "text"},
		},
		Url: PostmanUrl{
			Raw:   "http://127.0.0.1:1040/api/in/posts/in/{{post_id}}/results?attempt_id={{attempt_id}}",
			Host:  []string{"http://127.0.0.1:1040"},
			Path:  []string{"api", "in", "posts", "in", "{{post_id}}", "results"},
			Query: []PostmanQuery{{Key: "attempt_id", Value: "{{attempt_id}}", Description: "Required: The tracking ID returned by Step 1"}},
		},
		Description: "### Step 2: Poll for Real-Time Status\n\n**Frontend Action:** Start a `setInterval` (every 2 seconds) calling this endpoint using the `attempt_id` from Step 1.\n\n**UI Mapping:**\n" +
			"* `status === 'processing'` 👉 Show a spinner.\n" +
			"* `status === 'published'` 👉 Show a green checkmark + link.\n" +
			"* `status === 'failed'` 👉 Show a red X and display the `fail_reason`.\n\n" +
			"**Stop Polling:** When ALL platforms in the array reach `published` or `failed`.",
	}

	step2Body := `{
    "results": [
        {
            "id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
            "platform": "linkedin",
            "status": "published",
            "platform_post_id": "urn:li:share:7123456789",
            "platform_post_url": "https://www.linkedin.com/feed/update/urn:li:share:7123456789",
            "container_id": "",
            "fail_reason": "",
            "created_at": "2026-08-08T10:00:00Z",
            "updated_at": "2026-08-08T10:00:05Z"
        },
        {
            "id": "b2c3d4e5-f6a7-8901-bcde-f12345678901",
            "platform": "meta_ig",
            "status": "processing",
            "platform_post_id": "",
            "platform_post_url": "",
            "container_id": "17954998877665544",
            "fail_reason": "",
            "created_at": "2026-08-08T10:00:00Z",
            "updated_at": "2026-08-08T10:00:00Z"
        },
        {
            "id": "c3d4e5f6-a7b8-9012-cdef-123456789012",
            "platform": "x",
            "status": "failed",
            "platform_post_id": "",
            "platform_post_url": "",
            "container_id": "",
            "fail_reason": "x: tweet failed (403): Forbidden",
            "created_at": "2026-08-08T10:00:00Z",
            "updated_at": "2026-08-08T10:00:02Z"
        }
    ]
}`

	return PostmanItem{
		Name:        "📚 API Workflows & Guides",
		Description: "Mock requests showing the exact expected inputs and outputs for complex multi-step workflows.",
		Item: []PostmanItem{
			{
				Name:        "📖 Read Me: Real-Time Publishing Flow",
				Description: md,
				Request: &PostmanRequest{
					Method: "GET",
					Url: PostmanUrl{
						Raw:  "http://127.0.0.1:1040/health",
						Host: []string{"http://127.0.0.1:1040"},
						Path: []string{"health"},
					},
				},
			},
			{
				Name:    "Step 1: Trigger Publish (Expected Output)",
				Request: step1Req,
				Response: []PostmanResponse{
					{
						Name:   "Success: Publishing Initiated",
						Status: "OK",
						Code:   200,
						Header: []PostmanHeader{{Key: "Content-Type", Value: "application/json", Type: "text"}},
						Body:   step1Body,
					},
				},
			},
			{
				Name:    "Step 2: Poll Results (Expected Output)",
				Request: step2Req,
				Response: []PostmanResponse{
					{
						Name:   "Success: Mixed Status (Processing, Published, Failed)",
						Status: "OK",
						Code:   200,
						Header: []PostmanHeader{{Key: "Content-Type", Value: "application/json", Type: "text"}},
						Body:   step2Body,
					},
				},
			},
		},
	}
}