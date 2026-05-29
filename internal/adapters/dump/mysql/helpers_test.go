package mysql

import (
	"slices"
	"strings"
	"testing"
)

func assertNoPasswordInArgs(t *testing.T, args []string, pwd string) {
	t.Helper()
	if pwd == "" {
		return
	}
	for i, a := range args {
		if strings.Contains(a, pwd) {
			t.Fatalf("password leaked into args[%d] = %q", i, a)
		}
	}
}

func assertContainsAll(t *testing.T, args []string, wanted ...string) {
	t.Helper()
	for _, w := range wanted {
		found := slices.Contains(args, w)
		if !found {
			t.Fatalf("missing element %q in args=%v", w, args)
		}
	}
}
