package notifier

import (
	"strings"
	"testing"

	"github.com/denisakp/sentinel/internal/config"
)

// TestOneUnresolvableChannelDoesNotSilenceTheOthers is the regression guard for
// #187.
//
// The dispatcher was built as a unit: the first channel whose secret would not
// resolve aborted construction and returned no dispatcher at all, so every
// correctly configured channel on that job went silent too.
//
// The realistic shape is Slack for immediate notice and email as the durable
// record. Rotating the Slack webhook, or deploying where that one variable is
// unset, took email down with it. Backups then failed with nobody told, and the
// only signal was an absence of messages, which is indistinguishable from
// everything working. This is the failure alerting exists to prevent, turned on
// alerting itself.
func TestOneUnresolvableChannelDoesNotSilenceTheOthers(t *testing.T) {
	t.Setenv("SENTINEL_TEST_SMTP_PW", "pw")
	t.Setenv("SENTINEL_TEST_FROM", "sentinel@example.test")
	// SENTINEL_TEST_SLACK_HOOK is deliberately NOT set: the rotated webhook.

	channels := []config.NotificationChannel{
		{
			Type:          "slack",
			WebhookURLEnv: "SENTINEL_TEST_SLACK_HOOK",
		},
		{
			Type:            "email",
			SMTPHost:        "smtp.example.test",
			SMTPPort:        587,
			SMTPPasswordEnv: "SENTINEL_TEST_SMTP_PW",
			FromAddressEnv:  "SENTINEL_TEST_FROM",
			ToAddresses:     []string{"ops@example.test"},
		},
	}

	dispatcher, err := NewDispatcherFromConfig(channels)

	if dispatcher == nil {
		t.Fatal("no dispatcher returned; the working email channel is silenced by the broken Slack one")
	}
	if dispatcher.Len() != 1 {
		t.Errorf("dispatcher holds %d channel(s), want 1: the email channel resolved and must survive "+
			"the Slack channel's unresolvable secret", dispatcher.Len())
	}
	if err == nil {
		t.Fatal("no error reported; a channel that will never notify must be surfaced, not swallowed")
	}
	msg := err.Error()
	if !strings.Contains(msg, "SENTINEL_TEST_SLACK_HOOK") {
		t.Errorf("the error does not name the unresolvable variable, so nobody can act on it.\ngot: %s", msg)
	}
	if !strings.Contains(msg, "1 still active") {
		t.Errorf("the error does not say how much alerting survived; one failed channel and every "+
			"failed channel are different situations.\ngot: %s", msg)
	}
}

// TestEveryChannelFailingIsReportedAsSuch: losing all alerting must be
// distinguishable from losing one channel, because the operator's response
// differs.
func TestEveryChannelFailingIsReportedAsSuch(t *testing.T) {
	channels := []config.NotificationChannel{
		{Type: "slack", WebhookURLEnv: "SENTINEL_TEST_MISSING_A"},
		{Type: "discord", WebhookURLEnv: "SENTINEL_TEST_MISSING_B"},
	}

	dispatcher, err := NewDispatcherFromConfig(channels)
	if dispatcher == nil {
		t.Fatal("dispatcher must never be nil; callers are told to use it unconditionally")
	}
	if dispatcher.Len() != 0 {
		t.Errorf("dispatcher holds %d channel(s), want 0", dispatcher.Len())
	}
	if err == nil {
		t.Fatal("losing every channel must be an error")
	}
	if !strings.Contains(err.Error(), "0 still active") {
		t.Errorf("the error must say that no alerting survived.\ngot: %s", err.Error())
	}
}

// TestUnsupportedTypeSkipsOnlyThatChannel: a typo in one channel's type is a
// configuration mistake, and it should cost that channel, not all of them.
func TestUnsupportedTypeSkipsOnlyThatChannel(t *testing.T) {
	t.Setenv("SENTINEL_TEST_HOOK", "https://example.test/hook")
	channels := []config.NotificationChannel{
		{Type: "slakc", WebhookURLEnv: "SENTINEL_TEST_HOOK"}, // typo
		{Type: "slack", WebhookURLEnv: "SENTINEL_TEST_HOOK"},
	}

	dispatcher, err := NewDispatcherFromConfig(channels)
	if dispatcher.Len() != 1 {
		t.Errorf("dispatcher holds %d channel(s), want 1", dispatcher.Len())
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported notification type") {
		t.Errorf("the typo must be reported by name.\ngot: %v", err)
	}
}

// TestAllChannelsResolvingReportsNoError keeps the happy path honest: the new
// error plumbing must not manufacture a warning when nothing is wrong.
func TestAllChannelsResolvingReportsNoError(t *testing.T) {
	t.Setenv("SENTINEL_TEST_HOOK", "https://example.test/hook")
	channels := []config.NotificationChannel{
		{Type: "slack", WebhookURLEnv: "SENTINEL_TEST_HOOK"},
		{Type: "discord", WebhookURLEnv: "SENTINEL_TEST_HOOK"},
	}
	dispatcher, err := NewDispatcherFromConfig(channels)
	if err != nil {
		t.Errorf("all channels resolved but an error was reported: %v", err)
	}
	if dispatcher.Len() != 2 {
		t.Errorf("dispatcher holds %d channel(s), want 2", dispatcher.Len())
	}
}
