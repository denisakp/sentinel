package mongo

import (
	"sync"
	"testing"

	"github.com/denisakp/sentinel/internal/ports"
)

func TestRegisterUnregisterCloseAll(t *testing.T) {
	p := genPEMPair(t)
	cfg := &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: p.keyPath}

	const N = 4
	mats := make([]*MongoTLSMaterial, N)
	for i := 0; i < N; i++ {
		m, _, err := PrepareMongoTLS(cfg, "ring")
		if err != nil {
			t.Fatal(err)
		}
		mats[i] = m
	}

	// Unregister half so they won't be closed by CloseAll.
	for i := 0; i < N/2; i++ {
		Unregister(mats[i])
		if err := mats[i].Close(); err != nil {
			t.Fatal(err)
		}
	}

	if err := CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	// idempotent
	if err := CloseAll(); err != nil {
		t.Fatalf("CloseAll second: %v", err)
	}
}

func TestCleanupRingConcurrent(t *testing.T) {
	p := genPEMPair(t)
	cfg := &ports.Config{Enabled: true, Mode: "verify-full", ClientCert: p.certPath, ClientKey: p.keyPath}

	const N = 16
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			m, _, err := PrepareMongoTLS(cfg, "concur")
			if err != nil {
				t.Errorf("prep: %v", err)
				return
			}
			_ = m.Close()
		}()
	}
	wg.Wait()
	if err := CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
}
