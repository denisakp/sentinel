package db_probe

import (
	"context"
	stdsql "database/sql"
	"database/sql/driver"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// captureHandler is a minimal slog.Handler that records emitted records.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *captureHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(_ string) slog.Handler      { return h }

// fakeDriver implements just enough of database/sql/driver to control Ping/Close.
type fakeDriver struct {
	pingErr  error
	closeErr error
}

func (d *fakeDriver) Open(name string) (driver.Conn, error) {
	return &fakeConn{drv: d}, nil
}

type fakeConn struct{ drv *fakeDriver }

func (c *fakeConn) Prepare(query string) (driver.Stmt, error) { return nil, errors.New("nope") }
func (c *fakeConn) Close() error                              { return c.drv.closeErr }
func (c *fakeConn) Begin() (driver.Tx, error)                 { return nil, errors.New("nope") }
func (c *fakeConn) Ping(ctx context.Context) error            { return c.drv.pingErr }

var fakeRegisterOnce sync.Mutex
var fakeRegistered int

func registerFake(t *testing.T, pingErr, closeErr error) string {
	t.Helper()
	fakeRegisterOnce.Lock()
	defer fakeRegisterOnce.Unlock()
	fakeRegistered++
	name := "sentineltest-sql-" + itoa(fakeRegistered)
	stdsql.Register(name, &fakeDriver{pingErr: pingErr, closeErr: closeErr})
	return name
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestPingSqlDatabase(t *testing.T) {
	pingErr := errors.New("ping boom")
	closeErr := errors.New("close boom")

	cases := []struct {
		name     string
		pingErr  error
		closeErr error
		wantNil  bool
		wantErr  string
		wantWarn bool
	}{
		{"ok ok", nil, nil, true, "", false},
		{"ok close-err", nil, closeErr, false, "close ping conn", false},
		{"ping-err ok", pingErr, nil, false, "failed to ping database", false},
		{"both err", pingErr, closeErr, false, "failed to ping database", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drvName := registerFake(t, tc.pingErr, tc.closeErr)

			h := &captureHandler{}
			prev := slog.Default()
			slog.SetDefault(slog.New(h))
			t.Cleanup(func() { slog.SetDefault(prev) })

			err := PingSqlDatabase(drvName, "ignored")
			if tc.wantNil {
				if err != nil {
					t.Fatalf("want nil, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want err containing %q, got %v", tc.wantErr, err)
			}

			// Unwrap chain must reach the underlying source error.
			switch {
			case tc.pingErr != nil && !errors.Is(err, tc.pingErr):
				t.Fatalf("err chain missing ping cause: %v", err)
			case tc.pingErr == nil && tc.closeErr != nil && !errors.Is(err, tc.closeErr):
				t.Fatalf("err chain missing close cause: %v", err)
			}

			if tc.wantWarn {
				found := false
				h.mu.Lock()
				records := append([]slog.Record(nil), h.records...)
				h.mu.Unlock()
				for _, r := range records {
					if r.Message == "sql_ping_close_error" {
						found = true
					}
				}
				if !found {
					t.Fatalf("want sql_ping_close_error warn record, got %d records", len(records))
				}
			}
		})
	}
}
