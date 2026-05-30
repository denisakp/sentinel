package db_probe

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// captureMongoHandler is the mongo-test variant of the slog capture handler;
// distinct from captureHandler in ping_test.go to avoid name collision.
type captureMongoHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureMongoHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureMongoHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *captureMongoHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *captureMongoHandler) WithGroup(_ string) slog.Handler      { return h }

func (h *captureMongoHandler) has(msg string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

func swapMongoHandler(t *testing.T) *captureMongoHandler {
	t.Helper()
	h := &captureMongoHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func swapMongoConnect(t *testing.T, fn func(opts ...options.Lister[options.ClientOptions]) (*mongo.Client, error)) {
	t.Helper()
	prev := mongoConnect
	mongoConnect = fn
	t.Cleanup(func() { mongoConnect = prev })
}

// uri pointing at a port nothing is listening on, with a short server selection
// timeout so ping fails fast.
const unreachableMongoURI = "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=200&connectTimeoutMS=200"

func TestCheckMongoConnectivity_PingFailDisconnectOk(t *testing.T) {
	h := swapMongoHandler(t)
	err := CheckMongoConnectivity(unreachableMongoURI)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to ping the MongoDB instance") {
		t.Fatalf("unexpected error wording: %v", err)
	}
	if h.has("mongo_ping_disconnect_error") {
		t.Fatalf("disconnect warn record should not fire when disconnect succeeds")
	}
}

func TestCheckMongoConnectivity_PingFailDisconnectFail(t *testing.T) {
	swapMongoConnect(t, func(opts ...options.Lister[options.ClientOptions]) (*mongo.Client, error) {
		client, err := mongo.Connect(opts...)
		if err != nil {
			return client, err
		}
		_ = client.Disconnect(context.TODO())
		return client, nil
	})
	h := swapMongoHandler(t)

	err := CheckMongoConnectivity(unreachableMongoURI)
	if err == nil {
		t.Fatalf("want error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to ping the MongoDB instance") {
		t.Fatalf("ping err must be the returned cause: %v", err)
	}
	if !h.has("mongo_ping_disconnect_error") {
		t.Fatalf("want mongo_ping_disconnect_error slog record")
	}
}

func TestCheckMongoConnectivity_ConnectFail(t *testing.T) {
	stub := errors.New("forced connect error")
	swapMongoConnect(t, func(opts ...options.Lister[options.ClientOptions]) (*mongo.Client, error) {
		return nil, stub
	})
	err := CheckMongoConnectivity(unreachableMongoURI)
	if err == nil || !errors.Is(err, stub) {
		t.Fatalf("want wrapped stub error, got %v", err)
	}
}
