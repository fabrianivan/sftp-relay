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

// auditView displays audit log events in a clean, compact list.
type auditView struct {
	w      *Window
	reader *audit.Reader

	list         *widget.List
	events       []audit.Event
	filterType   *widget.Select
	filterStatus *widget.Select
	searchEntry  *widget.Entry
	refreshBtn   *widget.Button
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

	// Search entry
	v.searchEntry = widget.NewEntry()
	v.searchEntry.SetPlaceHolder("Search events...")
	v.searchEntry.OnChanged = func(string) {
		if v.initialized {
			v.refresh()
		}
	}

	// Filter type dropdown
	v.filterType = widget.NewSelect([]string{
		"All",
		"Transfers",
		"Config",
		"Engine",
	}, func(string) {
		if v.initialized {
			v.refresh()
		}
	})
	v.filterType.SetSelected("All")

	// Filter status dropdown
	v.filterStatus = widget.NewSelect([]string{
		"All",
		"Success",
		"Failed",
		"Running",
	}, func(string) {
		if v.initialized {
			v.refresh()
		}
	})
	v.filterStatus.SetSelected("All")

	// Refresh button
	v.refreshBtn = widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), func() {
		v.refresh()
	})

	// Stats label
	v.statsLabel = widget.NewLabel("0 events")

	// Create compact list
	v.list = widget.NewList(
		func() int { return len(v.events) },
		func() fyne.CanvasObject {
			return createCompactEventRow()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(v.events) {
				return
			}
			updateCompactEventRow(obj.(*fyne.Container), v.events[id])
		},
	)

	v.initialized = true
	return v
}

func (v *auditView) build() fyne.CanvasObject {
	if v.reader == nil {
		return container.NewCenter(
			container.NewVBox(
				widget.NewLabel("Audit Logging Disabled"),
				widget.NewLabel("Enable in Settings to track events"),
			),
		)
	}

	// Initial load
	v.refresh()

	// Header
	title := widget.NewLabel("Audit Log")
	title.TextStyle = fyne.TextStyle{Bold: true}
	
	subtitle := widget.NewLabel("Search and filter recorded events")

	// Filter bar - horizontal compact layout
	filterBar := container.NewHBox(
		widget.NewLabel("Filter"),
		v.filterType,
		v.filterStatus,
		layout.NewSpacer(),
		v.refreshBtn,
	)

	// Top section
	topSection := container.NewVBox(
		title,
		subtitle,
		widget.NewSeparator(),
		v.searchEntry,
		filterBar,
		widget.NewSeparator(),
	)

	// Bottom stats
	bottomSection := container.NewVBox(
		widget.NewSeparator(),
		v.statsLabel,
	)

	// Main layout
	return container.NewBorder(
		topSection,
		bottomSection,
		nil, nil,
		v.list,
	)
}

func (v *auditView) refresh() {
	if v.reader == nil || v.list == nil {
		return
	}

	filter := audit.Filter{Limit: 500}

	// Apply type filter
	switch v.filterType.Selected {
	case "Transfers":
		filter.EventTypes = []audit.EventType{
			audit.EventTransferStarted,
			audit.EventTransferCompleted,
			audit.EventTransferFailed,
			audit.EventFileArchived,
		}
	case "Config":
		filter.EventTypes = []audit.EventType{audit.EventConfigChanged}
	case "Engine":
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
	case "Running":
		filter.Status = []audit.Status{audit.StatusStarted}
	}

	// Query events
	events, err := v.reader.Query(filter)
	if err != nil {
		v.w.ctl.Log.Warnf("Failed to read audit log: %v", err)
		v.events = []audit.Event{}
	} else {
		// Filter by search
		if search := v.searchEntry.Text; search != "" {
			filtered := []audit.Event{}
			for _, e := range events {
				if matchesSearch(e, search) {
					filtered = append(filtered, e)
				}
			}
			events = filtered
		}

		// Reverse - newest first
		v.events = make([]audit.Event, len(events))
		for i, e := range events {
			v.events[len(events)-1-i] = e
		}
	}

	// Update stats
	success, failed := 0, 0
	for _, e := range v.events {
		if e.Status == audit.StatusSuccess {
			success++
		} else if e.Status == audit.StatusFailed {
			failed++
		}
	}

	v.statsLabel.SetText(fmt.Sprintf("%d events  •  %d success  •  %d failed", 
		len(v.events), success, failed))
	v.list.Refresh()
}

func matchesSearch(e audit.Event, search string) bool {
	s := toLower(search)
	if contains(toLower(string(e.EventType)), s) {
		return true
	}
	if contains(toLower(e.Error), s) {
		return true
	}
	if e.File != nil && contains(toLower(e.File.Name), s) {
		return true
	}
	return false
}

