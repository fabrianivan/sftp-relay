package ui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/audit"
	"github.com/fabrianivan/sftp-relay/internal/config"
	"github.com/fabrianivan/sftp-relay/internal/relay"
)

// auditView lists audit log events in compact single-line rows.
type auditView struct {
	w       *Window
	content fyne.CanvasObject
	reader  *audit.Reader

	list         *widget.List
	events       []audit.Event
	filterType   *widget.Select
	filterStatus *widget.Select
	search       *widget.Entry
	summary      *widget.Label
	empty        *fyne.Container
	ready        bool
}

// auditRow is one event: status bar, icon, time, event, details, status.
type auditRow struct {
	widget.BaseWidget
	bar    *canvas.Rectangle
	icon   *widget.Icon
	when   *widget.Label
	event  *widget.Label
	detail *widget.Label
	status *pill
	box    *fyne.Container
}

func newAuditRow() *auditRow {
	r := &auditRow{
		bar:    canvas.NewRectangle(ColorMuted),
		icon:   widget.NewIcon(theme.DocumentIcon()),
		when:   widget.NewLabel("2006-01-02 15:04:05"),
		event:  widget.NewLabelWithStyle("", fyne.TextAlignLeading, styleBold),
		detail: mutedLabel(""),
		status: newPill("Success", toneSuccess),
	}
	r.bar.CornerRadius = 1.5
	r.when.TextStyle = styleMono
	r.when.Importance = widget.LowImportance
	r.event.Truncation = fyne.TextTruncateEllipsis

	h := r.when.MinSize().Height
	bar := container.NewCenter(container.NewGridWrap(fyne.NewSize(3, h-14), r.bar))
	icon := container.NewCenter(container.NewGridWrap(fyne.NewSquareSize(18), r.icon))
	left := hstack(6, bar, pad(0, 0, 4, 0, icon), r.when, container.NewGridWrap(fyne.NewSize(190, h), r.event))
	right := container.NewCenter(container.NewGridWrap(fyne.NewSize(84, 22), r.status))
	r.box = container.NewBorder(nil, nil, left, pad(0, 0, 8, 14, right), r.detail)
	r.ExtendBaseWidget(r)
	return r
}

func (r *auditRow) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(r.box) }

func (r *auditRow) set(e audit.Event) {
	t, label := toneNeutral, "Unknown"
	switch e.Status {
	case audit.StatusSuccess:
		t, label = toneSuccess, "Success"
	case audit.StatusFailed:
		t, label = toneDanger, "Failed"
	case audit.StatusStarted:
		t, label = toneInfo, "Running"
	}
	if r.bar.FillColor != t.color() {
		r.bar.FillColor = t.color()
		r.bar.Refresh()
	}
	r.status.Set(label, t)

	res := theme.DocumentIcon()
	switch e.EventType {
	case audit.EventTransferStarted, audit.EventTransferCompleted, audit.EventTransferFailed:
		res = theme.UploadIcon()
	case audit.EventFileArchived:
		res = theme.FileIcon()
	case audit.EventConfigChanged:
		res = theme.SettingsIcon()
	case audit.EventEngineStarted, audit.EventEngineStopped:
		res = theme.ComputerIcon()
	case audit.EventConnectionSuccess, audit.EventConnectionFailed, audit.EventAuthSuccess, audit.EventAuthFailure:
		res = theme.LoginIcon()
	}
	r.icon.SetResource(theme.NewColoredResource(res, theme.ColorNameDisabled))

	r.when.SetText(e.Timestamp.Local().Format("2006-01-02 15:04:05"))
	r.event.SetText(eventName(e.EventType))
	if e.Status == audit.StatusFailed {
		r.detail.Importance = widget.DangerImportance
	} else {
		r.detail.Importance = widget.LowImportance
	}
	r.detail.Text = eventDetails(e)
	r.detail.Refresh()
}

