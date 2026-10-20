package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"ScaleOfCoherence/internal/demo"
)

// TestDemoStartupSeedIsIdempotent verifies that the startup path reuses the
// demo workspace instead of adding another item on every server restart.
func TestDemoStartupSeedIsIdempotent(t *testing.T) {
	ts := startAPI(t)
	ctx := context.Background()

	if err := demo.RunHandler(ctx, ts.Config.Handler, io.Discard); err != nil {
		t.Fatalf("first demo seed: %v", err)
	}
	first := countDemoItems(t, ts)
	if first == 0 {
		t.Fatal("first demo seed created no item")
	}

	if err := demo.RunHandler(ctx, ts.Config.Handler, io.Discard); err != nil {
		t.Fatalf("second demo seed: %v", err)
	}
	if second := countDemoItems(t, ts); second != first {
		t.Fatalf("restart created duplicate demo items: before=%d after=%d", first, second)
	}
}

func countDemoItems(t *testing.T, ts *httptest.Server) int {
	t.Helper()
	login := apiClient{t: t, base: ts.URL, xff: "demo-seed-" + unique()}
	token := login.json("POST", "/auth/login", map[string]any{
		"email": "demo-admin@demo.local", "password": "demo-password-123",
	}, http.StatusOK)["token"].(string)
	c := apiClient{t: t, base: ts.URL, token: token, xff: "demo-seed-" + unique()}
	return pageTotal(t, c.json("GET", "/items?q=Ship+the+demo&page=1&limit=100", nil, http.StatusOK))
}
