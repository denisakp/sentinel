package backup

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/shlex"
)

// ErrUnterminatedQuote is returned by ParseAdditionalArgs when the input ends
// inside an unterminated quoted span (or after a trailing backslash escape,
// which shlex also reports as an EOF-in-tokenizer error). The single sentinel
// keeps the contract surface small; the wrapped error message carries the
// offending input for operator diagnostics.
var ErrUnterminatedQuote = errors.New("unterminated quote in additional_args")

// ErrNULByte is returned when the input contains a NUL byte. NUL is a hard
// argv boundary character on POSIX and is never valid in additional_args.
var ErrNULByte = errors.New("NUL byte in additional_args")

// ParseAdditionalArgs tokenizes an operator-supplied additional_args string
// using POSIX shell quoting rules (backed by github.com/google/shlex).
// Empty or whitespace-only input returns an empty slice with no error.
// Malformed input (unterminated quote, trailing escape, NUL byte) returns
// nil and a wrapped ErrUnterminatedQuote or ErrNULByte. No variable
// expansion, command substitution, or glob expansion is performed; constructs
// like $VAR, `cmd`, $(cmd), and * appear literally in the emitted tokens.
// See specs/023-shlex-args-parser/contracts/parser.md for the full contract.
func ParseAdditionalArgs(entry string) ([]string, error) {
	if strings.TrimSpace(entry) == "" {
		return []string{}, nil
	}
	if strings.ContainsRune(entry, 0) {
		return nil, fmt.Errorf("%w: %q", ErrNULByte, entry)
	}
	tokens, err := shlex.Split(entry)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnterminatedQuote, entry)
	}
	return tokens, nil
}

// RemoveArgsDuplicate removes duplicate arguments from the list of arguments
func RemoveArgsDuplicate(args []string) []string {
	keys := make(map[string]bool) // map of arguments
	var list []string             // list of unique arguments

	for _, entry := range args {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}

	return list
}
