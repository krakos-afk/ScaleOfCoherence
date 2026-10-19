package api

import (
	"net/http"
	"strings"

	"ScaleOfCoherence/internal/api/controllers"
	"ScaleOfCoherence/internal/auth"
	"ScaleOfCoherence/internal/auth/identity"
	"ScaleOfCoherence/internal/comments"
	"ScaleOfCoherence/internal/items"
	"ScaleOfCoherence/internal/teams"
	"ScaleOfCoherence/internal/workflows"
)

// publicPaths are exact paths that skip authentication: the auth endpoints
// and the probes, which ingress and orchestrators must reach unauthenticated.
var publicPaths = map[string]bool{
	"/auth/login":  true,
	"/auth/signup": true,
	"/healthz":     true,
	"/readyz":      true,
}

func NewRouter(authSvc *auth.Service, teamSvc *teams.Service, itemSvc *items.Service, workflowSvc *workflows.Service, commentSvc *comments.Service, ready Readiness, spaDir string) http.Handler {
	mux := http.NewServeMux()
	ac := controllers.NewAuthController(authSvc)

	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("GET /readyz", handleReady(ready))

	mux.HandleFunc("POST /auth/signup", ac.Signup)
	mux.HandleFunc("POST /auth/login", ac.Login)
	mux.HandleFunc("GET /auth/users", ac.ListUsers)
	mux.HandleFunc("GET /auth/user-names", ac.ListUserNames)
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
	mux.HandleFunc("POST /items/{id}/advance", ic.AdvanceStage)
	mux.HandleFunc("POST /items/{id}/status", ic.SetStatus)
	mux.HandleFunc("GET /items/{id}/timeline", ic.Timeline)
	mux.HandleFunc("GET /items/my", ic.ListMy)
	mux.HandleFunc("GET /items/attention", ic.Attention)
	mux.HandleFunc("GET /items/{id}/activity/open", ic.GetOpenActivity)

	// Item comments.
	cc := controllers.NewCommentController(commentSvc)
	mux.HandleFunc("GET /items/{id}/comments", cc.List)
	mux.HandleFunc("POST /items/{id}/comments", cc.Create)
	mux.HandleFunc("DELETE /items/{id}/comments/{commentID}", cc.Delete)

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

	handler := withSPA(authenticate(authSvc, mux), spaDir)
	return withRequestMeta(logRequests(securityHeaders(limitBody(1 << 20)(handler))))
}

// authenticate gates every route except the public auth endpoints and the
// health probes, then records the caller in the request metadata for logging.
func authenticate(authSvc *auth.Service, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			controllers.WriteError(w, auth.ErrInvalidToken)
			return
		}
		id, err := authSvc.Authenticate(r.Context(), token)
		if err != nil {
			controllers.WriteError(w, err)
			return
		}
		meta := metaFrom(r.Context())
		meta.UserID = id.UserID
		meta.OrgID = id.OrgID
		next.ServeHTTP(w, r.WithContext(identity.WithIdentity(r.Context(), id)))
	})
}
