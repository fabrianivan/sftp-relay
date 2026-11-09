package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/audit"
	"github.com/fabrianivan/sftp-relay/internal/config"
)

// auditView displays audit log events.
type auditView struct {
	w      *Window
	reader *audit.Reader

	list         *widget.List
	events       []audit.Event
	filterType   *widget.Select
	filterStatus *widget.Select
	limitEntry   *widget.Entry
	refreshBtn   *widget.Button
	exportBtn    *widget.Button
	countLabel   *widget.Label
	initialized  bool
}

func newAuditView(w *Window) *auditView {
	v := &auditView{
		w:           w,
		events:      []audit.Event{},
		initialized: false,
	}

	// Initialize reader if audit log exists
	cfg := w.ctl.Config()
	if cfg.AuditLogEnabled() {
		logPath := resolveAuditPath(w.ctl.Path, cfg)
		v.reader = audit.NewReader(logPath)
	}

	v.filterType = widget.NewSelect([]string{
		"All Events",
		"Transfers",
		"Config Changes",
		"Engine Events",
	}, func(string) {
		if v.initialized {
			v.refresh()
		}
	})
	v.filterType.SetSelected("All Events")

	v.filterStatus = widget.NewSelect([]string{
		"All Status",
		"Success",
		"Failed",
		"Started",
	}, func(string) {
		if v.initialized {
			v.refresh()
		}
	})
	v.filterStatus.SetSelected("All Status")

	limitBind := binding.NewString()
	_ = limitBind.Set("100")
	v.limitEntry = widget.NewEntryWithData(limitBind)
	v.limitEntry.SetPlaceHolder("Limit")

	v.refreshBtn = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		v.refresh()
	})

	v.exportBtn = widget.NewButtonWithIcon("Export JSON", theme.DocumentSaveIcon(), func() {
		v.exportDialog()
	})

	v.countLabel = widget.NewLabel("0 events")

	v.list = widget.NewList(
		func() int { return len(v.events) },
		func() fyne.CanvasObject {
			return createEventCard()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(v.events) {
				return
			}
			event := v.events[id]
			updateEventCard(obj.(*fyne.Container), event)
		},
	)

	v.initialized = true

	return v
}

func (v *auditView) build() fyne.CanvasObject {
	if v.reader == nil {
		emptyIcon := widget.NewIcon(theme.InfoIcon())
		emptyText := widget.NewLabel("Audit logging is disabled")
		emptyText.TextStyle = fyne.TextStyle{Bold: true}
		emptyHint := widget.NewLabel("Enable audit logging in configuration to track system events")
		emptyHint.TextStyle = fyne.TextStyle{Italic: true}

		return container.NewCenter(
			container.NewVBox(
				emptyIcon,
				emptyText,
				emptyHint,
			),
		)
	}

	// Initial load
	v.refresh()

	// Filter controls with better styling
	typeLabel := widget.NewLabel("Type:")
	typeLabel.TextStyle = fyne.TextStyle{Bold: true}

	statusLabel := widget.NewLabel("Status:")
	statusLabel.TextStyle = fyne.TextStyle{Bold: true}

	limitLabel := widget.NewLabel("Limit:")
	limitLabel.TextStyle = fyne.TextStyle{Bold: true}

	filters := container.NewBorder(
		nil, nil,
		container.NewHBox(
			typeLabel,
			v.filterType,
			statusLabel,
			v.filterStatus,
			limitLabel,
			container.NewMax(
				container.NewPadded(v.limitEntry),
			),
		),
		container.NewHBox(
			v.refreshBtn,
			v.exportBtn,
		),
		container.NewCenter(v.countLabel),
	)

	return container.NewBorder(
		container.NewVBox(
			filters,
			widget.NewSeparator(),
		),
		nil, nil, nil,
		v.list,
	)
}

func (v *auditView) refresh() {
	if v.reader == nil || v.list == nil {
		return
	}

	filter := audit.Filter{}

	// Apply type filter
	switch v.filterType.Selected {
	case "Transfers":
		filter.EventTypes = []audit.EventType{
			audit.EventTransferStarted,
			audit.EventTransferCompleted,
			audit.EventTransferFailed,
			audit.EventFileArchived,
		}
	case "Config Changes":
		filter.EventTypes = []audit.EventType{audit.EventConfigChanged}
	case "Engine Events":
		filter.EventTypes = []audit.EventType{
			audit.EventEngineStarted,
			audit.EventEngineStopped,
		}
	}

	// Apply status filter
	switch v.filterStatus.Selected {
	case "Success":
		filter.Status = []audit.Status{audit.StatusSuccess}
	case "Failed":
		filter.Status = []audit.Status{audit.StatusFailed}
	case "Started":
		filter.Status = []audit.Status{audit.StatusStarted}
	}

	// Apply limit
	limitStr := v.limitEntry.Text
	if limitStr != "" {
		var limit int
		fmt.Sscanf(limitStr, "%d", &limit)
		if limit > 0 && limit <= 10000 {
			filter.Limit = limit
		}
	}
	if filter.Limit == 0 {
		filter.Limit = 100
	}

	// Query events
	events, err := v.reader.Query(filter)
	if err != nil {
		v.w.ctl.Log.Warnf("Failed to read audit log: %v", err)
		v.events = []audit.Event{}
	} else {
		// Reverse to show newest first
		v.events = make([]audit.Event, len(events))
		for i, e := range events {
			v.events[len(events)-1-i] = e
		}
	}

	v.countLabel.SetText(fmt.Sprintf("%d events", len(v.events)))
	v.list.Refresh()
}

