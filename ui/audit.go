package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/audit"
	"github.com/fabrianivan/sftp-relay/internal/config"
)

// auditView displays audit log events with modern card-based design.
type auditView struct {
	w      *Window
	reader *audit.Reader

	list         *widget.List
	events       []audit.Event
	filterType   *widget.Select
	filterStatus *widget.Select
	searchEntry  *widget.Entry
	refreshBtn   *widget.Button
	exportBtn    *widget.Button
	statsLabel   *widget.Label
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

	// Search entry with modern styling
	v.searchEntry = widget.NewEntry()
	v.searchEntry.SetPlaceHolder("🔍 Search events, files, or details...")
	v.searchEntry.OnChanged = func(string) {
		if v.initialized {
			v.refresh()
		}
	}

	// Filter type dropdown
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

	// Filter status dropdown
	v.filterStatus = widget.NewSelect([]string{
		"All Status",
		"Success",
		"Failed",
		"In Progress",
	}, func(string) {
		if v.initialized {
			v.refresh()
		}
	})
	v.filterStatus.SetSelected("All Status")

	// Modern buttons
	v.refreshBtn = widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
		v.refresh()
	})
	v.refreshBtn.Importance = widget.LowImportance

	v.exportBtn = widget.NewButtonWithIcon("Export", theme.DocumentSaveIcon(), func() {
		v.exportDialog()
	})
	v.exportBtn.Importance = widget.HighImportance

	// Stats label
	v.statsLabel = widget.NewLabel("Loading...")

	// Create modern card list
	v.list = widget.NewList(
		func() int { return len(v.events) },
		func() fyne.CanvasObject {
			return createModernEventCard()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(v.events) {
				return
			}
			event := v.events[id]
			updateModernEventCard(obj.(*fyne.Container), event)
		},
	)

	v.initialized = true
	return v
}

func (v *auditView) build() fyne.CanvasObject {
	if v.reader == nil {
		// Modern empty state
		emptyIcon := widget.NewIcon(theme.InfoIcon())
		emptyTitle := widget.NewLabel("Audit Logging Disabled")
		emptyTitle.TextStyle = fyne.TextStyle{Bold: true}
		emptyTitle.Alignment = fyne.TextAlignCenter

		emptyDesc := widget.NewLabel("Enable audit logging in Settings to track all system events,\nfile transfers, and configuration changes.")
		emptyDesc.Alignment = fyne.TextAlignCenter

		enableBtn := widget.NewButton("Open Settings", func() {
			// TODO: Navigate to settings
		})
		enableBtn.Importance = widget.HighImportance

		return container.NewCenter(
			container.NewVBox(
				emptyIcon,
				layout.NewSpacer(),
				emptyTitle,
				emptyDesc,
				layout.NewSpacer(),
				enableBtn,
			),
		)
	}

	// Initial load
	v.refresh()

	// Modern header with title and description
	title := widget.NewLabel("Audit Log")
	title.TextStyle = fyne.TextStyle{Bold: true}

	subtitle := widget.NewLabel("Track and review all system activity")
	subtitle.TextStyle = fyne.TextStyle{Italic: true}

	header := container.NewVBox(
		title,
		subtitle,
	)

	// Modern filter bar with better layout
	filterRow := container.NewBorder(
		nil, nil,
		container.NewHBox(
			widget.NewLabel("Type:"),
			v.filterType,
			widget.NewLabel("Status:"),
			v.filterStatus,
		),
		container.NewHBox(
			v.refreshBtn,
			v.exportBtn,
		),
		v.searchEntry,
	)

	// Stats bar with modern badges
	statsBar := container.NewHBox(
		v.statsLabel,
		layout.NewSpacer(),
	)

	// Main content with proper spacing
	content := container.NewBorder(
		// Top
		container.NewVBox(
			container.NewPadded(header),
			widget.NewSeparator(),
			container.NewPadded(filterRow),
			widget.NewSeparator(),
		),
		// Bottom
		container.NewVBox(
			widget.NewSeparator(),
			container.NewPadded(statsBar),
		),
		nil, nil,
		// Center - scrollable list
		v.list,
	)

	return content
}

func (v *auditView) refresh() {
	if v.reader == nil || v.list == nil {
		return
	}

	filter := audit.Filter{
		Limit: 500,
	}

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
	case "In Progress":
		filter.Status = []audit.Status{audit.StatusStarted}
	}

	// Query events
	events, err := v.reader.Query(filter)
	if err != nil {
		v.w.ctl.Log.Warnf("Failed to read audit log: %v", err)
		v.events = []audit.Event{}
	} else {
		// Filter by search text
		searchText := v.searchEntry.Text
		if searchText != "" {
			filtered := []audit.Event{}
			for _, e := range events {
				if matchesSearch(e, searchText) {
					filtered = append(filtered, e)
				}
			}
			events = filtered
		}

		// Reverse to show newest first
		v.events = make([]audit.Event, len(events))
		for i, e := range events {
			v.events[len(events)-1-i] = e
		}
	}

	// Calculate statistics
	success, failed, inProgress := 0, 0, 0
	for _, e := range v.events {
		switch e.Status {
		case audit.StatusSuccess:
			success++
		case audit.StatusFailed:
			failed++
		case audit.StatusStarted:
			inProgress++
		}
	}

	v.statsLabel.SetText(fmt.Sprintf("📊 %d total  •  ✅ %d success  •  ❌ %d failed  •  ⏳ %d in progress",
		len(v.events), success, failed, inProgress))
	v.list.Refresh()
}

