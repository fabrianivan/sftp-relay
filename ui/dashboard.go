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
	r.subtitle.Importance = widget.MediumImportance
	r.subtitle.Truncation = fyne.TextTruncateEllipsis
	r.badge.Importance = widget.MediumImportance

	// Enhanced dot with better visual
	dotObj := container.NewGridWrap(fyne.NewSize(12, 12), r.dot)

	// Improved header layout
	header := container.NewBorder(nil, nil,
		container.NewHBox(
			container.NewCenter(dotObj),
			r.title,
		),
		r.badge,
	)

	// Better spacing for subtitle
	r.box = container.NewVBox(
		header,
		container.NewPadded(r.subtitle),
	)
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
	// Create enhanced stat card with better visual hierarchy
	t := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{})
	t.Importance = widget.MediumImportance

	// Add visual container for value
	valueContainer := container.NewCenter(
		container.NewVBox(
			container.NewCenter(val),
			container.NewPadded(t),
		),
	)

	card := widget.NewCard("", "", valueContainer)
	return card
}

func (d *dashboard) build() {
	// Modern logo with glow effect
	logo := canvas.NewImageFromResource(Icon())
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(64, 64))

	// Title with gradient effect (simulated with color)
	title := canvas.NewText(AppName, ColorAccent)
	title.TextSize = 32
	title.TextStyle = fyne.TextStyle{Bold: true}

	// Modern version badge
	vBadge := widget.NewLabelWithStyle("v"+d.w.version, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	vBadge.Importance = widget.SuccessImportance

	badgeContainer := container.NewStack(
		canvas.NewRectangle(ColorSuccessGlow),
		container.NewPadded(vBadge),
	)

	titleRow := container.NewHBox(
		title,
		layout.NewSpacer(),
		badgeContainer,
	)

	// Improved subtitle with better typography
	sub := widget.NewLabel("Automated SFTP synchronization · SHA-256 integrity · Smart archiving")
	sub.Importance = widget.MediumImportance
	sub.Wrapping = fyne.TextWrapWord

	// Enhanced header layout
	logoWithPadding := container.NewPadded(container.NewCenter(logo))
	headerContent := container.NewVBox(
		titleRow,
		container.NewPadded(sub),
	)
	header := container.NewBorder(nil, nil, logoWithPadding, nil, headerContent)

	d.srcRow = newServerStatusRow("📥  SOURCE SERVER")
	d.dstRow = newServerStatusRow("📤  TARGET SERVER")
	d.scanRow = newServerStatusRow("⚡  RELAY ENGINE")

	// Enhanced status card with better spacing
	statusTitle := canvas.NewText("Connection Status", ColorForeground)
	statusTitle.TextSize = 18
	statusTitle.TextStyle = fyne.TextStyle{Bold: true}

	status := widget.NewCard("", "", container.NewVBox(
		statusTitle,
		container.NewPadded(widget.NewSeparator()),
		d.srcRow.box,
		container.NewPadded(widget.NewSeparator()),
		d.dstRow.box,
		container.NewPadded(widget.NewSeparator()),
		d.scanRow.box,
	))

	// Enhanced stat values with better typography
	d.deliveredVal = canvas.NewText("0", ColorAccent)
	d.deliveredVal.TextSize = 36
	d.deliveredVal.TextStyle = fyne.TextStyle{Bold: true}

	d.failedVal = canvas.NewText("0", ColorMuted)
	d.failedVal.TextSize = 36
	d.failedVal.TextStyle = fyne.TextStyle{Bold: true}

	d.volVal = canvas.NewText("0 B", ColorForeground)
	d.volVal.TextSize = 32
	d.volVal.TextStyle = fyne.TextStyle{Bold: true}

	// Modern stats grid with better labels
	statsTitle := canvas.NewText("📊  Today's Statistics", ColorForeground)
	statsTitle.TextSize = 18
	statsTitle.TextStyle = fyne.TextStyle{Bold: true}

	stats := container.NewVBox(
		statsTitle,
		container.NewPadded(widget.NewSeparator()),
		container.NewGridWithColumns(3,
			statCard("Files Relayed", d.deliveredVal),
			statCard("Failed Transfers", d.failedVal),
			statCard("Data Volume", d.volVal),
		),
	)

	// Enhanced control buttons with modern styling
	d.toggle = widget.NewButtonWithIcon("Start Relay", theme.MediaPlayIcon(), d.onToggle)
	d.toggle.Importance = widget.HighImportance

	d.scan = widget.NewButtonWithIcon("Scan Now", theme.ViewRefreshIcon(), func() {
		d.w.ctl.Engine.ScanNow()
	})
	d.scan.Importance = widget.MediumImportance

	controlTitle := canvas.NewText("🎮  Control Panel", ColorForeground)
	controlTitle.TextSize = 18
	controlTitle.TextStyle = fyne.TextStyle{Bold: true}

	controls := container.NewVBox(
		controlTitle,
		container.NewPadded(widget.NewSeparator()),
		container.NewGridWithColumns(2, d.toggle, d.scan),
	)

	// Enhanced active transfers section
	d.idle = widget.NewLabelWithStyle("💤  No transfers in progress · Standing by", fyne.TextAlignCenter, fyne.TextStyle{Italic: true})
	d.idle.Importance = widget.MediumImportance

	d.transfers = container.NewVBox(container.NewPadded(d.idle))
	d.activeCard = widget.NewCard("🔄  Active Transfers", "", d.transfers)

	// Modern activity feed
	feedTitle := canvas.NewText("📡  Live Activity Feed", ColorForeground)
	feedTitle.TextSize = 18
	feedTitle.TextStyle = fyne.TextStyle{Bold: true}

	d.feedList = widget.NewList(
		func() int { return len(d.feed) },
		func() fyne.CanvasObject {
			ts := widget.NewLabel("00:00:00")
			ts.TextStyle = fyne.TextStyle{Monospace: true}
			ts.Importance = widget.MediumImportance
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

	// Enhanced two-column layout with better proportions
	left := container.NewVBox(
		container.NewPadded(status),
		container.NewPadded(stats),
		container.NewPadded(controls),
	)

	right := container.NewBorder(
		container.NewPadded(d.activeCard),
		nil, nil, nil,
		container.NewBorder(
			container.NewPadded(feedTitle),
			nil, nil, nil,
			d.feedList,
		),
	)

	body := container.New(layout.NewGridLayoutWithColumns(2), left, right)

	// Final layout with enhanced spacing
	d.content = container.NewPadded(
		container.NewBorder(
			container.NewVBox(
				header,
				container.NewPadded(widget.NewSeparator()),
			),
			nil, nil, nil,
			body,
		),
	)
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
	d.activeCard.SetTitle(fmt.Sprintf("🔄  Active Transfers (%d)", len(d.rows)))
	return r
}

func (d *dashboard) dropRow(file string) {
	if r, ok := d.rows[file]; ok {
		d.transfers.Remove(r.box)
		delete(d.rows, file)
	}
	if len(d.rows) == 0 && len(d.transfers.Objects) == 0 {
		d.transfers.Add(d.idle)
		d.activeCard.SetTitle("🔄  Active Transfers")
	} else if len(d.rows) > 0 {
		d.activeCard.SetTitle(fmt.Sprintf("🔄  Active Transfers (%d)", len(d.rows)))
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
