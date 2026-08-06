// cmd/social-test/main.go
//
// Single entry point for manual platform-adapter tests, replacing the old
// cmd/linkedin-test, cmd/meta-test, cmd/x-test directories. Each flow is
// just as isolated as before (only imports its own platform package +
// internal/domain + pkg/config — never touches DB/Redis/Fiber), just
// dispatched from one binary via a flag instead of one folder per platform.
//
// Usage:
//
//	go run ./cmd/social-test -platform=linkedin
//	go run ./cmd/social-test -platform=meta
//	go run ./cmd/social-test -platform=x
//	go run ./cmd/social-test -platform=tiktok
//	go run ./cmd/api {to start the service
//	go run ./cmd/schema {to sync PostgreSQL schema tables and ensure database structure:
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	plat := flag.String("platform", "", "which platform to test: linkedin, meta, x, tiktok")
	flag.Parse()

	switch *plat {
	case "linkedin":
		RunLinkedInTest()
	case "meta":
		RunMetaTest()
	case "x":
		RunXTest()
	case "tiktok":
		RunTikTokTest()
	default:
		fmt.Println("Usage: go run ./cmd/social-test -platform=<linkedin|meta|x|tiktok>")
		os.Exit(1)
	}
}
