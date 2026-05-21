package notifier

import "errors"

// ErrNon2xxResponse identifies a non-2xx HTTP response returned by a remote
// notifier endpoint. Channels wrap status-code errors with %w against this
// sentinel so callers (and tests) can identify them with errors.Is.
var ErrNon2xxResponse = errors.New("non-2xx response from notifier remote")
