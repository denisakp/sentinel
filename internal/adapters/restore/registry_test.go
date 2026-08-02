package restore

import "testing"

func TestNewArgsFactory(t *testing.T) {
	for _, engine := range []string{"postgres", "mysql", "mariadb", "mongodb"} {
		if _, err := NewArgsFactory(engine); err != nil {
			t.Errorf("NewArgsFactory(%q) unexpected error: %v", engine, err)
		}
	}
	if _, err := NewArgsFactory("oracle"); err == nil {
		t.Error("NewArgsFactory(\"oracle\") expected error, got nil")
	}
}
