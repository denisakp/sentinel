package notifier

import "github.com/denisakp/sentinel/internal/ports"

// Compile-time port conformance assertions. Drift here fails the build.
var (
	_ ports.Dispatcher = (*Dispatcher)(nil)
	_ ports.Notifier   = (*SlackNotifier)(nil)
	_ ports.Notifier   = (*DiscordNotifier)(nil)
	_ ports.Notifier   = (*EmailNotifier)(nil)
	_ ports.Notifier   = (*WebhookNotifier)(nil)
)
