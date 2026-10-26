package storage

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lyrarma/cloud-api/src/models"
)

func TestPostgresRegistrationConfirmation(t *testing.T) {
	databaseURL := os.Getenv("LYRARMA_TEST_USERS_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set LYRARMA_TEST_USERS_DATABASE_URL to run PostgreSQL confirmation tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	id, err := randomUserID()
	if err != nil {
		t.Fatal(err)
	}
	schema := pgx.Identifier{"confirmation_test_" + id}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "confirmation_test_" + id
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := &UserStore{db: pool, defaultPlan: "user", configuredPlans: []string{"user"}, planNames: map[string]struct{}{"user": {}}}
	if err := store.init(); err != nil {
		t.Fatal(err)
	}
	if err := store.finalizeSchema(ctx); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	request := models.PendingRegistration{Username: "alice", Email: "alice@example.test", PasswordHash: "stored-bcrypt-hash", Plan: []string{"user"}, ConfirmationKey: strings.Repeat("a", 64), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreatePendingRegistration(request); err != nil {
		t.Fatal(err)
	}

	// Concurrent requests must consume one key only once in PostgreSQL itself
	var wg sync.WaitGroup
	results := make(chan error, 16)
	start := make(chan struct{})
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.ConfirmRegistration(request.ConfirmationKey)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrRegistrationNotFound) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("got %d successful concurrent confirmations", succeeded)
	}
	user, err := store.GetUserByUsername("alice")
	if err != nil || user.Email != request.Email || user.Password != request.PasswordHash || len(user.ID) != 32 {
		t.Fatal("confirmed PostgreSQL account does not match its pending registration")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM registration_requests").Scan(&count); err != nil || count != 0 {
		t.Fatal("confirmed request was not consumed")
	}

	// Expired keys are removed without creating an account
	request.Username = "expired"
	request.Email = "expired@example.test"
	request.ConfirmationKey = strings.Repeat("b", 64)
	request.CreatedAt = now.Add(-2 * time.Hour)
	request.ExpiresAt = now.Add(-time.Hour)
	if err := store.CreatePendingRegistration(request); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmRegistration(request.ConfirmationKey); !errors.Is(err, ErrRegistrationExpired) {
		t.Fatalf("expired key result: %v", err)
	}
	if _, err := store.GetUserByUsername("expired"); !errors.Is(err, ErrUserNotFound) {
		t.Fatal("expired key created an account")
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM registration_requests").Scan(&count); err != nil || count != 0 {
		t.Fatal("expired request was not removed")
	}

	// Account insertion errors must restore the pending request for a later retry
	request.Username = "conflict"
	request.Email = "pending@example.test"
	request.ConfirmationKey = strings.Repeat("c", 64)
	request.CreatedAt = now
	request.ExpiresAt = now.Add(time.Hour)
	if err := store.CreatePendingRegistration(request); err != nil {
		t.Fatal(err)
	}
	existing := models.User{ID: "existing-account", Username: "conflict", Email: "existing@example.test", Password: "existing-hash", Plan: []string{"user"}}
	if err := store.CreateUser(existing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmRegistration(request.ConfirmationKey); !errors.Is(err, ErrUserAlreadyExists) {
		t.Fatalf("identity conflict result: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM registration_requests WHERE username = 'conflict'").Scan(&count); err != nil || count != 1 {
		t.Fatal("failed account creation lost the pending request")
	}
	if err := store.DeleteUser(existing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConfirmRegistration(request.ConfirmationKey); err != nil {
		t.Fatalf("confirmation retry after rollback failed: %v", err)
	}
}
