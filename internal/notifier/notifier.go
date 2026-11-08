// Package notifier sends desktop notifications.
package notifier

// Notifier shows a short message to the user.
type Notifier interface {
	Notify(title, message string)
}

// Noop discards notifications (headless mode and tests).
type Noop struct{}

// Notify does nothing.
func (Noop) Notify(string, string) {}

// Func adapts a function to the Notifier interface.
type Func func(title, message string)

// Notify calls f.
func (f Func) Notify(title, message string) { f(title, message) }
