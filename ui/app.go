// Package ui is the Fyne desktop interface.
//
// Fyne 2.6+ requires every widget update from a background goroutine to go
// through fyne.Do; all engine/log listeners in this package follow that rule.
package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"

	"github.com/fabrianivan/sftp-relay/assets"
	"github.com/fabrianivan/sftp-relay/internal/setup"
)

// AppName is shown in titles and notifications.
const AppName = "SFTP Relay"

// Window is the main application window.
type Window struct {
	app     fyne.App
	win     fyne.Window
	ctl     *setup.Controller
	version string

	tabs      *container.AppTabs
	dashboard *dashboard
	history   *historyView
	settings  *settingsView
	logs      *logView
	audit     *auditView
}

// Icon returns the embedded application icon.
func Icon() fyne.Resource { return fyne.NewStaticResource("sftp-relay.png", assets.Icon) }

// NewWindow builds the main window. configProblem, when non-nil, opens the
// Settings tab with the validation error shown.
func NewWindow(a fyne.App, ctl *setup.Controller, version string, configProblem error) *Window {
	w := &Window{app: a, ctl: ctl, version: version}
	w.win = a.NewWindow(AppName + " v" + version)
	w.win.SetIcon(Icon())
	w.win.Resize(fyne.NewSize(1280, 820)) // Larger modern window
	w.win.SetMaster()

	w.dashboard = newDashboard(w)
	w.history = newHistoryView(w)
	w.settings = newSettingsView(w)
	w.logs = newLogView(w)
	w.audit = newAuditView(w)

	// Modern tabs with better icons
	w.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Dashboard", theme.HomeIcon(), w.dashboard.content),
		container.NewTabItemWithIcon("History", theme.HistoryIcon(), w.history.content),
		container.NewTabItemWithIcon("Audit", theme.DocumentIcon(), w.audit.build()),
		container.NewTabItemWithIcon("Settings", theme.SettingsIcon(), w.settings.content),
		container.NewTabItemWithIcon("Logs", theme.ListIcon(), w.logs.content),
	)
	w.tabs.OnSelected = func(t *container.TabItem) {
		if t.Text == "History" {
			w.history.refresh()
		} else if t.Text == "Audit" {
			w.audit.refresh()
		}
	}
	w.win.SetContent(w.tabs)

	if desk, ok := a.(desktop.App); ok {
		w.setupTray(desk)
		w.win.SetCloseIntercept(func() { w.win.Hide() }) // keep relaying from the tray
	}

	if configProblem != nil {
		w.tabs.SelectIndex(3) // Settings tab moved to index 3
		dialog.ShowError(configProblem, w.win)
	}
	return w
}

func (w *Window) setupTray(desk desktop.App) {
	pause := fyne.NewMenuItem("Pause scanning", nil)
	pause.Action = func() {
		if w.ctl.Engine.Running() {
			w.ctl.Engine.Stop()
		} else {
			w.ctl.Engine.Start()
		}
	}
	quit := fyne.NewMenuItem("Quit", func() { w.app.Quit() })
	quit.IsQuit = true
	menu := fyne.NewMenu(AppName,
		fyne.NewMenuItem("Open "+AppName, func() { w.win.Show(); w.win.RequestFocus() }),
		fyne.NewMenuItem("Scan now", w.ctl.Engine.ScanNow),
		pause,
		fyne.NewMenuItemSeparator(),
		quit,
	)
	desk.SetSystemTrayIcon(Icon())
	desk.SetSystemTrayMenu(menu)
	w.dashboard.onStateChange = func(running bool) {
		if running {
			pause.Label = "Pause scanning"
		} else {
			pause.Label = "Resume scanning"
		}
		menu.Refresh()
	}
}

// Show displays the window.
func (w *Window) Show() { w.win.Show() }
