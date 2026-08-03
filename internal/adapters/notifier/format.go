package notifier

import (
	"fmt"
	"regexp"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

// FormattedMessage contains the formatted notification message
type FormattedMessage struct {
	Title       string
	MessageText string
	Details     map[string]string
	Status      ports.BackupStatus
	Timestamp   time.Time
}

// FormatMessage formats a backup context into a notification message
func FormatMessage(backup *ports.BackupContext) *FormattedMessage {
	title := fmt.Sprintf("Backup %s: %s", backup.BackupName, backup.Status)

	details := map[string]string{
		"Status":        string(backup.Status),
		"Database":      backup.DatabaseName,
		"Database Type": backup.DatabaseType,
		"Timestamp":     backup.EndTime.Format(time.RFC3339),
		"Duration":      formatDuration(backup.Duration()),
		"File Size":     formatFileSize(backup.FileSize),
		"File Path":     backup.FilePath,
	}

	if backup.Error != "" {
		details["Error"] = backup.Error
	}

	messageText := fmt.Sprintf(`Backup Execution Report
Database: %s
Type: %s
Status: %s
Duration: %s
Size: %s
Path: %s`, backup.DatabaseName, backup.DatabaseType, backup.Status,
		formatDuration(backup.Duration()), formatFileSize(backup.FileSize), backup.FilePath)

	if backup.Error != "" {
		messageText += fmt.Sprintf("\nError: %s", backup.Error)
	}

	return &FormattedMessage{
		Title:       title,
		MessageText: messageText,
		Details:     details,
		Status:      backup.Status,
		Timestamp:   backup.EndTime,
	}
}

// FormatRestoreMessage formats a restore context into a notification message
func FormatRestoreMessage(restore *ports.RestoreContext) *FormattedMessage {
	title := fmt.Sprintf("Restore %s: %s", restore.RestoreName, restore.Status)

	details := map[string]string{
		"Status":         string(restore.Status),
		"Database":       restore.DatabaseName,
		"Database Type":  restore.DatabaseType,
		"Timestamp":      restore.EndTime.Format(time.RFC3339),
		"Duration":       formatDuration(restore.Duration()),
		"Bytes Restored": formatFileSize(restore.BytesRestored),
		"Source Backup":  restore.SourceBackupPath,
	}

	if restore.VerificationPassed {
		details["Verification"] = "Passed"
	} else {
		details["Verification"] = "Failed"
	}

	if restore.Error != "" {
		details["Error"] = restore.Error
	}

	messageText := fmt.Sprintf(`Restore Execution Report
Database: %s
Type: %s
Status: %s
Duration: %s
Bytes Restored: %s
Source Backup: %s
Verification: %v`, restore.DatabaseName, restore.DatabaseType, restore.Status,
		formatDuration(restore.Duration()), formatFileSize(restore.BytesRestored),
		restore.SourceBackupPath, restore.VerificationPassed)

	if restore.Error != "" {
		messageText += fmt.Sprintf("\nError: %s", restore.Error)
	}

	return &FormattedMessage{
		Title:       title,
		MessageText: messageText,
		Details:     details,
		Status:      restore.Status,
		Timestamp:   restore.EndTime,
	}
}

// formatDuration formats a duration for human readability
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
	minutes := int(d.Minutes())
	seconds := int(d.Seconds()) % 60
	return fmt.Sprintf("%dm %ds", minutes, seconds)
}

// formatFileSize formats bytes into human-readable file size
func formatFileSize(bytes int64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KB", float64(bytes)/1024)
	}
	if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", float64(bytes)/(1024*1024))
	}
	return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
}

// SanitizeForSlack removes characters that may break Slack formatting
func SanitizeForSlack(text string) string {
	// Replace markdown with plain text equivalents
	regs := map[*regexp.Regexp]string{
		regexp.MustCompile(`\*\*(.*?)\*\*`): "$1", // bold
		regexp.MustCompile(`_(.*?)_`):       "$1", // italic
		regexp.MustCompile(`~(.*?)~`):       "$1", // strikethrough
	}
	result := text
	for regex, replacement := range regs {
		result = regex.ReplaceAllString(result, replacement)
	}
	return result
}

// StatusEmoji returns an emoji based on backup status
func StatusEmoji(status ports.BackupStatus) string {
	switch status {
	case ports.NotifyStatusSuccess:
		return "OK"
	case ports.NotifyStatusFailure:
		return "FAIL"
	case ports.NotifyStatusWarning:
		return "WARN"
	default:
		return "INFO"
	}
}
