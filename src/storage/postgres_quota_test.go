package storage

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type quotaTransaction struct {
	pgx.Tx
	used, reserved int64
	inserted       bool
	committed      bool
	rolledBack     bool
}

type quotaRow func(...any) error

func (row quotaRow) Scan(values ...any) error { return row(values...) }

func (tx *quotaTransaction) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	return quotaRow(func(values ...any) error {
		switch sql {
		case lockUserForUploadSQL:
			*(values[0].(*string)) = "user"
		case selectStorageUsageSQL:
			*(values[0].(*int64)) = tx.used
			*(values[1].(*int64)) = tx.reserved
		default:
			return errors.New("unexpected quota query")
		}
		return nil
	})
}

func (tx *quotaTransaction) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	if sql != insertUploadReservationSQL {
		return pgconn.CommandTag{}, errors.New("unexpected quota statement")
	}
	tx.inserted = true
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (tx *quotaTransaction) Commit(context.Context) error   { tx.committed = true; return nil }
func (tx *quotaTransaction) Rollback(context.Context) error { tx.rolledBack = true; return nil }

func TestReserveUploadLimits(t *testing.T) {
	for _, test := range []struct {
		name                           string
		limit, size, used, reserved    int64
		wantQuotaError, wantOtherError bool
	}{
		{name: "unlimited", limit: -1, size: 1000, used: 10000, reserved: 5000},
		{name: "empty unlimited upload", limit: -1},
		{name: "largest representable upload", limit: -1, size: math.MaxInt64},
		{name: "finite boundary", limit: 100, size: 40, used: 50, reserved: 10},
		{name: "finite full", limit: 100, size: 41, used: 50, reserved: 10, wantQuotaError: true},
		{name: "finite oversized file", limit: 100, size: 101, wantQuotaError: true},
		{name: "negative file", limit: -1, size: -1, wantQuotaError: true},
		{name: "invalid zero limit", limit: 0, wantQuotaError: true},
		{name: "invalid negative limit", limit: -2, wantQuotaError: true},
		{name: "unlimited usage overflow", limit: -1, size: 1, used: math.MaxInt64, wantQuotaError: true},
		{name: "unlimited reservation overflow", limit: -1, size: 1, used: math.MaxInt64 - 1, reserved: 1, wantQuotaError: true},
		{name: "corrupt usage", limit: -1, used: -1, wantOtherError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			tx := &quotaTransaction{used: test.used, reserved: test.reserved}
			err := reserveUpload(context.Background(), func(context.Context) (pgx.Tx, error) { return tx, nil }, "user", "reservation", test.size, test.limit)
			if errors.Is(err, ErrStorageQuotaExceeded) != test.wantQuotaError || (err != nil) != (test.wantQuotaError || test.wantOtherError) {
				t.Fatalf("unexpected reservation error: %v", err)
			}
			if tx.inserted != (err == nil) || tx.committed != (err == nil) {
				t.Fatalf("invalid reservation transaction state: %+v", tx)
			}
		})
	}
}
