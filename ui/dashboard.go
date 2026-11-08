package ui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/relay"
)

const maxActivity = 60

type activity struct {
	at   time.Time
	text string
	bad  bool
}

type transferRow struct {
	box *fyne.Container
	bar *widget.ProgressBar
}

type serverStatusRow struct {
	dot      *canvas.Circle
	title    *widget.Label
	subtitle *widget.Label
	badge    *widget.Label
	box      fyne.CanvasObject
}

func newServerStatusRow(title string) *serverStatusRow {
	r := &serverStatusRow{
		dot:      canvas.NewCircle(ColorMuted),
		title:    widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		subtitle: widget.NewLabel("—"),
		badge:    widget.NewLabelWithStyle("Idle", fyne.TextAlignTrailing, fyne.TextStyle{Bold: true}),
	}
	r.subtitle.Importance = widget.LowImportance
	r.subtitle.Truncation = fyne.TextTruncateEllipsis
	r.badge.Importance = widget.LowImportance

	dotObj := container.NewGridWrap(fyne.NewSize(10, 10), r.dot)
	header := container.NewBorder(nil, nil,
		container.NewHBox(container.NewCenter(dotObj), r.title),
		r.badge,
	)
	r.box = container.NewVBox(header, container.NewPadded(r.subtitle))
	return r
}

type dashboard struct {
	w       *Window
	content fyne.CanvasObject

	srcRow, dstRow, scanRow *serverStatusRow

	deliveredVal *canvas.Text
	failedVal    *canvas.Text
	volVal       *canvas.Text

	toggle, scan *widget.Button
	activeCard   *widget.Card
	transfers    *fyne.Container
	idle         *widget.Label
	rows         map[string]*transferRow
	feed         []activity
	feedList     *widget.List

	onStateChange func(running bool)
	lastRunning   *bool
}

func newDashboard(w *Window) *dashboard {
	d := &dashboard{w: w, rows: map[string]*transferRow{}}
	d.build()
	fyne.Do(func() {
		go d.listen()
		go d.poll()
	})
	return d
}

func statCard(title string, val *canvas.Text) fyne.CanvasObject {
	t := widget.NewLabel(title)
	t.Alignment = fyne.TextAlignCenter
	t.Importance = widget.LowImportance
	return widget.NewCard("", "", container.NewVBox(
		container.NewCenter(val),
		t,
	))
}

