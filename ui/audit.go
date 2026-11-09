package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
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
}

func newAuditView(w *Window) *auditView {
	v := &auditView{
		w:      w,
		events: []audit.Event{},
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
		v.refresh()
	})
	v.filterType.SetSelected("All Events")

	v.filterStatus = widget.NewSelect([]string{
		"All Status",
		"Success",
		"Failed",
		"Started",
	}, func(string) {
		v.refresh()
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
			return container.NewVBox(
				widget.NewRichTextFromMarkdown(""),
				widget.NewSeparator(),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id >= len(v.events) {
				return
			}
			vbox := obj.(*fyne.Container)
			event := v.events[id]

			// Format event as markdown
			md := formatAuditEvent(event)
			richText := vbox.Objects[0].(*widget.RichText)
			richText.ParseMarkdown(md)
		},
	)

	return v
}

func (v *auditView) build() fyne.CanvasObject {
	if v.reader == nil {
		return container.NewCenter(
			widget.NewLabel("Audit logging is disabled or not available"),
		)
	}

	// Initial load
	v.refresh()

	// Filter controls
	filters := container.NewBorder(
		nil, nil,
		container.NewHBox(
			widget.NewLabel("Type:"),
			v.filterType,
			widget.NewLabel("Status:"),
			v.filterStatus,
			widget.NewLabel("Limit:"),
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
	if v.reader == nil {
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

func formatAuditEvent(e audit.Event) string {
	// Status icon
	icon := "✅"
	if e.Status == audit.StatusFailed {
		icon = "❌"
	} else if e.Status == audit.StatusStarted {
		icon = "▶️"
	}

	// Format timestamp
	ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")

	// Build markdown
	md := fmt.Sprintf("## %s %s - %s\n\n", icon, e.EventType, e.Status)
	md += fmt.Sprintf("**Time:** %s  \n", ts)
	md += fmt.Sprintf("**Actor:** %s@%s (PID %d)  \n", e.Actor.User, e.Actor.Hostname, e.Actor.PID)

	if e.File != nil {
		md += fmt.Sprintf("**File:** %s (%s)  \n", e.File.Name, humanBytes(e.File.Size))
		if e.File.SHA256 != "" {
			md += fmt.Sprintf("**SHA-256:** `%s...`  \n", e.File.SHA256[:12])
		}
	}

	if e.Source != nil {
		md += fmt.Sprintf("**Source:** %s@%s:%d%s  \n",
			e.Source.Username, e.Source.Host, e.Source.Port, e.Source.Directory)
	}

	if e.Target != nil {
		md += fmt.Sprintf("**Target:** %s@%s:%d%s  \n",
			e.Target.Username, e.Target.Host, e.Target.Port, e.Target.Directory)
	}

	if e.Duration != nil {
		md += fmt.Sprintf("**Duration:** %s  \n", e.Duration.Round(time.Millisecond))
	}

	if e.Error != "" {
		md += fmt.Sprintf("**Error:** `%s`  \n", e.Error)
	}

	if len(e.Details) > 0 {
		md += "\n**Details:**  \n"
		for k, v := range e.Details {
			md += fmt.Sprintf("- %s: %v  \n", k, v)
		}
	}

	return md
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
