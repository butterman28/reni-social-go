package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings" // <-- Added for HasPrefix
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/swagger"

	"reni-social/internal/middleware"
	"reni-social/internal/routes"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db"
)

// @title Reni Social API
// @version 1.0
// @description Reni Social backend API
// @BasePath /
//
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
//
// @securityDefinitions.apikey SessionId
// @in header
// @name X-Session-Id

func main() {
	os.Setenv("DBURL", config.C.DBURL)
	db.Connect() // connect to DB

	//fmt.Println(" DEBUG: LinkedIn Client ID is:", config.C.LinkedInClientID)


	// Initialize Redis
	utils.InitRedis()

	app := fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.0.0.0/0"}, // trust all proxies
		BodyLimit:               100 * 1024 * 1024,     // Set to 100MB
	})
	app.Get("/swagger/*", swagger.HandlerDefault) 

	log.Fatal(app.Listen(":3000"))
	app.Use(logger.New())

	app.Use(cors.New(cors.Config{
		// Use a function to dynamically validate origins
		AllowOriginsFunc: func(origin string) bool {
			// 1. Allow standard local development origins
			if origin == "http://localhost:5173" || origin == "http://127.0.0.1:5173" {
				return true
			}
			// 2. Allow any device on the 192.168.1.x local network (with or without a port)
			if strings.HasPrefix(origin, "http://192.168.1.") {
				return true
			}
			// Deny all other origins
			return false
		},
		AllowOrigins: "http://localhost:5173,http://localhost:5173/,https://renisocial.stories.ng",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Session-ID, timezone, locale, ip",
		// AllowCredentials: true, // Uncomment this if your frontend sends cookies or auth headers
	}))

	app.Use(recover.New())
	// Apply global rate limiting (e.g., max 60 requests/minute per client)
    app.Use(middleware.RedisRateLimiter(300, 1*time.Minute))
	app.Options("/*", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})

	routes.Setup(app)

	go func() {
		if config.C.USE_TLS {
			err := app.ListenTLS(
				":"+config.C.Port,
				config.C.CERT_FILE,
				config.C.KEY_FILE,
			)
			if err != nil {
				fmt.Println(err)
			}
		} else {
			err := app.Listen(":" + config.C.Port)
			if err != nil {
				fmt.Println(err)
			}
		}
	}()

	// Create a channel to listen for OS signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	_ = <-c // This blocks main until a signal is received
	fmt.Println("Gracefully shutting down...")
	_ = app.Shutdown()
}