func matchesSearch(e audit.Event, search string) bool {
	// Simple case-insensitive contains
	searchLower := toLower(search)
	if contains(toLower(string(e.EventType)), searchLower) {
		return true
	}
	if contains(toLower(e.Error), searchLower) {
		return true
	}
	if e.File != nil && contains(toLower(e.File.Name), searchLower) {
		return true
	}
	return false
}

func toLower(s string) string {
	// Simple ASCII lowercase
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			result[i] = c + 32
		} else {
			result[i] = c
		}
	}
	return string(result)
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (v *auditView) exportDialog() {
	title := widget.NewLabel("Export Audit Log")
	title.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewVBox(
		title,
		widget.NewLabel(""),
		widget.NewLabel(fmt.Sprintf("Ready to export %d events to JSON", len(v.events))),
		widget.NewLabel(""),
		widget.NewLabel("This feature will be available in a future update."),
	)

	dialog := widget.NewModalPopUp(content, v.w.win.Canvas())

	okBtn := widget.NewButton("Close", func() {
		dialog.Hide()
	})
	okBtn.Importance = widget.HighImportance

	content.Add(widget.NewLabel(""))
	content.Add(okBtn)

	dialog.Resize(fyne.NewSize(450, 250))
	dialog.Show()
}

// createModernEventCard creates a modern card with gradient, shadows, and icons
func createModernEventCard() fyne.CanvasObject {
	// Card background
	cardBg := canvas.NewRectangle(color.NRGBA{R: 30, G: 33, B: 45, A: 255})
	cardBg.CornerRadius = 8

	// Status indicator (colored dot)
	statusDot := canvas.NewCircle(color.NRGBA{R: 76, G: 175, B: 80, A: 255})
	statusDot.StrokeColor = color.NRGBA{R: 76, G: 175, B: 80, A: 100}
	statusDot.StrokeWidth = 2
	statusDot.Resize(fyne.NewSize(12, 12))

	// Event icon
	eventIcon := widget.NewIcon(theme.DocumentIcon())

	// Status badge background
	badgeBg := canvas.NewRectangle(color.NRGBA{R: 76, G: 175, B: 80, A: 40})
	badgeBg.CornerRadius = 12

	// Status badge text
	statusBadge := widget.NewLabel("SUCCESS")
	statusBadge.TextStyle = fyne.TextStyle{Bold: true}
	statusBadge.Alignment = fyne.TextAlignCenter

	// Badge container
	badge := container.NewStack(
		badgeBg,
		container.NewPadded(statusBadge),
	)
	badge.Resize(fyne.NewSize(90, 24))

	// Event type and time
	eventTitle := widget.NewLabel("")
	eventTitle.TextStyle = fyne.TextStyle{Bold: true}

	timestamp := widget.NewLabel("")
	timestamp.TextStyle = fyne.TextStyle{Italic: true}

	// Details text
	detailsLabel := widget.NewLabel("")
	detailsLabel.Wrapping = fyne.TextWrapWord

	// File/size info
	fileInfo := widget.NewLabel("")

	// Duration badge
	durationLabel := widget.NewLabel("")
	durationLabel.TextStyle = fyne.TextStyle{Monospace: true}

	// Actor info
	actorLabel := widget.NewLabel("")
	actorLabel.TextStyle = fyne.TextStyle{Italic: true}

	// Layout: Icon | Content | Badge
	leftSection := container.NewVBox(
		container.NewHBox(eventIcon, statusDot),
	)

	middleSection := container.NewVBox(
		container.NewHBox(eventTitle, layout.NewSpacer(), timestamp),
		detailsLabel,
		fileInfo,
		container.NewHBox(actorLabel, layout.NewSpacer(), durationLabel),
	)

	rightSection := container.NewVBox(
		badge,
	)

	content := container.NewBorder(
		nil, nil,
		container.NewPadded(leftSection),
		container.NewPadded(rightSection),
		container.NewPadded(middleSection),
	)

	// Stack background and content
	card := container.NewStack(
		cardBg,
		content,
	)

	// Add spacing between cards
	return container.NewPadded(card)
}