func toLower(s string) string {
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

// createCompactEventRow creates a single-line compact row
func createCompactEventRow() fyne.CanvasObject {
	// Status indicator (small colored bar on left)
	statusBar := canvas.NewRectangle(color.NRGBA{R: 76, G: 175, B: 80, A: 255})
	statusBar.SetMinSize(fyne.NewSize(3, 32))

	// Icon
	icon := widget.NewIcon(theme.DocumentIcon())

	// Time
	timeLabel := widget.NewLabel("00:00:00")
	timeLabel.TextStyle = fyne.TextStyle{Monospace: true}

	// Event name
	eventLabel := widget.NewLabel("")
	eventLabel.TextStyle = fyne.TextStyle{Bold: true}

	// Details
	detailsLabel := widget.NewLabel("")

	// Status badge
	statusBadge := widget.NewLabel("SUCCESS")
	statusBadge.TextStyle = fyne.TextStyle{Bold: true}

	// Compact horizontal layout: [Bar][Icon] Time Event Details [Status]
	row := container.NewBorder(
		nil, nil,
		container.NewHBox(statusBar, icon, timeLabel),
		statusBadge,
		container.NewHBox(eventLabel, widget.NewLabel("•"), detailsLabel),
	)

	return container.NewPadded(row)
}

func updateCompactEventRow(row *fyne.Container, e audit.Event) {
	// Get the inner border container
	innerRow := row.Objects[0].(*fyne.Container)
	
	// Left section
	leftBox := innerRow.Objects[0].(*fyne.Container)
	statusBar := leftBox.Objects[0].(*canvas.Rectangle)
	icon := leftBox.Objects[1].(*widget.Icon)
	timeLabel := leftBox.Objects[2].(*widget.Label)
	
	// Center section
	centerBox := innerRow.Objects[1].(*fyne.Container)
	eventLabel := centerBox.Objects[0].(*widget.Label)
	detailsLabel := centerBox.Objects[2].(*widget.Label)
	
	// Right section
	statusBadge := innerRow.Objects[2].(*widget.Label)

	// Update colors and status
	var statusColor color.Color
	var statusText string
	
	switch e.Status {
	case audit.StatusSuccess:
		statusColor = color.NRGBA{R: 76, G: 175, B: 80, A: 255}
		statusText = "✓ SUCCESS"
	case audit.StatusFailed:
		statusColor = color.NRGBA{R: 244, G: 67, B: 54, A: 255}
		statusText = "✗ FAILED"
	case audit.StatusStarted:
		statusColor = color.NRGBA{R: 33, G: 150, B: 243, A: 255}
		statusText = "⟳ RUNNING"
	default:
		statusColor = color.NRGBA{R: 158, G: 158, B: 158, A: 255}
		statusText = "UNKNOWN"
	}
	
	statusBar.FillColor = statusColor
	statusBar.Refresh()
	statusBadge.SetText(statusText)

	// Update icon
	switch e.EventType {
	case audit.EventTransferStarted, audit.EventTransferCompleted, audit.EventTransferFailed:
		icon.SetResource(theme.MailSendIcon())
	case audit.EventConfigChanged:
		icon.SetResource(theme.SettingsIcon())
	case audit.EventEngineStarted, audit.EventEngineStopped:
		icon.SetResource(theme.ComputerIcon())
	case audit.EventFileArchived:
		icon.SetResource(theme.FileIcon())
	default:
		icon.SetResource(theme.DocumentIcon())
	}

	// Update time
	timeLabel.SetText(e.Timestamp.Local().Format("15:04:05"))

	// Update event name
	eventLabel.SetText(string(e.EventType))

	// Update details - compact single line
	var details string
	if e.Error != "" {
		details = e.Error
	} else if e.File != nil {
		details = fmt.Sprintf("%s (%s)", e.File.Name, humanBytes(e.File.Size))
		if e.Duration != nil {
			details += fmt.Sprintf(" in %s", e.Duration.Round(10*time.Millisecond))
		}
	} else if e.Source != nil && e.Target != nil {
		details = fmt.Sprintf("%s@%s → %s@%s", 
			e.Source.Username, e.Source.Host,
			e.Target.Username, e.Target.Host)
	} else {
		details = fmt.Sprintf("%s@%s", e.Actor.User, e.Actor.Hostname)
	}
	
	// Truncate if too long
	if len(details) > 80 {
		details = details[:77] + "..."
	}
	detailsLabel.SetText(details)
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
