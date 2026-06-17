package db

import (
	"database/sql"
	"log"
	"time"

	"os"

	_ "github.com/lib/pq"
)

var DB *sql.DB

func Connect() {
	var err error

	dsn := os.Getenv("DBURL")

	DB, err = sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// ðŸ”¹ Connection pool configuration
	DB.SetMaxOpenConns(50)           // maximum total connections
	DB.SetMaxIdleConns(25)           // idle connections kept ready
	DB.SetConnMaxLifetime(time.Hour) // recreate connections every 1 hour

	// ðŸ”¹ Verify connection
	err = DB.Ping()
	if err != nil {
		log.Fatal("Failed to ping database:", err)
	}

	log.Println("+ Database connected")

	// ðŸ”¹ Debug: print connection pool stats
	stats := DB.Stats()

	log.Println("DB Pool Stats")
	log.Println("Open connections:", stats.OpenConnections)
	log.Println("Idle connections:", stats.Idle)
	log.Println("In use connections:", stats.InUse)
	log.Println("Wait count:", stats.WaitCount)
}

func PrintPoolStats() {
	stats := DB.Stats()

	log.Println("ðŸ“Š DB Pool Stats")
	log.Println("Open:", stats.OpenConnections)
	log.Println("Idle:", stats.Idle)
	log.Println("InUse:", stats.InUse)
	log.Println("WaitCount:", stats.WaitCount)
}
