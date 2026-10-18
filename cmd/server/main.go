package main

import (
	"log"
	"net/http"
	"os"

	"ScaleOfCoherence/internal/api"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/database/repository"
	"ScaleOfCoherence/internal/items"
	"ScaleOfCoherence/internal/teams"
	"ScaleOfCoherence/internal/workflows"
)

func main() {
	db := initializeDatabase()

	secret := "fdcc8fb2d2108c452015b3bf13f1963e" //os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET is required")
	}
	authSvc := auth.NewService(repository.NewAuthRepo(db.DB()), secret)
	teamSvc := teams.NewService(repository.NewTeamRepo(db.DB()))
	itemSvc := items.NewService(repository.NewItemRepo(db.DB()))
	workflowSvc := workflows.NewService(repository.NewWorkflowRepo(db.DB()))

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	log.Printf("listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, api.NewRouter(authSvc, teamSvc, itemSvc, workflowSvc)))
}
