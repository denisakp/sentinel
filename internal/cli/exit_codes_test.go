package cli

import (
	"errors"
	"fmt"
	"testing"
)

func TestCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"bare ErrVerifyNotFound", ErrVerifyNotFound, 2},
		{"wrapped ErrVerifyNotFound", fmt.Errorf("ctx: %w", ErrVerifyNotFound), 2},
		{"bare ErrVerifySkipped", ErrVerifySkipped, 3},
		{"wrapped ErrVerifySkipped", fmt.Errorf("ctx: %w", ErrVerifySkipped), 3},
		{"bare ErrVerifyInternal", ErrVerifyInternal, 4},
		{"wrapped ErrVerifyInternal", fmt.Errorf("ctx: %w", ErrVerifyInternal), 4},
		{"arbitrary error", errors.New("x"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Code(tc.err); got != tc.want {
				t.Fatalf("Code(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
