package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/audit"
	"github.com/fabrianivan/sftp-relay/internal/config"
)

// auditView displays audit log events in a table format.
type auditView struct {
	w      *Window
	reader *audit.Reader

	table        *widget.Table
	events       []audit.Event
	filterType   *widget.Select
	filterStatus *widget.Select
	searchEntry  *widget.Entry
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

	// Search entry
	v.searchEntry = widget.NewEntry()
	v.searchEntry.SetPlaceHolder("Search event type or details...")
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
		"Started",
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

	// Export button
	v.exportBtn = widget.NewButtonWithIcon("Export", theme.DocumentSaveIcon(), func() {
		v.exportDialog()
	})

	// Count label
	v.countLabel = widget.NewLabel("0 delivered, 0 failed. Showing 0 of 0 records")

	// Create table
	v.table = widget.NewTable(
		func() (int, int) {
			return len(v.events) + 1, 6 // +1 for header row, 6 columns
		},
		func() fyne.CanvasObject {
			return widget.NewLabel("")
		},
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)

			// Header row
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
				switch id.Col {
				case 0:
					label.SetText("Time")
				case 1:
					label.SetText("Event")
				case 2:
					label.SetText("Status")
				case 3:
					label.SetText("File/Target")
				case 4:
					label.SetText("Duration")
				case 5:
					label.SetText("Details")
				}
				return
			}

			// Data rows
			if id.Row-1 >= len(v.events) {
				label.SetText("")
				return
			}

			event := v.events[id.Row-1]
			label.TextStyle = fyne.TextStyle{}

			switch id.Col {
			case 0: // Time
				label.SetText(event.Timestamp.Local().Format("15:04:05"))
			case 1: // Event
				label.SetText(string(event.EventType))
			case 2: // Status
				label.SetText(string(event.Status))
			case 3: // File/Target
				if event.File != nil {
					label.SetText(event.File.Name)
				} else if event.Target != nil {
					label.SetText(fmt.Sprintf("%s@%s", event.Target.Username, event.Target.Host))
				} else {
					label.SetText("-")
				}
			case 4: // Duration
				if event.Duration != nil {
					label.SetText(event.Duration.Round(time.Millisecond).String())
				} else {
					label.SetText("-")
				}
			case 5: // Details
				if event.Error != "" {
					label.SetText(event.Error)
				} else if event.File != nil {
					label.SetText(humanBytes(event.File.Size))
				} else if len(event.Details) > 0 {
					for k, v := range event.Details {
						label.SetText(fmt.Sprintf("%s: %v", k, v))
						break
					}
				} else {
					label.SetText("-")
				}
			}
		},
	)

	// Set column widths
	v.table.SetColumnWidth(0, 80)  // Time
	v.table.SetColumnWidth(1, 180) // Event
	v.table.SetColumnWidth(2, 100) // Status
	v.table.SetColumnWidth(3, 200) // File/Target
	v.table.SetColumnWidth(4, 100) // Duration
	v.table.SetColumnWidth(5, 300) // Details

	v.initialized = true

	return v
}

func (v *auditView) build() fyne.CanvasObject {
	if v.reader == nil {
		emptyLabel := widget.NewLabel("Audit logging is disabled")
		emptyLabel.TextStyle = fyne.TextStyle{Bold: true}
		hintLabel := widget.NewLabel("Enable audit logging in configuration to track system events")

		return container.NewCenter(
			container.NewVBox(
				emptyLabel,
				hintLabel,
			),
		)
	}

	// Initial load
	v.refresh()

	// Title and subtitle
	title := widget.NewLabel("Audit Log")
	title.TextStyle = fyne.TextStyle{Bold: true}
	subtitle := widget.NewLabel("Search and filter recorded audit events")

	// Filter bar
	filterLabel := widget.NewLabel("Filter")
	filterLabel.TextStyle = fyne.TextStyle{Bold: true}

	filterBar := container.NewHBox(
		filterLabel,
		v.filterType,
		v.filterStatus,
		layout.NewSpacer(),
		v.refreshBtn,
		v.exportBtn,
	)

	// Search bar
	searchBar := container.NewBorder(
		nil, nil,
		widget.NewLabel("Search"),
		nil,
		v.searchEntry,
	)

	// Status bar at bottom
	statusBar := container.NewHBox(
		v.countLabel,
	)

	// Main layout
	content := container.NewBorder(
		container.NewVBox(
			title,
			subtitle,
			widget.NewSeparator(),
			searchBar,
			filterBar,
			widget.NewSeparator(),
		),
		container.NewVBox(
			widget.NewSeparator(),
			statusBar,
		),
		nil, nil,
		v.table,
	)

	return content
}

func (v *auditView) refresh() {
	if v.reader == nil || v.table == nil {
		return
	}

	filter := audit.Filter{
		Limit: 1000,
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
	case "Started":
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
				// Search in event type, error, file name
				if contains(string(e.EventType), searchText) ||
					contains(e.Error, searchText) ||
					(e.File != nil && contains(e.File.Name, searchText)) {
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
	delivered := 0
	failed := 0
	for _, e := range v.events {
		if e.Status == audit.StatusSuccess {
			delivered++
		} else if e.Status == audit.StatusFailed {
			failed++
		}
	}

	v.countLabel.SetText(fmt.Sprintf("%d delivered, %d failed. Showing %d of %d records",
		delivered, failed, len(v.events), len(v.events)))
	v.table.Refresh()
}

func (v *auditView) exportDialog() {
	content := container.NewVBox(
		widget.NewLabel("Export Audit Log"),
		widget.NewLabel(fmt.Sprintf("Ready to export %d events", len(v.events))),
		widget.NewLabel(""),
		widget.NewLabel("Export format: JSON"),
		widget.NewLabel("This feature will be available in the next update."),
	)

	dialog := widget.NewModalPopUp(
		content,
		v.w.win.Canvas(),
	)

	okBtn := widget.NewButton("OK", func() {
		dialog.Hide()
	})

	content.Add(okBtn)
	dialog.Resize(fyne.NewSize(400, 200))
	dialog.Show()
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (substr == "" ||
		len(s) > 0 && (s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || contains(s[1:], substr))))
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