func updateModernEventCard(cardContainer *fyne.Container, e audit.Event) {
	// Get card stack
	card := cardContainer.Objects[0].(*fyne.Container)

	// Get background (first in stack)
	cardBg := card.Objects[0].(*canvas.Rectangle)

	// Get content border
	content := card.Objects[1].(*fyne.Container)

	// Get sections
	leftPadded := content.Objects[0].(*fyne.Container)
	leftSection := leftPadded.Objects[0].(*fyne.Container)
	leftHBox := leftSection.Objects[0].(*fyne.Container)
	eventIcon := leftHBox.Objects[0].(*widget.Icon)
	statusDot := leftHBox.Objects[1].(*canvas.Circle)

	middlePadded := content.Objects[1].(*fyne.Container)
	middleSection := middlePadded.Objects[0].(*fyne.Container)

	titleRow := middleSection.Objects[0].(*fyne.Container)
	eventTitle := titleRow.Objects[0].(*widget.Label)
	timestamp := titleRow.Objects[2].(*widget.Label)

	detailsLabel := middleSection.Objects[1].(*widget.Label)
	fileInfo := middleSection.Objects[2].(*widget.Label)

	bottomRow := middleSection.Objects[3].(*fyne.Container)
	actorLabel := bottomRow.Objects[0].(*widget.Label)
	durationLabel := bottomRow.Objects[2].(*widget.Label)

	rightPadded := content.Objects[2].(*fyne.Container)
	rightSection := rightPadded.Objects[0].(*fyne.Container)
	badge := rightSection.Objects[0].(*fyne.Container)
	badgeBg := badge.Objects[0].(*canvas.Rectangle)
	badgePadded := badge.Objects[1].(*fyne.Container)
	statusBadge := badgePadded.Objects[0].(*widget.Label)

	// Update status color and badge
	var statusColor color.Color
	var statusText string

	switch e.Status {
	case audit.StatusSuccess:
		statusColor = color.NRGBA{R: 76, G: 175, B: 80, A: 255} // Green
		badgeBg.FillColor = color.NRGBA{R: 76, G: 175, B: 80, A: 40}
		statusText = "SUCCESS"
	case audit.StatusFailed:
		statusColor = color.NRGBA{R: 244, G: 67, B: 54, A: 255} // Red
		badgeBg.FillColor = color.NRGBA{R: 244, G: 67, B: 54, A: 40}
		statusText = "FAILED"
	case audit.StatusStarted:
		statusColor = color.NRGBA{R: 33, G: 150, B: 243, A: 255} // Blue
		badgeBg.FillColor = color.NRGBA{R: 33, G: 150, B: 243, A: 40}
		statusText = "IN PROGRESS"
	default:
		statusColor = color.NRGBA{R: 158, G: 158, B: 158, A: 255} // Gray
		badgeBg.FillColor = color.NRGBA{R: 158, G: 158, B: 158, A: 40}
		statusText = "UNKNOWN"
	}

	statusDot.FillColor = statusColor
	statusDot.StrokeColor = statusColor
	statusBadge.SetText(statusText)
	badgeBg.Refresh()

	// Update icon based on event type
	switch e.EventType {
	case audit.EventTransferStarted, audit.EventTransferCompleted, audit.EventTransferFailed:
		eventIcon.SetResource(theme.MailSendIcon())
	case audit.EventConfigChanged:
		eventIcon.SetResource(theme.SettingsIcon())
	case audit.EventEngineStarted, audit.EventEngineStopped:
		eventIcon.SetResource(theme.ComputerIcon())
	case audit.EventFileArchived:
		eventIcon.SetResource(theme.FileIcon())
	default:
		eventIcon.SetResource(theme.DocumentIcon())
	}

	// Update title
	eventTitle.SetText(string(e.EventType))

	// Update timestamp
	timestamp.SetText(e.Timestamp.Local().Format("Jan 02, 15:04:05"))

	// Update details
	if e.Error != "" {
		detailsLabel.SetText(fmt.Sprintf("❌ Error: %s", e.Error))
	} else if e.Source != nil && e.Target != nil {
		detailsLabel.SetText(fmt.Sprintf("📤 %s@%s → 📥 %s@%s",
			e.Source.Username, e.Source.Host,
			e.Target.Username, e.Target.Host))
	} else if len(e.Details) > 0 {
		for k, v := range e.Details {
			detailsLabel.SetText(fmt.Sprintf("ℹ️ %s: %v", k, v))
			break
		}
	} else {
		detailsLabel.SetText("")
	}

	// Update file info
	if e.File != nil {
		fileInfo.SetText(fmt.Sprintf("📄 %s • %s", e.File.Name, humanBytes(e.File.Size)))
	} else {
		fileInfo.SetText("")
	}

	// Update actor
	actorLabel.SetText(fmt.Sprintf("👤 %s@%s", e.Actor.User, e.Actor.Hostname))

	// Update duration
	if e.Duration != nil {
		durationLabel.SetText(fmt.Sprintf("⏱️ %s", e.Duration.Round(time.Millisecond)))
	} else {
		durationLabel.SetText("")
	}

	// Subtle card hover effect (change background slightly)
	if e.Status == audit.StatusFailed {
		cardBg.FillColor = color.NRGBA{R: 35, G: 30, B: 30, A: 255}
	} else {
		cardBg.FillColor = color.NRGBA{R: 30, G: 33, B: 45, A: 255}
	}
	cardBg.Refresh()
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

func resolveAuditPath(configPath string, cfg *config.Config) string {
	return config.Resolve(configPath, cfg.AuditLog.LogFile)
}
