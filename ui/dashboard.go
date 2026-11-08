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

type dashboard struct {
	w       *Window
	content fyne.CanvasObject

	srcDot, dstDot, scanDot *canvas.Circle
	srcText, dstText        *widget.Label
	scanText, nextText      *widget.Label
	delivered, failed, vol  *widget.Label
	toggle, scan            *widget.Button
	transfers               *fyne.Container
	idle                    *widget.Label
	rows                    map[string]*transferRow
	feed                    []activity
	feedList                *widget.List

	onStateChange func(running bool)
	lastRunning   *bool
}

func newDashboard(w *Window) *dashboard {
	d := &dashboard{w: w, rows: map[string]*transferRow{}}
	d.build()
	go d.listen()
	go d.poll()
	return d
}

func dot() *canvas.Circle {
	c := canvas.NewCircle(ColorMuted)
	return c
}

func dotCell(c *canvas.Circle) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(12, 12), c)
}

func statCard(title string, value *widget.Label) fyne.CanvasObject {
	value.TextStyle = fyne.TextStyle{Bold: true}
	value.Alignment = fyne.TextAlignCenter
	t := widget.NewLabel(title)
	t.Alignment = fyne.TextAlignCenter
	t.Importance = widget.LowImportance
	return widget.NewCard("", "", container.NewVBox(value, t))
}

func (d *dashboard) build() {
	logo := canvas.NewImageFromResource(Icon())
	logo.FillMode = canvas.ImageFillContain
	logo.SetMinSize(fyne.NewSize(52, 52))
	title := canvas.NewText(AppName, ColorAccent)
	title.TextSize = 24
	title.TextStyle = fyne.TextStyle{Bold: true}
	sub := widget.NewLabel("Moves new files from the source server to the target, verifies them, then archives the originals.")
	sub.Importance = widget.LowImportance
	sub.Wrapping = fyne.TextWrapWord
	header := container.NewBorder(nil, nil, logo, nil, container.NewVBox(title, sub))

	d.srcDot, d.dstDot, d.scanDot = dot(), dot(), dot()
	d.srcText, d.dstText = widget.NewLabel("Source: —"), widget.NewLabel("Target: —")
	d.scanText, d.nextText = widget.NewLabel("Scanner: —"), widget.NewLabel("")
	d.nextText.Importance = widget.LowImportance
	for _, l := range []*widget.Label{d.srcText, d.dstText, d.scanText} {
		l.Wrapping = fyne.TextWrapWord
	}
	status := widget.NewCard("Status", "", container.NewVBox(
		container.NewBorder(nil, nil, container.NewCenter(dotCell(d.srcDot)), nil, d.srcText),
		container.NewBorder(nil, nil, container.NewCenter(dotCell(d.dstDot)), nil, d.dstText),
		container.NewBorder(nil, nil, container.NewCenter(dotCell(d.scanDot)), nil, d.scanText),
		d.nextText,
	))

	d.delivered, d.failed, d.vol = widget.NewLabel("0"), widget.NewLabel("0"), widget.NewLabel("0 B")
	stats := container.NewGridWithColumns(3,
		statCard("relayed today", d.delivered),
		statCard("failed today", d.failed),
		statCard("transferred today", d.vol),
	)

	d.toggle = widget.NewButtonWithIcon("Start", theme.MediaPlayIcon(), d.onToggle)
	d.toggle.Importance = widget.HighImportance
	d.scan = widget.NewButtonWithIcon("Scan now", theme.ViewRefreshIcon(), func() {
		d.w.ctl.Engine.ScanNow()
	})
	controls := container.NewGridWithColumns(2, d.toggle, d.scan)

	d.idle = widget.NewLabel("No transfers in progress")
	d.idle.Importance = widget.LowImportance
	d.transfers = container.NewVBox(d.idle)
	active := widget.NewCard("Active transfers", "", d.transfers)

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
			msg.Refresh()
		},
	)
	left := container.NewVBox(status, stats, controls)
	right := container.NewBorder(active, nil, nil, nil,
		container.NewBorder(widget.NewLabelWithStyle("Recent activity", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), nil, nil, nil, d.feedList))
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
	return r
}

func (d *dashboard) dropRow(file string) {
	if r, ok := d.rows[file]; ok {
		d.transfers.Remove(r.box)
		delete(d.rows, file)
	}
	if len(d.rows) == 0 && len(d.transfers.Objects) == 0 {
		d.transfers.Add(d.idle)
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
	src := fmt.Sprintf("Source  %s  %s", cfg.Source.Addr(), cfg.Source.Directory)
	if s.SourceConnected {
		setDot(d.srcDot, ColorSuccess)
		d.srcText.SetText(src + "  · connected")
	} else {
		setDot(d.srcDot, ColorMuted)
		d.srcText.SetText(src + "  · idle")
	}
	switch {
	case !s.TargetEnabled:
		setDot(d.dstDot, ColorWarning)
		d.dstText.SetText("Target  none — archive-only mode")
	case s.TargetConnected:
		setDot(d.dstDot, ColorSuccess)
		d.dstText.SetText(fmt.Sprintf("Target  %s  %s  · connected", cfg.Target.Addr(), cfg.Target.Directory))
	default:
		setDot(d.dstDot, ColorMuted)
		d.dstText.SetText(fmt.Sprintf("Target  %s  %s  · idle", cfg.Target.Addr(), cfg.Target.Directory))
	}

	switch {
	case s.Scanning:
		setDot(d.scanDot, ColorAccent)
		d.scanText.SetText("Scanner  scanning…")
	case s.Running && !s.ScheduleActive:
		setDot(d.scanDot, ColorWarning)
		d.scanText.SetText(fmt.Sprintf("Scanner  waiting for active hours (%s–%s)", cfg.ActiveSchedule.Start, cfg.ActiveSchedule.End))
	case s.Running:
		setDot(d.scanDot, ColorSuccess)
		d.scanText.SetText(fmt.Sprintf("Scanner  running every %ds", cfg.PollInterval))
	default:
		setDot(d.scanDot, ColorError)
		d.scanText.SetText("Scanner  paused")
	}
	next := ""
	if !s.LastScan.IsZero() {
		next = "Last scan " + s.LastScan.Format(time.TimeOnly)
	}
	if s.Running && !s.NextScan.IsZero() {
		if in := time.Until(s.NextScan).Round(time.Second); in > 0 {
			next += fmt.Sprintf(" · next in %s", in)
		}
	}
	d.nextText.SetText(next)

	d.delivered.SetText(fmt.Sprint(s.Today.Delivered))
	d.failed.SetText(fmt.Sprint(s.Today.Failed))
	d.vol.SetText(relay.HumanBytes(s.Today.Bytes))

	if d.lastRunning == nil || *d.lastRunning != s.Running {
		running := s.Running
		d.lastRunning = &running
		if running {
			d.toggle.SetText("Pause")
			d.toggle.SetIcon(theme.MediaPauseIcon())
			d.toggle.Importance = widget.MediumImportance
		} else {
			d.toggle.SetText("Start")
			d.toggle.SetIcon(theme.MediaPlayIcon())
			d.toggle.Importance = widget.HighImportance
		}
		d.toggle.Refresh()
		if d.onStateChange != nil {
			d.onStateChange(running)
		}
	}
}
