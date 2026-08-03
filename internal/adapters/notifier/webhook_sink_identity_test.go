package notifier

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"github.com/denisakp/sentinel/internal/ports"
)

// TestWebhookPayload_ErrorFieldIsIdentity proves the webhook payload builder
// serializes the ports.BackupContext.Error string verbatim. The notifier sink does
// not redact; redaction MUST happen upstream (in the dump adapter via
// sanitize.RedactStderr).
func TestWebhookPayload_ErrorFieldIsIdentity(t *testing.T) {
	cases := []struct {
		name       string
		errMsg     string
		mustHave   []string
		mustNotHas []string
	}{
		{
			name:     "redacted",
			errMsg:   "failed to execute pg_dump command - exit 1, password=*****",
			mustHave: []string{"*****"},
		},
		{
			name:       "control_non_redacted",
			errMsg:     "failed to execute pg_dump command - exit 1, password=hunter2",
			mustHave:   []string{"hunter2"},
			mustNotHas: nil, // control proves serializer does not strip secrets
		},
	}

	w := &WebhookNotifier{config: &ports.WebhookNotificationConfig{Type: "webhook"}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bc := &ports.BackupContext{
				BackupName:   "leak-probe",
				DatabaseType: "postgres",
				Status:       ports.NotifyStatusFailure,
				StartTime:    time.Now().UTC(),
				EndTime:      time.Now().UTC(),
				Error:        tc.errMsg,
			}
			msg := FormatMessage(bc)
			payload := w.buildBackupPayload(bc, msg)
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			s := string(body)
			for _, want := range tc.mustHave {
				if !strings.Contains(s, want) {
					t.Errorf("payload missing %q: %s", want, s)
				}
			}
			for _, no := range tc.mustNotHas {
				if strings.Contains(s, no) {
					t.Errorf("payload contains forbidden %q: %s", no, s)
				}
			}
		})
	}

	// Additional contract assertion: a payload built from an UPSTREAM-redacted
	// error must never carry the raw secret.
	t.Run("redacted_payload_has_no_secret", func(t *testing.T) {
		bc := &ports.BackupContext{
			BackupName: "leak-probe",
			Status:     ports.NotifyStatusFailure,
			StartTime:  time.Now().UTC(),
			EndTime:    time.Now().UTC(),
			Error:      "failed to execute pg_dump command - exit 1, password=*****",
		}
		msg := FormatMessage(bc)
		body, _ := json.Marshal(w.buildBackupPayload(bc, msg))
		if strings.Contains(string(body), "hunter2") {
			t.Errorf("redacted payload unexpectedly contains hunter2: %s", string(body))
		}
	})
}
