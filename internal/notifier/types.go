package notifier

import "github.com/denisakp/sentinel/internal/ports"

// StatusColor defines color representations for notification statuses across different platforms.
//
// Internal to the notifier adapter — not part of the dispatcher/notifier
// port contract. Channel-specific rendering helpers live alongside the
// concrete channel implementations.
type StatusColor struct {
	// SlackHex is the hex color code for Slack messages (e.g., "#36a64f")
	SlackHex string
	// DiscordInt is the integer color code for Discord embeds (e.g., 3066993)
	DiscordInt int
	// Name is the human-readable status name
	Name string
}

// GetStatusColor returns the color configuration for a given backup status.
// This ensures consistent colors across all notification platforms.
func GetStatusColor(status ports.BackupStatus) StatusColor {
	switch status {
	case ports.NotifyStatusSuccess:
		return StatusColor{
			SlackHex:   "#36a64f", // Green
			DiscordInt: 3066993,   // Green
			Name:       "Success",
		}
	case ports.NotifyStatusFailure:
		return StatusColor{
			SlackHex:   "#ff0000", // Red
			DiscordInt: 15158332,  // Red
			Name:       "Failure",
		}
	case ports.NotifyStatusWarning:
		return StatusColor{
			SlackHex:   "#ffaa00", // Orange
			DiscordInt: 15105570,  // Orange
			Name:       "Warning",
		}
	default:
		return StatusColor{
			SlackHex:   "#808080", // Gray (default/unknown)
			DiscordInt: 9807270,   // Gray
			Name:       "Unknown",
		}
	}
}

// ShouldNotify checks if notification should be sent for given status.
func ShouldNotify(events []string, status ports.BackupStatus) bool {
	for _, event := range events {
		if event == string(status) {
			return true
		}
	}
	return false
}
