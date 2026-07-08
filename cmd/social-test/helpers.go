package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"reni-social/internal/platform"
)

func askYesNo(prompt string) bool {
	fmt.Println(prompt)
	fmt.Print("Type 'yes' to proceed, anything else to skip: ")
	var confirm string
	fmt.Scanln(&confirm)
	return strings.ToLower(confirm) == "yes"
}

func promptInput(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}

func printResult(result *platform.PublishResult) {
	fmt.Println("✅ Published successfully!")
	fmt.Printf("   Platform Post ID: %s\n", result.PlatformPostID)
	if result.URL != "" {
		fmt.Printf("   URL: %s\n", result.URL)
	}
}

func printJSON(data map[string]any) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("Error formatting JSON: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

func portFromURL(u *url.URL) string {
	if u.Port() != "" {
		return u.Port()
	}
	if u.Scheme == "https" {
		return "443"
	}
	return "80"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
