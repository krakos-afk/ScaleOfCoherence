package main

import (
	"log"

	"ScaleOfCoherence/internal/database"
)

func initializeDatabase() *database.DatabasePG {
	db, err := database.Connect()
	if err != nil {
		log.Fatal(err)
	}

	if err := db.Migrate(); err != nil {
		log.Fatal(err)
	}
	return db
}
