package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		flagValue   string
		setupFile   string // relative filename to create inside tmpDir before the test
		wantPath    string // expected resolved path (may be relative or absolute)
		wantErr     bool
		errContains []string // substrings that must appear in the error message
	}{
		{
			name:      "explicit flag value returned as-is without existence check",
			flagValue: "/tmp/explicit.yaml",
			wantPath:  "/tmp/explicit.yaml",
		},
		{
			name:      "relative explicit flag value returned unchanged",
			flagValue: "relative/explicit.yaml",
			wantPath:  "relative/explicit.yaml",
		},
		{
			name:      "default file discovered when flag is empty",
			flagValue: "",
			setupFile: DefaultConfigFileName,
			wantPath:  DefaultConfigFileName,
		},
		{
			name:        "actionable error when neither flag nor default file exists",
			flagValue:   "",
			wantErr:     true,
			errContains: []string{DefaultConfigFileName, "--config"},
		},
		{
			name:        "error mentions searched filename",
			flagValue:   "",
			wantErr:     true,
			errContains: []string{"sentinel-config.yaml"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each sub-test uses its own sub-directory for isolation.
			subDir := t.TempDir()

			orig, err := os.Getwd()
			if err != nil {
				t.Fatalf("getwd: %v", err)
			}
			if err := os.Chdir(subDir); err != nil {
				t.Fatalf("chdir: %v", err)
			}
			t.Cleanup(func() { _ = os.Chdir(orig) })

			if tc.setupFile != "" {
				fullPath := filepath.Join(subDir, tc.setupFile)
				if err := os.WriteFile(fullPath, []byte("# test config\n"), 0644); err != nil {
					t.Fatalf("write setup file: %v", err)
				}
				_ = tmpDir // suppress unused warning
			}

			got, err := ResolveConfigPath(tc.flagValue)

			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got path=%q", got)
					return
				}
				for _, substr := range tc.errContains {
					if !strings.Contains(err.Error(), substr) {
						t.Errorf("error %q does not contain %q", err.Error(), substr)
					}
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if got != tc.wantPath {
				t.Errorf("got=%q, want=%q", got, tc.wantPath)
			}
		})
	}
}

func TestResolveConfigPath_ExplicitFlagPrecedence(t *testing.T) {
	// Even when the default file exists, an explicit flag value must win.
	subDir := t.TempDir()

	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(subDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	// Create the default file in the directory.
	if err := os.WriteFile(filepath.Join(subDir, DefaultConfigFileName), []byte("# default\n"), 0644); err != nil {
		t.Fatalf("write default file: %v", err)
	}

	explicit := "/tmp/override.yaml"
	got, err := ResolveConfigPath(explicit)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != explicit {
		t.Errorf("got=%q, want=%q (explicit flag must take precedence)", got, explicit)
	}
}
