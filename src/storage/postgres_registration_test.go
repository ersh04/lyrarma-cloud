package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/lyrarma/cloud-api/src/models"
)

type registrationRow func(...any) error

func (r registrationRow) Scan(values ...any) error { return r(values...) }

type registrationTransaction struct {
	pgx.Tx
	usernameExists bool
	emailExists    bool
	cleanupErr     error
	lookupErr      error
	insertErr      error
	statements     []string
	insertArgs     []any
}

type confirmationTransaction struct {
	pgx.Tx
	valid      bool
	lookupErr  error
	insertErr  error
	commitErr  error
	commits    int
	rollbacks  int
	insertArgs []any
	lookupArgs []any
	statements []string
}

func (tx *confirmationTransaction) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	tx.lookupArgs = args
	tx.statements = append(tx.statements, sql)
	return registrationRow(func(values ...any) error {
		if sql != consumePendingRegistrationSQL {
			return errors.New("unexpected confirmation lookup")
		}
		if tx.lookupErr != nil {
			return tx.lookupErr
		}
		*(values[0].(*string)) = "alice"
		*(values[1].(*string)) = "alice@example.test"
		*(values[2].(*string)) = "existing-bcrypt-hash"
		*(values[3].(*[]string)) = []string{"user"}
		*(values[4].(*bool)) = tx.valid
		return nil
	})
}

func (tx *confirmationTransaction) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.statements = append(tx.statements, sql)
	if sql != insertAuthUserSQL {
		return pgconn.CommandTag{}, errors.New("unexpected confirmation insert")
	}
	tx.insertArgs = args
	return pgconn.NewCommandTag("INSERT 0 1"), tx.insertErr
}

func (tx *confirmationTransaction) Commit(context.Context) error   { tx.commits++; return tx.commitErr }
func (tx *confirmationTransaction) Rollback(context.Context) error { tx.rollbacks++; return nil }

func TestConfirmPendingRegistration(t *testing.T) {
	const key = "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	for _, test := range []struct {
		name       string
		tx         confirmationTransaction
		planErr    error
		wantErr    error
		wantCommit bool
		wantInsert bool
	}{
		{name: "success", tx: confirmationTransaction{valid: true}, wantCommit: true, wantInsert: true},
		{name: "missing or already consumed", tx: confirmationTransaction{lookupErr: pgx.ErrNoRows}, wantErr: ErrRegistrationNotFound},
		{name: "expired is deleted", wantErr: ErrRegistrationExpired, wantCommit: true},
		{name: "lookup failure", tx: confirmationTransaction{lookupErr: errors.New("lookup failure")}},
		{name: "invalid plan restores request", tx: confirmationTransaction{valid: true}, planErr: errors.New("unknown plan")},
		{name: "insert failure restores request", tx: confirmationTransaction{valid: true, insertErr: errors.New("insert failure")}, wantInsert: true},
		{name: "username conflict", tx: confirmationTransaction{valid: true, insertErr: &pgconn.PgError{Code: "23505", ConstraintName: "users_username_unique"}}, wantInsert: true, wantErr: ErrUserAlreadyExists},
		{name: "email conflict", tx: confirmationTransaction{valid: true, insertErr: &pgconn.PgError{Code: "23505", ConstraintName: "users_email_unique"}}, wantInsert: true, wantErr: ErrEmailAlreadyUsed},
		{name: "commit failure", tx: confirmationTransaction{valid: true, commitErr: errors.New("commit failure")}, wantInsert: true, wantCommit: true},
		{name: "expired cleanup commit failure", tx: confirmationTransaction{commitErr: errors.New("commit failure")}, wantCommit: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := test.tx
			user, err := confirmPendingRegistration(context.Background(), func(context.Context) (pgx.Tx, error) { return &tx, nil }, key, func(plans []string) error {
				if !reflect.DeepEqual(plans, []string{"user"}) {
					t.Error("incorrect stored plan validation")
				}
				return test.planErr
			})
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("got %v, want %v", err, test.wantErr)
				}
			} else {
				wantFailure := test.tx.lookupErr != nil || test.tx.insertErr != nil || test.tx.commitErr != nil || test.planErr != nil
				if (err != nil) != wantFailure {
					t.Fatalf("unexpected confirmation result: %v", err)
				}
			}
			if (tx.commits == 1) != test.wantCommit || tx.rollbacks != 1 || (tx.insertArgs != nil) != test.wantInsert {
				t.Fatal("incorrect confirmation transaction lifecycle")
			}
			digest := sha256.Sum256([]byte(key))
			if !reflect.DeepEqual(tx.lookupArgs, []any{hex.EncodeToString(digest[:])}) {
				t.Fatal("confirmation lookup must use the key hash")
			}
			if len(tx.statements) == 0 || tx.statements[0] != consumePendingRegistrationSQL {
				t.Fatal("confirmation must consume its request inside the account transaction")
			}
			if err == nil {
				if len(user.ID) != 32 || user.Username != "alice" || user.Password != "existing-bcrypt-hash" || user.Email != "alice@example.test" {
					t.Fatal("incorrect confirmed account")
				}
				if !reflect.DeepEqual(tx.insertArgs, []any{user.ID, user.Username, user.Password, user.Email, user.Plan}) {
					t.Fatal("confirmed account must retain the stored password hash and plan")
				}
			} else if user.ID != "" {
				t.Fatal("failed confirmation must not return an authenticated user")
			}
		})
	}
}