func (v *auditView) exportDialog() {
	// TODO: Add file dialog for export location
	// For now, show a simple dialog
	dialog := widget.NewModalPopUp(
		container.NewVBox(
			widget.NewLabel("Export feature coming soon!"),
			widget.NewLabel(fmt.Sprintf("Current view: %d events", len(v.events))),
			widget.NewButton("OK", func() {}),
		),
		v.w.win.Canvas(),
	)
	dialog.Show()
}

func createEventCard() fyne.CanvasObject {
	// Status bar (colored left border)
	statusBar := canvas.NewRectangle(color.NRGBA{R: 0, G: 200, B: 0, A: 255})
	statusBar.SetMinSize(fyne.NewSize(4, 0))

	// Icon placeholder
	icon := widget.NewIcon(theme.DocumentIcon())

	// Title and timestamp line
	title := widget.NewLabel("")
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Wrapping = fyne.TextTruncate

	timestamp := widget.NewLabel("")
	timestamp.TextStyle = fyne.TextStyle{Italic: true}

	// Details line
	details := widget.NewLabel("")
	details.Wrapping = fyne.TextWrapWord

	// Actor line
	actor := widget.NewLabel("")
	actor.TextStyle = fyne.TextStyle{Monospace: true}

	// Content area
	content := container.NewVBox(
		container.NewHBox(title, layout.NewSpacer(), timestamp),
		details,
		actor,
	)

	// Card with status bar on left
	card := container.NewBorder(
		nil, nil,
		statusBar,
		nil,
		container.NewPadded(
			container.NewHBox(
				icon,
				content,
			),
		),
	)

	// Wrap in VBox with separator for spacing
	return container.NewVBox(
		card,
		widget.NewSeparator(),
	)
}

func updateEventCard(cardContainer *fyne.Container, e audit.Event) {
	// Get the card (first element before separator)
	card := cardContainer.Objects[0].(*fyne.Container)

	// Get status bar (left border)
	statusBar := card.Objects[0].(*canvas.Rectangle)

	// Get padded content container
	paddedContent := card.Objects[1].(*fyne.Container)
	contentHBox := paddedContent.Objects[0].(*fyne.Container)

	// Get icon and content vbox
	icon := contentHBox.Objects[0].(*widget.Icon)
	content := contentHBox.Objects[1].(*fyne.Container)

	// Get label widgets
	titleRow := content.Objects[0].(*fyne.Container)
	title := titleRow.Objects[0].(*widget.Label)
	timestamp := titleRow.Objects[2].(*widget.Label)
	details := content.Objects[1].(*widget.Label)
	actor := content.Objects[2].(*widget.Label)

	// Update status bar color and icon based on status
	switch e.Status {
	case audit.StatusSuccess:
		statusBar.FillColor = color.NRGBA{R: 76, G: 175, B: 80, A: 255} // Green
		icon.SetResource(theme.ConfirmIcon())
	case audit.StatusFailed:
		statusBar.FillColor = color.NRGBA{R: 244, G: 67, B: 54, A: 255} // Red
		icon.SetResource(theme.ErrorIcon())
	case audit.StatusStarted:
		statusBar.FillColor = color.NRGBA{R: 33, G: 150, B: 243, A: 255} // Blue
		icon.SetResource(theme.MediaPlayIcon())
	default:
		statusBar.FillColor = color.NRGBA{R: 158, G: 158, B: 158, A: 255} // Gray
		icon.SetResource(theme.InfoIcon())
	}
	statusBar.Refresh()

	// Update icon based on event type
	switch e.EventType {
	case audit.EventTransferStarted, audit.EventTransferCompleted, audit.EventTransferFailed:
		icon.SetResource(theme.MailSendIcon())
	case audit.EventConfigChanged:
		icon.SetResource(theme.SettingsIcon())
	case audit.EventEngineStarted, audit.EventEngineStopped:
		icon.SetResource(theme.ComputerIcon())
	case audit.EventFileArchived:
		icon.SetResource(theme.FileIcon())
	}

	// Update title
	title.SetText(fmt.Sprintf("%s - %s", e.EventType, e.Status))

	// Update timestamp
	timestamp.SetText(e.Timestamp.Local().Format("Jan 02, 15:04:05"))

	// Build details text
	var detailsText string
	if e.File != nil {
		detailsText = fmt.Sprintf("File: %s (%s)", e.File.Name, humanBytes(e.File.Size))
		if e.Duration != nil {
			detailsText += fmt.Sprintf(" • Duration: %s", e.Duration.Round(time.Millisecond))
		}
	} else if e.Source != nil && e.Target != nil {
		detailsText = fmt.Sprintf("From: %s@%s → To: %s@%s",
			e.Source.Username, e.Source.Host,
			e.Target.Username, e.Target.Host)
	} else if len(e.Details) > 0 {
		// Show first detail
		for k, v := range e.Details {
			detailsText = fmt.Sprintf("%s: %v", k, v)
			break
		}
	}

	if e.Error != "" {
		if detailsText != "" {
			detailsText += " • "
		}
		detailsText += fmt.Sprintf("Error: %s", e.Error)
	}

	details.SetText(detailsText)

	// Update actor
	actor.SetText(fmt.Sprintf("Actor: %s@%s (PID %d)",
		e.Actor.User, e.Actor.Hostname, e.Actor.PID))
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// resolveAuditPath computes the full path to the audit log file.
func resolveAuditPath(configPath string, cfg *config.Config) string {
	return config.Resolve(configPath, cfg.AuditLog.LogFile)
}