// eventName turns TRANSFER_COMPLETED into "Transfer completed".
func eventName(t audit.EventType) string {
	s := strings.ToLower(strings.ReplaceAll(string(t), "_", " "))
	if s == "" {
		return "Event"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// eventDetails is the one-line description of an event.
func eventDetails(e audit.Event) string {
	var parts []string
	switch {
	case e.File != nil:
		file := fmt.Sprintf("%s (%s)", e.File.Name, relay.HumanBytes(e.File.Size))
		if e.Duration != nil {
			file += fmt.Sprintf(" in %s", e.Duration.Round(10*time.Millisecond))
		}
		parts = append(parts, file)
	case e.Source != nil && e.Target != nil:
		parts = append(parts, fmt.Sprintf("%s@%s → %s@%s",
			e.Source.Username, e.Source.Host, e.Target.Username, e.Target.Host))
	case e.Error == "":
		parts = append(parts, fmt.Sprintf("%s@%s", e.Actor.User, e.Actor.Hostname))
	}
	if e.Error != "" {
		parts = append(parts, e.Error)
	}
	return strings.Join(parts, "  ·  ")
}

func newAuditView(w *Window) *auditView {
	v := &auditView{w: w, events: []audit.Event{}}

	cfg := w.ctl.Config()
	if cfg.AuditLogEnabled() {
		v.reader = audit.NewReader(resolveAuditPath(w.ctl.Path, cfg))
	}
	if v.reader == nil {
		v.content = page(
			pageHeader("Audit log", mutedLabel("A record of transfers, configuration changes and engine events.")),
			panel(emptyState("Audit logging is turned off", "Set auditLog.enabled to true in the configuration file to record events.")),
		)
		return v
	}

	onFilter := func(string) {
		if v.ready {
			v.refresh()
		}
	}
	v.search = searchEntry("Search by event, file name or error…", onFilter)
	v.filterType = widget.NewSelect([]string{"All", "Transfers", "Config", "Engine"}, onFilter)
	v.filterType.SetSelected("All")
	v.filterStatus = widget.NewSelect([]string{"All", "Success", "Failed", "Running"}, onFilter)
	v.filterStatus.SetSelected("All")
	v.summary = mutedLabel("")
	v.empty = emptyState("No events to show", "Nothing matches the current filters.")

	v.list = widget.NewList(
		func() int { return len(v.events) },
		func() fyne.CanvasObject { return newAuditRow() },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(v.events) {
				return
			}
			obj.(*auditRow).set(v.events[id])
		},
	)

	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), v.refresh)
	toolbar := container.NewBorder(nil, nil, nil,
		hstack(16, field("Type", v.filterType, 150), field("Status", v.filterStatus, 150)),
		v.search)
	body := container.NewBorder(pad(0, gap, 0, 0, toolbar), nil, nil, nil,
		panelInset(6, container.NewStack(v.list, v.empty)))

	v.content = page(pageHeader("Audit log", v.summary, refresh), body)
	v.ready = true
	v.refresh()
	return v
}

func (v *auditView) refresh() {
	if v.reader == nil || !v.ready {
		return
	}

	filter := audit.Filter{Limit: 500}
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
	switch v.filterStatus.Selected {
	case "Success":
		filter.Status = []audit.Status{audit.StatusSuccess}
	case "Failed":
		filter.Status = []audit.Status{audit.StatusFailed}
	case "Running":
		filter.Status = []audit.Status{audit.StatusStarted}
	}

	events, err := v.reader.Query(filter)
	if err != nil {
		v.w.ctl.Log.Warnf("Failed to read audit log: %v", err)
		events = nil
	}
	// Newest first, keeping only rows that match the search box.
	q := strings.ToLower(strings.TrimSpace(v.search.Text))
	v.events = v.events[:0]
	success, failed := 0, 0
	for i := len(events) - 1; i >= 0; i-- {
		e := events[i]
		if q != "" && !matchesSearch(e, q) {
			continue
		}
		switch e.Status {
		case audit.StatusSuccess:
			success++
		case audit.StatusFailed:
			failed++
		}
		v.events = append(v.events, e)
	}

	v.summary.SetText(fmt.Sprintf("%d events  ·  %d successful  ·  %d failed", len(v.events), success, failed))
	if len(v.events) == 0 {
		v.empty.Show()
	} else {
		v.empty.Hide()
	}
	v.list.Refresh()
}

// matchesSearch reports whether an event mentions the (lower-case) query.
func matchesSearch(e audit.Event, q string) bool {
	if strings.Contains(strings.ToLower(string(e.EventType)), q) ||
		strings.Contains(strings.ToLower(eventName(e.EventType)), q) ||
		strings.Contains(strings.ToLower(e.Error), q) {
		return true
	}
	return e.File != nil && strings.Contains(strings.ToLower(e.File.Name), q)
}

func resolveAuditPath(configPath string, cfg *config.Config) string {
	return config.Resolve(configPath, cfg.AuditLog.LogFile)
}