func (d *dashboard) build() {
	logo := canvas.NewImageFromResource(Icon())
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(52, 52))
	title := canvas.NewText(AppName, ColorAccent)
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}

	vBadge := canvas.NewText(" "+d.w.version+" ", ColorSuccess)
	vBadge.TextSize = 13
	vBadge.TextStyle = fyne.TextStyle{Bold: true}
	titleRow := container.NewHBox(title, container.NewCenter(vBadge))

	sub := widget.NewLabel("Automated SFTP synchronization, SHA-256 integrity verification, and archiving.")
	sub.Importance = widget.LowImportance
	sub.Wrapping = fyne.TextWrapWord
	header := container.NewBorder(nil, nil, container.NewPadded(logo), nil, container.NewVBox(titleRow, sub))

	d.srcRow = newServerStatusRow("SOURCE SERVER")
	d.dstRow = newServerStatusRow("TARGET SERVER")
	d.scanRow = newServerStatusRow("RELAY ENGINE")

	status := widget.NewCard("Connections & Engine", "", container.NewVBox(
		d.srcRow.box,
		widget.NewSeparator(),
		d.dstRow.box,
		widget.NewSeparator(),
		d.scanRow.box,
	))

	d.deliveredVal = canvas.NewText("0", ColorAccent)
	d.deliveredVal.TextSize = 28
	d.deliveredVal.TextStyle = fyne.TextStyle{Bold: true}

	d.failedVal = canvas.NewText("0", ColorMuted)
	d.failedVal.TextSize = 28
	d.failedVal.TextStyle = fyne.TextStyle{Bold: true}

	d.volVal = canvas.NewText("0 B", ColorForeground)
	d.volVal.TextSize = 22
	d.volVal.TextStyle = fyne.TextStyle{Bold: true}

	stats := container.NewGridWithColumns(3,
		statCard("Relayed Today", d.deliveredVal),
		statCard("Failed Today", d.failedVal),
		statCard("Data Transferred", d.volVal),
	)

	d.toggle = widget.NewButtonWithIcon("Start Relay", theme.MediaPlayIcon(), d.onToggle)
	d.toggle.Importance = widget.HighImportance
	d.scan = widget.NewButtonWithIcon("Scan Now", theme.ViewRefreshIcon(), func() {
		d.w.ctl.Engine.ScanNow()
	})
	controls := container.NewGridWithColumns(2, d.toggle, d.scan)

	d.idle = widget.NewLabelWithStyle("No transfers in progress · Standing by", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	d.idle.Importance = widget.LowImportance
	d.transfers = container.NewVBox(container.NewPadded(d.idle))
	d.activeCard = widget.NewCard("Active Transfers", "", d.transfers)

	d.feedList = widget.NewList(
		func() int { return len(d.feed) },
		func() fyne.CanvasObject {
			ts := widget.NewLabel("00:00:00")
			ts.TextStyle = fyne.TextStyle{Monospace: true}
			ts.Importance = widget.LowImportance
			msg := widget.NewLabel("")
			msg.Truncation = fyne.TextTruncateEllipsis
			return container.NewBorder(nil, nil, ts, nil, msg)
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(d.feed) {
				return
			}
			a := d.feed[len(d.feed)-1-i]
			c := o.(*fyne.Container)
			msg := c.Objects[0].(*widget.Label)
			ts := c.Objects[1].(*widget.Label)
			ts.SetText(a.at.Format(time.TimeOnly))
			msg.SetText(a.text)
			if a.bad {
				msg.Importance = widget.DangerImportance
			} else {
				msg.Importance = widget.MediumImportance
			}
		},
	)
	left := container.NewVBox(status, stats, controls)
	right := container.NewBorder(d.activeCard, nil, nil, nil,
		container.NewBorder(widget.NewLabelWithStyle("Live Activity Feed", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, d.feedList))
	body := container.New(layout.NewGridLayoutWithColumns(2), left, right)
	d.content = container.NewPadded(container.NewBorder(container.NewVBox(header, widget.NewSeparator()), nil, nil, nil, body))
	d.render(d.w.ctl.Engine.Status())
}

func (d *dashboard) onToggle() {
	eng := d.w.ctl.Engine
	if eng.Running() {
		go eng.Stop()
		return
	}
	if err := d.w.ctl.Config().Validate(); err != nil {
		dialog.ShowError(fmt.Errorf("fix the configuration first:\n%w", err), d.w.win)
		d.w.tabs.SelectIndex(2)
		return
	}
	eng.Start()
}

func (d *dashboard) addActivity(text string, bad bool) {
	d.feed = append(d.feed, activity{at: time.Now(), text: text, bad: bad})
	if len(d.feed) > maxActivity {
		d.feed = d.feed[len(d.feed)-maxActivity:]
	}
	d.feedList.Refresh()
}

// listen applies engine events on the UI thread.
func (d *dashboard) listen() {
	events, _ := d.w.ctl.Engine.Subscribe()
	for ev := range events {
		ev := ev
		fyne.Do(func() { d.apply(ev) })
	}
}

func (d *dashboard) apply(ev relay.Event) {
	switch ev.Kind {
	case relay.TransferStarted:
		d.row(ev.File)
		d.addActivity("Started "+ev.File+" ("+relay.HumanBytes(ev.Size)+")", false)
	case relay.TransferProgress:
		r := d.row(ev.File)
		if ev.Size > 0 {
			r.bar.SetValue(float64(ev.Transferred) / float64(ev.Size))
		}
		speed := relay.HumanBytes(int64(ev.Speed)) + "/s"
		r.bar.TextFormatter = func() string { return fmt.Sprintf("%.0f%%  ·  %s", r.bar.Value*100, speed) }
		r.bar.Refresh()
	case relay.TransferDone:
		d.dropRow(ev.File)
		d.addActivity("✓ "+ev.File+" "+ev.Message, false)
		d.w.history.refreshIfVisible()
	case relay.TransferFailed:
		d.dropRow(ev.File)
		d.addActivity("✗ "+ev.File+": "+ev.Message, ev.Err)
		d.w.history.refreshIfVisible()
	case relay.ScanFinished, relay.StateChanged:
		if !ev.Quiet {
			d.addActivity(ev.Message, ev.Err)
		}
		d.render(d.w.ctl.Engine.Status())
	}
}

func (d *dashboard) row(file string) *transferRow {
	if r, ok := d.rows[file]; ok {
		return r
	}
	name := widget.NewLabel(file)
	name.Truncation = fyne.TextTruncateEllipsis
	bar := widget.NewProgressBar()
	r := &transferRow{box: container.NewVBox(name, bar), bar: bar}
	d.rows[file] = r
	d.transfers.Remove(d.idle)
	d.transfers.Add(r.box)
	d.activeCard.SetTitle(fmt.Sprintf("Active Transfers (%d)", len(d.rows)))
	return r
}

func (d *dashboard) dropRow(file string) {
	if r, ok := d.rows[file]; ok {
		d.transfers.Remove(r.box)
		delete(d.rows, file)
	}
	if len(d.rows) == 0 && len(d.transfers.Objects) == 0 {
		d.transfers.Add(d.idle)
		d.activeCard.SetTitle("Active Transfers")
	} else if len(d.rows) > 0 {
		d.activeCard.SetTitle(fmt.Sprintf("Active Transfers (%d)", len(d.rows)))
	}
}

func (d *dashboard) poll() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		s := d.w.ctl.Engine.Status()
		fyne.Do(func() { d.render(s) })
	}
}

