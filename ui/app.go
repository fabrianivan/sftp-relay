// Package ui is the Fyne desktop interface.
//
// Fyne 2.6+ requires every widget update from a background goroutine to go
// through fyne.Do; all engine/log listeners in this package follow that rule.
package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"

	"github.com/fabrianivan/sftp-relay/assets"
	"github.com/fabrianivan/sftp-relay/internal/setup"
)

// AppName is shown in titles and notifications.
const AppName = "SFTP Relay"

// Screens, in sidebar order.
const (
	pageDashboard = "Dashboard"
	pageHistory   = "History"
	pageAudit     = "Audit"
	pageSettings  = "Settings"
	pageLogs      = "Logs"
)

// Window is the main application window.
type Window struct {
	app     fyne.App
	win     fyne.Window
	ctl     *setup.Controller
	version string

	current string
	pages   map[string]fyne.CanvasObject
	nav     map[string]*railButton
	body    *fyne.Container

	engineDot  *canvas.Circle
	engineText *canvas.Text

	dashboard *dashboard
	history   *historyView
	settings  *settingsView
	logs      *logView
	audit     *auditView
}

// Icon returns the embedded application icon.
func Icon() fyne.Resource { return fyne.NewStaticResource("sftp-relay.png", assets.Icon) }

// NewWindow builds the main window. configProblem, when non-nil, opens the
// Settings screen with the validation error shown.
func NewWindow(a fyne.App, ctl *setup.Controller, version string, configProblem error) *Window {
	w := &Window{app: a, ctl: ctl, version: version}
	w.win = a.NewWindow(AppName + " " + versionLabel(version))
	w.win.SetIcon(Icon())
	w.win.Resize(fyne.NewSize(1280, 820))
	w.win.SetMaster()
	w.win.SetPadded(false)

	// The rail comes first: the dashboard reports engine state to it.
	rail := w.buildRail()

	w.dashboard = newDashboard(w)
	w.history = newHistoryView(w)
	w.settings = newSettingsView(w)
	w.logs = newLogView(w)
	w.audit = newAuditView(w)

	w.pages = map[string]fyne.CanvasObject{
		pageDashboard: w.dashboard.content,
		pageHistory:   w.history.content,
		pageAudit:     w.audit.content,
		pageSettings:  w.settings.content,
		pageLogs:      w.logs.content,
	}
	w.body = container.NewStack()
	w.win.SetContent(container.NewBorder(nil, nil, rail, nil, w.body))
	w.showPage(pageDashboard)

	if desk, ok := a.(desktop.App); ok {
		w.setupTray(desk)
		w.win.SetCloseIntercept(func() { w.win.Hide() }) // keep relaying from the tray
	}

	if configProblem != nil {
		w.showPage(pageSettings)
		dialog.ShowError(configProblem, w.win)
	}
	return w
}

// buildRail creates the narrow navigation strip: logo, destinations and the
// engine's state at the bottom, visible from every screen.
func (w *Window) buildRail() fyne.CanvasObject {
	logo := canvas.NewImageFromResource(Icon())
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSquareSize(38))

	items := []struct {
		name string
		icon fyne.Resource
	}{
		{pageDashboard, theme.HomeIcon()},
		{pageHistory, theme.HistoryIcon()},
		{pageAudit, theme.DocumentIcon()},
		{pageSettings, theme.SettingsIcon()},
		{pageLogs, theme.ListIcon()},
	}
	w.nav = map[string]*railButton{}
	list := vstack(4)
	for _, it := range items {
		b := newRailButton(it.name, it.icon, func() { w.showPage(it.name) })
		w.nav[it.name] = b
		list.Add(b)
	}

	var dot fyne.CanvasObject
	w.engineDot, dot = newDot(ColorMuted)
	w.engineText = newText("Stopped", 11, ColorForegroundSub, styleBold)
	w.engineText.Alignment = fyne.TextAlignCenter
	engine := vstack(5, dot, w.engineText)

	edge := canvas.NewRectangle(ColorSeparator)
	edge.SetMinSize(fyne.NewSize(1, 1))
	return container.NewStack(
		canvas.NewRectangle(ColorRail),
		container.NewBorder(nil, nil, nil, edge,
			container.NewBorder(
				pad(18, 16, 0, 0, container.NewCenter(logo)),
				pad(0, 16, 0, 0, engine),
				nil, nil,
				pad(0, 0, 8, 8, list))),
	)
}

// versionLabel formats a build version for display: 2.0.0 becomes v2.0.0,
// while tags and names such as v2.0.0 or dev are shown as they are.
func versionLabel(v string) string {
	if v != "" && v[0] >= '0' && v[0] <= '9' {
		return "v" + v
	}
	return v
}

// showPage switches the visible screen.
func (w *Window) showPage(name string) {
	p, ok := w.pages[name]
	if !ok {
		return
	}
	w.current = name
	for n, b := range w.nav {
		b.setSelected(n == name)
	}
	w.body.Objects = []fyne.CanvasObject{p}
	w.body.Refresh()
	w.dashboard.flow.setShown(name == pageDashboard)
	switch name {
	case pageHistory:
		w.history.refresh()
	case pageAudit:
		w.audit.refresh()
	}
}

// setEngineState updates the status light at the bottom of the rail.
func (w *Window) setEngineState(label string, t tone) {
	setDot(w.engineDot, t.color())
	setText(w.engineText, label, ColorForegroundSub)
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
