package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestIdempotencyClaimLifecycle(t *testing.T) {
	resetDB(t)
	orgID := seedOrg(t, "org-"+randName())
	userID := seedUser(t, orgID, "user-"+randName(), "admin")
	repo := NewIdempotencyRepo(testDB)
	ctx := context.Background()

	const key = "key-lifecycle"
	const hash = "hash-1"

	stored, err := repo.Claim(ctx, orgID, userID, key, hash)
	if err != nil || stored != nil {
		t.Fatalf("fresh claim = (%+v, %v), want (nil, nil)", stored, err)
	}

	if _, err := repo.Claim(ctx, orgID, userID, key, hash); !errors.Is(err, ErrIdempotencyInProgress) {
		t.Fatalf("second claim err = %v, want ErrIdempotencyInProgress", err)
	}

	if err := repo.Complete(ctx, orgID, userID, key, 201, []byte(`{"id":"x"}`), "application/json"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	replay, err := repo.Claim(ctx, orgID, userID, key, hash)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if replay == nil || replay.Status != 201 || string(replay.Body) != `{"id":"x"}` || replay.ContentType != "application/json" {
		t.Fatalf("replay = %+v", replay)
	}

	if _, err := repo.Claim(ctx, orgID, userID, key, "other-hash"); !errors.Is(err, ErrIdempotencyMismatch) {
		t.Fatalf("mismatch claim err = %v, want ErrIdempotencyMismatch", err)
	}

	if err := repo.Delete(ctx, orgID, userID, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := repo.Claim(ctx, orgID, userID, key, hash); err != nil {
		t.Fatalf("claim after delete: %v", err)
	}
}

func TestIdempotencyClaimConcurrency(t *testing.T) {
	resetDB(t)
	orgID := seedOrg(t, "org-"+randName())
	userID := seedUser(t, orgID, "user-"+randName(), "admin")
	repo := NewIdempotencyRepo(testDB)
	ctx := context.Background()

	const workers = 8
	const key = "key-race"
	results := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Claim(ctx, orgID, userID, key, "same-hash")
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	fresh, inProgress, other := 0, 0, 0
	for err := range results {
		switch {
		case err == nil:
			fresh++
		case errors.Is(err, ErrIdempotencyInProgress):
			inProgress++
		default:
			other++
			t.Errorf("unexpected claim error: %v", err)
		}
	}
	if fresh != 1 || inProgress != workers-1 || other != 0 {
		t.Fatalf("fresh=%d inProgress=%d other=%d, want 1/%d/0", fresh, inProgress, workers-1, other)
	}
}

func TestIdempotencyKeysAreScopedPerOrgAndUser(t *testing.T) {
	resetDB(t)
	orgA := seedOrg(t, "org-a-"+randName())
	orgB := seedOrg(t, "org-b-"+randName())
	userA := seedUser(t, orgA, "user-a-"+randName(), "admin")
	userB := seedUser(t, orgA, "user-b-"+randName(), "user")
	userC := seedUser(t, orgB, "user-c-"+randName(), "admin")
	repo := NewIdempotencyRepo(testDB)
	ctx := context.Background()

	const key = "shared-key"
	if _, err := repo.Claim(ctx, orgA, userA, key, "h"); err != nil {
		t.Fatalf("org A user A: %v", err)
	}
	// Same key, same org, different user: independent.
	if _, err := repo.Claim(ctx, orgA, userB, key, "h"); err != nil {
		t.Fatalf("org A user B: %v", err)
	}
	// Same key, different org: independent.
	if _, err := repo.Claim(ctx, orgB, userC, key, "h"); err != nil {
		t.Fatalf("org B user C: %v", err)
	}
	// Original key is still in progress and untouched by the others.
	if _, err := repo.Claim(ctx, orgA, userA, key, "h"); !errors.Is(err, ErrIdempotencyInProgress) {
		t.Fatalf("original claim = %v, want in progress", err)
	}
}

func TestIdempotencyDeleteExpired(t *testing.T) {
	resetDB(t)
	orgID := seedOrg(t, "org-"+randName())
	userID := seedUser(t, orgID, "user-"+randName(), "admin")
	repo := NewIdempotencyRepo(testDB)
	ctx := context.Background()

	if _, err := repo.Claim(ctx, orgID, userID, "old-key", "h"); err != nil {
		t.Fatalf("claim old: %v", err)
	}
	if _, err := repo.Claim(ctx, orgID, userID, "new-key", "h"); err != nil {
		t.Fatalf("claim new: %v", err)
	}
	if _, err := testDB.Exec(`
		UPDATE idempotency_keys SET created_at = now() - interval '25 hours'
		WHERE key = 'old-key'`); err != nil {
		t.Fatalf("backdate: %v", err)
	}

	n, err := repo.DeleteExpired(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted %d rows, want 1", n)
	}

	var remaining int
	if err := testDB.QueryRow(`SELECT count(*) FROM idempotency_keys WHERE key = 'new-key'`).Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 1 {
		t.Fatal("recent key was deleted")
	}
}
