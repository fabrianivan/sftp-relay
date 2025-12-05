package notifier

import "github.com/gen2brain/beeep"

type Notifier interface {
	Notify(title, message string, timeoutSec int)
}

type BeeepNotifier struct{}

func (BeeepNotifier) Notify(title, message string, _ int) {
	_ = beeep.Notify("SFTP Uploader - "+title, message, "assets/logo.ico")
}

type NoopNotifier struct{}

func (NoopNotifier) Notify(string, string, int) {}