func TestConfirmPendingRegistrationMalformedKey(t *testing.T) {
	for _, key := range []string{"", "abc", strings.Repeat("z", 64), strings.Repeat("a", 62), strings.Repeat("a", 66)} {
		_, err := confirmPendingRegistration(context.Background(), func(context.Context) (pgx.Tx, error) {
			t.Fatal("invalid keys must not query the database")
			return nil, nil
		}, key, nil)
		if !errors.Is(err, ErrRegistrationNotFound) {
			t.Fatalf("unexpected malformed key error: %v", err)
		}
	}
}

func (tx *registrationTransaction) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.statements = append(tx.statements, sql)
	switch sql {
	case deleteExpiredRegistrationsSQL:
		return pgconn.NewCommandTag("DELETE 1"), tx.cleanupErr
	case insertPendingRegistrationSQL:
		tx.insertArgs = args
		return pgconn.NewCommandTag("INSERT 0 1"), tx.insertErr
	default:
		return pgconn.CommandTag{}, errors.New("unexpected SQL statement")
	}
}

func (tx *registrationTransaction) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	tx.statements = append(tx.statements, sql)
	return registrationRow(func(values ...any) error {
		if sql != selectRegisteredIdentitySQL {
			return errors.New("unexpected SQL query")
		}
		*(values[0].(*bool)) = tx.usernameExists
		*(values[1].(*bool)) = tx.emailExists
		return tx.lookupErr
	})
}

func TestInsertPendingRegistration(t *testing.T) {
	now := time.Now().UTC()
	request := models.PendingRegistration{
		Username: "alice", Email: "alice@example.com", PasswordHash: "bcrypt-hash", Plan: []string{"user"},
		ConfirmationKey: "secret-confirmation-key", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
	}
	for _, test := range []struct {
		name       string
		tx         registrationTransaction
		wantErr    error
		wantInsert bool
	}{
		{name: "success", wantInsert: true},
		{name: "existing user", tx: registrationTransaction{usernameExists: true}, wantErr: ErrUserAlreadyExists},
		{name: "existing email", tx: registrationTransaction{emailExists: true}, wantErr: ErrEmailAlreadyUsed},
		{name: "pending username conflict", tx: registrationTransaction{insertErr: &pgconn.PgError{Code: "23505", ConstraintName: "registration_requests_username_unique"}}, wantErr: ErrUserAlreadyExists, wantInsert: true},
		{name: "pending email conflict", tx: registrationTransaction{insertErr: &pgconn.PgError{Code: "23505", ConstraintName: "registration_requests_email_unique"}}, wantErr: ErrEmailAlreadyUsed, wantInsert: true},
		{name: "key collision", tx: registrationTransaction{insertErr: &pgconn.PgError{Code: "23505", ConstraintName: "registration_requests_pkey"}}, wantInsert: true},
		{name: "cleanup error", tx: registrationTransaction{cleanupErr: errors.New("cleanup failed")}},
		{name: "lookup error", tx: registrationTransaction{lookupErr: errors.New("lookup failed")}},
		{name: "insert error", tx: registrationTransaction{insertErr: errors.New("write failed")}, wantInsert: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := test.tx
			err := insertPendingRegistration(context.Background(), &tx, request)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("got %v, want %v", err, test.wantErr)
				}
			} else if (err != nil) != (test.tx.cleanupErr != nil || test.tx.lookupErr != nil || test.tx.insertErr != nil) {
				t.Fatalf("unexpected storage result: %v", err)
			}
			if (tx.insertArgs != nil) != test.wantInsert {
				t.Fatal("unexpected pending registration insert")
			}
			if tx.statements[0] != deleteExpiredRegistrationsSQL {
				t.Fatal("expired reservations must be removed before checking identity availability")
			}
			if test.wantInsert {
				digest := sha256.Sum256([]byte(request.ConfirmationKey))
				want := []any{hex.EncodeToString(digest[:]), request.Username, request.Email, request.PasswordHash, request.Plan, request.CreatedAt, request.ExpiresAt}
				if !reflect.DeepEqual(tx.insertArgs, want) {
					t.Fatal("pending registration must persist the hashed key and registration data")
				}
			}
		})
	}
}
