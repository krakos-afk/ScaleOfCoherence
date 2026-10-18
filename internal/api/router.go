package api

import (
	"log"
	"net/http"
	"strings"
	"time"

	"ScaleOfCoherence/internal/api/controllers"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/items"
	"ScaleOfCoherence/internal/teams"
	"ScaleOfCoherence/internal/workflows"
)

var publicPaths = map[string]bool{
	"/auth/login":  true,
	"/auth/signup": true,
}

func NewRouter(authSvc *auth.Service, teamSvc *teams.Service, itemSvc *items.Service, workflowSvc *workflows.Service) http.Handler {
	mux := http.NewServeMux()
	ac := controllers.NewAuthController(authSvc)

	mux.HandleFunc("POST /auth/signup", ac.Signup)
	mux.HandleFunc("POST /auth/login", ac.Login)
	mux.HandleFunc("GET /auth/users", ac.ListUsers)
	mux.HandleFunc("POST /auth/users", ac.CreateUser)
	mux.HandleFunc("DELETE /auth/users/{id}", ac.DeleteUser)

	tc := controllers.NewTeamController(teamSvc)
	mux.HandleFunc("POST /teams", tc.Create)
	mux.HandleFunc("GET /teams", tc.List)
	mux.HandleFunc("GET /teams/{id}", tc.Get)
	mux.HandleFunc("PUT /teams/{id}", tc.Update)
	mux.HandleFunc("DELETE /teams/{id}", tc.Delete)
	mux.HandleFunc("GET /teams/{id}/members", tc.ListMembers)
	mux.HandleFunc("POST /teams/{id}/members", tc.AddMember)
	mux.HandleFunc("DELETE /teams/{id}/members/{userID}", tc.RemoveMember)

	ic := controllers.NewItemController(itemSvc)
	mux.HandleFunc("POST /items", ic.Create)
	mux.HandleFunc("GET /items", ic.List)
	mux.HandleFunc("GET /items/{id}", ic.Get)
	mux.HandleFunc("PUT /items/{id}", ic.Update)
	mux.HandleFunc("DELETE /items/{id}", ic.Delete)
	mux.HandleFunc("GET /items/{id}/changes", ic.ListChanges)
	mux.HandleFunc("GET /items/{id}/activity", ic.ListActivity)
	mux.HandleFunc("GET /items/my", ic.ListMy)
	mux.HandleFunc("GET /items/{id}/activity/open", ic.GetOpenActivity)

	// Stage activity lifecycle endpoints.
	mux.HandleFunc("POST /items/{id}/activity/claim", ic.ClaimActivity)
	mux.HandleFunc("POST /items/{id}/activity/complete", ic.CompleteActivity)
	mux.HandleFunc("POST /items/{id}/activity/approve", ic.ApproveActivity)
	mux.HandleFunc("POST /items/{id}/activity/reassign", ic.ReassignActivity)

	wc := controllers.NewWorkflowController(workflowSvc)
	mux.HandleFunc("POST /workflows", wc.Create)
	mux.HandleFunc("GET /workflows", wc.List)
	mux.HandleFunc("GET /workflows/{id}", wc.Get)
	mux.HandleFunc("PUT /workflows/{id}", wc.Update)
	mux.HandleFunc("DELETE /workflows/{id}", wc.Delete)
	mux.HandleFunc("GET /workflows/{id}/stages", wc.ListStages)
	mux.HandleFunc("POST /workflows/{id}/stages", wc.CreateStage)
	mux.HandleFunc("GET /workflows/{id}/stages/{stageID}", wc.GetStage)
	mux.HandleFunc("PUT /workflows/{id}/stages/{stageID}", wc.UpdateStage)
	mux.HandleFunc("DELETE /workflows/{id}/stages/{stageID}", wc.DeleteStage)

	return logRequests(authenticate(authSvc, mux))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}

// authenticate gates every route except the public auth endpoints.
func authenticate(authSvc *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		id, err := authSvc.Authenticate(token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(identity.WithIdentity(r.Context(), id)))
	})
}
