package main

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"

	"reni-social/internal/routes"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db"

	"os"
	"os/signal"
	"syscall"
)

func main() {
	os.Setenv("DBURL", config.C.DBURL)
	db.Connect() // connect to DB

	// Initialize Redis
	utils.InitRedis()

	// app := fiber.New()
	app := fiber.New(fiber.Config{
		EnableTrustedProxyCheck: true,
		TrustedProxies:          []string{"0.0.0.0/0"}, // trust all proxies
		BodyLimit:               100 * 1024 * 1024,     // Set to 100MB
	})

	app.Use(cors.New(cors.Config{
		AllowOrigins: "http://localhost:5173",
		AllowMethods: "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Session-ID, timezone, locale, ip",
	}))

	app.Use(recover.New())

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
