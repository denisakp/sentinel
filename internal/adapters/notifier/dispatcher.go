package notifier

import (
	"context"
	"fmt"
	"github.com/denisakp/sentinel/internal/ports"
	"log/slog"
	"sync"
	"time"
)

// Dispatcher handles sending notifications to multiple channels
type Dispatcher struct {
	notifiers []ports.Notifier
	logger    *slog.Logger
	timeout   time.Duration
}

// NewDispatcher creates a new notification dispatcher
func NewDispatcher(logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dispatcher{
		notifiers: []ports.Notifier{},
		logger:    logger,
		timeout:   30 * time.Second,
	}
}

// AddWebhookNotifier adds a webhook-based notifier (Slack, Discord, or generic webhook)
func (d *Dispatcher) AddWebhookNotifier(config *ports.WebhookNotificationConfig) error {
	if config == nil {
		return fmt.Errorf("webhook notification config is nil")
	}

	if config.WebhookURL == "" {
		return fmt.Errorf("webhook URL is empty")
	}

	var notifier ports.Notifier
	switch config.Type {
	case "slack":
		notifier = NewSlackNotifier(config)
	case "discord":
		notifier = NewDiscordNotifier(config)
	case "webhook":
		notifier = NewWebhookNotifier(config)
	default:
		return fmt.Errorf("unknown webhook type: %s", config.Type)
	}

	d.notifiers = append(d.notifiers, notifier)
	d.logger.Debug("added webhook notifier", "type", config.Type)
	return nil
}

// AddEmailNotifier adds an email notifier
func (d *Dispatcher) AddEmailNotifier(config *ports.EmailNotificationConfig) error {
	if config == nil {
		return fmt.Errorf("email notification config is nil")
	}

	if config.FromAddress == "" {
		return fmt.Errorf("email from address is empty")
	}

	if len(config.ToAddresses) == 0 {
		return fmt.Errorf("email to addresses are empty")
	}

	notifier := NewEmailNotifier(config)
	d.notifiers = append(d.notifiers, notifier)
	d.logger.Debug("added email notifier")
	return nil
}

// NotifyAsync sends notifications asynchronously to all configured channels
// Returns a channel that closes when all notifications are sent
func (d *Dispatcher) NotifyAsync(backup *ports.BackupContext) <-chan error {
	resultChan := make(chan error, len(d.notifiers))

	if len(d.notifiers) == 0 {
		close(resultChan)
		return resultChan
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)

	var wg sync.WaitGroup
	for _, notifier := range d.notifiers {
		wg.Add(1)
		go func(n ports.Notifier) {
			defer wg.Done()
			if !n.IsEnabled() {
				return
			}
			if err := n.SendBackup(ctx, backup); err != nil {
				resultChan <- fmt.Errorf("%s notification failed - %w", n.Type(), err)
			}
		}(notifier)
	}

	go func() {
		wg.Wait()
		cancel()
		close(resultChan)
	}()

	return resultChan
}

// NotifyRestoreAsync sends notifications asynchronously for restore operations
func (d *Dispatcher) NotifyRestoreAsync(restore *ports.RestoreContext) <-chan error {
	resultChan := make(chan error, len(d.notifiers))

	if len(d.notifiers) == 0 {
		close(resultChan)
		return resultChan
	}

	ctx, cancel := context.WithTimeout(context.Background(), d.timeout)

	var wg sync.WaitGroup
	for _, notifier := range d.notifiers {
		wg.Add(1)
		go func(n ports.Notifier) {
			defer wg.Done()
			if !n.IsEnabled() {
				return
			}
			if err := n.SendRestore(ctx, restore); err != nil {
				resultChan <- fmt.Errorf("%s notification failed - %w", n.Type(), err)
			}
		}(notifier)
	}

	go func() {
		wg.Wait()
		cancel()
		close(resultChan)
	}()

	return resultChan
}

// Notify sends notifications synchronously to all configured channels
// Collects and returns all errors (non-blocking - continues sending even if one fails)
func (d *Dispatcher) Notify(backup *ports.BackupContext) error {
	if len(d.notifiers) == 0 {
		return nil
	}

	resultChan := d.NotifyAsync(backup)
	var errs []error
	for err := range resultChan {
		if err != nil {
			errs = append(errs, err)
			d.logger.Warn("notification channel failed", "error", err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notification errors: %v", errs)
	}
	return nil
}

// NotifyRestore sends notifications synchronously for restore operations
func (d *Dispatcher) NotifyRestore(restore *ports.RestoreContext) error {
	if len(d.notifiers) == 0 {
		return nil
	}

	resultChan := d.NotifyRestoreAsync(restore)
	var errs []error
	for err := range resultChan {
		if err != nil {
			errs = append(errs, err)
			d.logger.Warn("notification channel failed", "error", err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("notification errors: %v", errs)
	}
	return nil
}

// Clear removes all registered notifiers
func (d *Dispatcher) Clear() {
	d.notifiers = []ports.Notifier{}
}

// Count returns the number of registered notifiers
func (d *Dispatcher) Count() int {
	return len(d.notifiers)
}

// SetTimeout sets the timeout for notification delivery
func (d *Dispatcher) SetTimeout(timeout time.Duration) {
	d.timeout = timeout
}

// Len reports how many notifiers the dispatcher will fan out to. Used to tell
// "some alerting survived" from "alerting is entirely gone", which are different
// situations for an operator reading a warning.
func (d *Dispatcher) Len() int { return len(d.notifiers) }