func setDot(c *canvas.Circle, col color.Color) {
	if c.FillColor != col {
		c.FillColor = col
		c.Refresh()
	}
}

func (d *dashboard) render(s relay.Status) {
	cfg := d.w.ctl.Config()
	d.srcRow.subtitle.SetText(fmt.Sprintf("%s  →  %s", cfg.Source.Addr(), cfg.Source.Directory))
	if s.SourceConnected {
		setDot(d.srcRow.dot, ColorSuccess)
		d.srcRow.badge.SetText("Connected")
		d.srcRow.badge.Importance = widget.SuccessImportance
	} else {
		setDot(d.srcRow.dot, ColorMuted)
		d.srcRow.badge.SetText("Idle")
		d.srcRow.badge.Importance = widget.LowImportance
	}

	switch {
	case !s.TargetEnabled:
		setDot(d.dstRow.dot, ColorWarning)
		d.dstRow.subtitle.SetText("Archive-only mode (no remote target)")
		d.dstRow.badge.SetText("Disabled")
		d.dstRow.badge.Importance = widget.WarningImportance
	case s.TargetConnected:
		setDot(d.dstRow.dot, ColorSuccess)
		d.dstRow.subtitle.SetText(fmt.Sprintf("%s  →  %s", cfg.Target.Addr(), cfg.Target.Directory))
		d.dstRow.badge.SetText("Connected")
		d.dstRow.badge.Importance = widget.SuccessImportance
	default:
		setDot(d.dstRow.dot, ColorMuted)
		d.dstRow.subtitle.SetText(fmt.Sprintf("%s  →  %s", cfg.Target.Addr(), cfg.Target.Directory))
		d.dstRow.badge.SetText("Idle")
		d.dstRow.badge.Importance = widget.LowImportance
	}

	switch {
	case s.Scanning:
		setDot(d.scanRow.dot, ColorAccent)
		d.scanRow.badge.SetText("Scanning…")
		d.scanRow.badge.Importance = widget.HighImportance
	case s.Running && !s.ScheduleActive:
		setDot(d.scanRow.dot, ColorWarning)
		d.scanRow.badge.SetText("Waiting Schedule")
		d.scanRow.badge.Importance = widget.WarningImportance
	case s.Running:
		setDot(d.scanRow.dot, ColorSuccess)
		d.scanRow.badge.SetText("Running")
		d.scanRow.badge.Importance = widget.SuccessImportance
	default:
		setDot(d.scanRow.dot, ColorError)
		d.scanRow.badge.SetText("Paused")
		d.scanRow.badge.Importance = widget.DangerImportance
	}

	sub := fmt.Sprintf("Poll interval: %ds", cfg.PollInterval)
	if !s.LastScan.IsZero() {
		sub += "  ·  Last: " + s.LastScan.Format(time.TimeOnly)
	}
	if s.Running && !s.NextScan.IsZero() {
		if in := time.Until(s.NextScan).Round(time.Second); in > 0 {
			sub += fmt.Sprintf("  ·  Next: %s", in)
		}
	}
	d.scanRow.subtitle.SetText(sub)

	d.deliveredVal.Text = fmt.Sprint(s.Today.Delivered)
	d.deliveredVal.Refresh()

	d.failedVal.Text = fmt.Sprint(s.Today.Failed)
	if s.Today.Failed > 0 {
		d.failedVal.Color = ColorError
	} else {
		d.failedVal.Color = ColorMuted
	}
	d.failedVal.Refresh()

	d.volVal.Text = relay.HumanBytes(s.Today.Bytes)
	d.volVal.Refresh()

	if d.lastRunning == nil || *d.lastRunning != s.Running {
		running := s.Running
		d.lastRunning = &running
		if running {
			d.toggle.SetText("Pause Relay")
			d.toggle.SetIcon(theme.MediaPauseIcon())
			d.toggle.Importance = widget.MediumImportance
		} else {
			d.toggle.SetText("Start Relay")
			d.toggle.SetIcon(theme.MediaPlayIcon())
			d.toggle.Importance = widget.HighImportance
		}
		d.toggle.Refresh()
		if d.onStateChange != nil {
			d.onStateChange(running)
		}
	}
}
