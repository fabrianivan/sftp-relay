package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/relay"
)

const maxActivity = 60

type activity struct {
	at   time.Time
	text string
	tone tone
}

// feedTone is the colour of a stream: sky for the source server, violet for
// the local folder.
func feedTone(feed string) tone {
	if feed == relay.FeedLocal {
		return toneLocal
	}
	return toneInfo
}

// transferRow shows one file that is travelling right now.
type transferRow struct {
	feed  string
	box   *fyne.Container
	head  *fyne.Container
	stat  *canvas.Text
	meter *meter
}

// feedRow is a line of the activity list.
type feedRow struct {
	widget.BaseWidget
	when *widget.Label
	dot  *canvas.Circle
	msg  *widget.Label
	box  *fyne.Container
}

func newFeedRow() *feedRow {
	r := &feedRow{when: widget.NewLabel("00:00:00"), msg: widget.NewLabel("")}
	r.when.TextStyle = styleMono
	r.when.Importance = widget.LowImportance
	r.msg.Truncation = fyne.TextTruncateEllipsis
	var dot fyne.CanvasObject
	r.dot, dot = newDot(ColorFaint)
	r.box = container.NewBorder(nil, nil, hstack(4, r.when, dot), nil, r.msg)
	r.ExtendBaseWidget(r)
	return r
}

func (r *feedRow) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(r.box) }

func (r *feedRow) set(a activity) {
	r.when.SetText(a.at.Format(time.TimeOnly))
	if a.tone == toneNeutral {
		setDot(r.dot, ColorFaint)
	} else {
		setDot(r.dot, a.tone.color())
	}
	if a.tone == toneDanger {
		r.msg.Importance = widget.DangerImportance
	} else {
		r.msg.Importance = widget.MediumImportance
	}
	r.msg.Text = a.text
	r.msg.Refresh()
}

// figure is one of today's numbers: a caption over a large value.
type figure struct {
	value *canvas.Text
	box   *fyne.Container
}

func newFigure(caption string) *figure {
	f := &figure{value: newText("0", 28, ColorForeground, styleBold)}
	f.box = vstack(4, overline(caption), f.value)
	return f
}

type dashboard struct {
	w       *Window
	content fyne.CanvasObject

	stateDot *canvas.Circle
	headline *canvas.Text
	subline  *widget.Label
	toggle   *widget.Button
	scan     *widget.Button

	flow *flowMap

	relayed, failed, volume *figure
	chart                   *hourChart
	chartNote               *canvas.Text
	chartAt                 time.Time

	active      *fyne.Container
	activeCount *pill
	transfers   *fyne.Container
	idle        *fyne.Container
	rows        map[string]*transferRow

	feed      []activity
	feedList  *widget.List
	feedEmpty *fyne.Container

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

func (d *dashboard) build() {
	// Headline: what the relay is doing, in words.
	var dot fyne.CanvasObject
	d.stateDot, dot = newDotSized(ColorMuted, 11)
	d.headline = newText("Relay", 24, ColorForeground, styleBold)
	d.subline = mutedLabel("")
	d.toggle = widget.NewButtonWithIcon("Start relay", theme.MediaPlayIcon(), d.onToggle)
	d.toggle.Importance = widget.HighImportance
	d.scan = widget.NewButtonWithIcon("Scan now", theme.ViewRefreshIcon(), func() {
		d.w.ctl.Engine.ScanNow()
	})
	header := container.NewBorder(nil, nil, nil, vcenter(hstack(8, d.scan, d.toggle)),
		vstack(6, hstack(12, pad(0, 0, 2, 0, dot), d.headline), tight(d.subline)))

	// The relay map.
	d.flow = newFlowMap()
	flow := panelInset(20, d.flow)

	// Today's figures next to the last 24 hours.
	d.relayed = newFigure("RELAYED TODAY")
	d.failed = newFigure("FAILED TODAY")
	d.volume = newFigure("DATA SENT")
	d.chart = newHourChart()
	d.chartNote = newText("", 11, ColorMuted, fyne.TextStyle{})
	figures := hstack(34, d.relayed.box, d.failed.box, d.volume.box)
	divider := canvas.NewRectangle(ColorSeparator)
	divider.SetMinSize(fyne.NewSize(1, 1))
	trend := container.NewBorder(
		container.NewBorder(nil, nil, overline("LAST 24 HOURS"), d.chartNote), nil, nil, nil,
		pad(8, 0, 0, 0, d.chart))
	today := panelInset(18, container.NewBorder(nil, nil,
		hstack(26, vcenter(figures), divider), nil, pad(0, 0, 26, 0, trend)))

	// Transfers in progress.
	d.activeCount = newPill("0", toneInfo)
	d.activeCount.Hide()
	d.transfers = vstack(14)
	d.idle = emptyState("No transfers in progress", "Files show up here while they travel to the target.")
	d.active = titledPanel("Active transfers", vcenter(d.activeCount),
		container.NewStack(d.idle, container.NewVScroll(d.transfers)))

	// Activity.
	d.feedList = widget.NewList(
		func() int { return len(d.feed) },
		func() fyne.CanvasObject { return newFeedRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(d.feed) {
				return
			}
			o.(*feedRow).set(d.feed[len(d.feed)-1-i])
		},
	)
	d.feedEmpty = emptyState("No activity yet", "Scans and transfers are listed here as they happen.")
	activityPanel := titledPanel("Activity", nil, container.NewStack(d.feedList, d.feedEmpty))

	body := container.NewBorder(pad(0, gap, 0, 0, vstack(gap, flow, today)), nil, nil, nil,
		columns(gap, []float32{5, 6}, d.active, activityPanel))

	d.content = page(header, body)
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
		d.w.showPage(pageSettings)
		return
	}
	eng.Start()
}

func (d *dashboard) addActivity(text string, t tone) {
	d.feed = append(d.feed, activity{at: time.Now(), text: text, tone: t})
	if len(d.feed) > maxActivity {
		d.feed = d.feed[len(d.feed)-maxActivity:]
	}
	d.feedEmpty.Hide()
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
		d.row(ev.Feed, ev.File)
		d.addActivity("Started "+ev.File+" ("+relay.HumanBytes(ev.Size)+")", feedTone(ev.Feed))
	case relay.TransferProgress:
		r := d.row(ev.Feed, ev.File)
		var done float64
		if ev.Size > 0 {
			done = float64(ev.Transferred) / float64(ev.Size)
		}
		r.meter.SetValue(done)
		setText(r.stat, fmt.Sprintf("%.0f%%  ·  %s/s", done*100, relay.HumanBytes(int64(ev.Speed))), ColorMuted)
		r.head.Refresh()
	case relay.TransferDone:
		d.dropRow(ev.Feed, ev.File)
		what := ev.Message
		if ev.Feed == relay.FeedLocal && what == string(history.Delivered) {
			what = "backed up"
		}
		d.addActivity(ev.File+" "+what, toneSuccess)
		d.refreshChart()
		d.w.history.refreshIfVisible()
	case relay.TransferFailed:
		d.dropRow(ev.Feed, ev.File)
		t := toneWarning // e.g. still being written, will retry
		if ev.Err {
			t = toneDanger
		}
		d.addActivity(ev.File+": "+ev.Message, t)
		d.refreshChart()
		d.w.history.refreshIfVisible()
	case relay.ScanFinished, relay.StateChanged:
		if !ev.Quiet {
			t := toneNeutral
			if ev.Err {
				t = toneDanger
			}
			d.addActivity(ev.Message, t)
		}
		d.render(d.w.ctl.Engine.Status())
	}
}

func (d *dashboard) row(feed, file string) *transferRow {
	key := feed + "|" + file
	if r, ok := d.rows[key]; ok {
		return r
	}
	col := feedTone(feed).color()
	name := widget.NewLabel(file)
	name.Truncation = fyne.TextTruncateEllipsis
	_, dot := newDot(col)
	r := &transferRow{feed: feed, stat: newText("starting…", 12, ColorMuted, fyne.TextStyle{}), meter: newMeter(col)}
	r.head = container.NewBorder(nil, nil, pad(0, 0, 0, 10, dot), vcenter(r.stat), tight(name))
	r.box = vstack(8, r.head, r.meter)
	d.rows[key] = r
	d.transfers.Add(r.box)
	d.syncTransfers()
	return r
}

func (d *dashboard) dropRow(feed, file string) {
	key := feed + "|" + file
	if r, ok := d.rows[key]; ok {
		d.transfers.Remove(r.box)
		delete(d.rows, key)
		d.syncTransfers()
	}
}

// syncTransfers keeps the count badge, the empty state and the map's moving
// streams in step with the rows.
func (d *dashboard) syncTransfers() {
	if n := len(d.rows); n == 0 {
		d.activeCount.Hide()
		d.idle.Show()
	} else {
		d.activeCount.Set(fmt.Sprint(n), toneInfo)
		d.activeCount.Show()
		d.idle.Hide()
	}
	d.active.Refresh()
	d.syncStreams(d.w.ctl.Engine.Status())
}

// syncStreams tells the map which streams exist and which carry files.
func (d *dashboard) syncStreams(s relay.Status) {
	var fromSource, fromLocal bool
	for _, r := range d.rows {
		if r.feed == relay.FeedLocal {
			fromLocal = true
		} else {
			fromSource = true
		}
	}
	d.flow.setStream(streamSource, s.SourceEnabled, fromSource)
	d.flow.setStream(streamLocal, s.LocalEnabled, fromLocal)
	d.flow.setStream(streamTarget, s.TargetEnabled && (s.SourceEnabled || s.LocalEnabled), fromSource || fromLocal)
}

// refreshChart recounts the last 24 hours from the transfer journal.
func (d *dashboard) refreshChart() {
	now := time.Now()
	d.chartAt = now
	var ok, bad [24]int
	for _, r := range d.w.ctl.Engine.History().Records() {
		age := now.Sub(r.Time)
		if age < 0 || age >= 24*time.Hour {
			continue
		}
		i := 23 - int(age/time.Hour)
		if r.Status == history.Failed {
			bad[i]++
		} else {
			ok[i]++
		}
	}
	d.chart.set(ok, bad)
	note := "no transfers yet"
	if peak := d.chart.peak(); peak > 0 {
		note = fmt.Sprintf("busiest hour: %d file(s)", peak)
	}
	setText(d.chartNote, note, ColorMuted)
}

func (d *dashboard) poll() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		s := d.w.ctl.Engine.Status()
		fyne.Do(func() { d.render(s) })
	}
}

func (d *dashboard) render(s relay.Status) {
	cfg := d.w.ctl.Config()

	// What the engine is doing: the headline, the dial and the rail agree.
	poll := cfg.Poll()
	wait := time.Duration(0)
	if s.Running && !s.NextScan.IsZero() {
		wait = time.Until(s.NextScan).Round(time.Second)
	}
	var (
		headline, sub, short string
		t                    tone
		dial, caption        string
		frac                 float32
	)
	last := "no scan yet"
	if !s.LastScan.IsZero() {
		last = "last scan " + s.LastScan.Format(time.TimeOnly)
	}
	switch {
	case s.Scanning:
		headline, short, t = "Scanning for new files", "Scanning", toneInfo
		sub = "Looking through the watched folders…"
		dial, caption, frac = "Scan", "in progress", 1
	case s.Running && !s.ScheduleActive:
		headline, short, t = "Waiting for active hours", "Off hours", toneWarning
		sub = fmt.Sprintf("Scheduled scans run between %s and %s  ·  %s", cfg.ActiveSchedule.Start, cfg.ActiveSchedule.End, last)
		dial, caption = "Idle", "off hours"
	case s.Running:
		headline, short, t = "Relay is running", "Running", toneSuccess
		sub = fmt.Sprintf("Scans every %s  ·  %s", poll, last)
		dial, caption = "Now", "next scan"
		if wait > 0 {
			dial = wait.String()
			if poll > 0 {
				frac = 1 - float32(wait)/float32(poll)
			}
		} else {
			frac = 1
		}
	default:
		headline, short, t = "Relay is paused", "Paused", toneWarning
		sub = "Nothing is picked up until you start the relay again  ·  " + last
		dial, caption = "Paused", "not scanning"
	}
	setDot(d.stateDot, t.color())
	setText(d.headline, headline, ColorForeground)
	if d.subline.Text != sub {
		d.subline.SetText(sub)
	}
	d.flow.setHub(dial, caption, frac, t.color())
	d.w.setEngineState(short, t)

	// Stations.
	switch {
	case !s.SourceEnabled:
		d.flow.source.set(false, "No source server set up", "Off", toneNeutral)
	case s.SourceError != "":
		d.flow.source.set(true, s.SourceError, "Unreachable", toneDanger)
	case s.SourceConnected:
		d.flow.source.set(true, cfg.Source.Addr()+"  ›  "+cfg.Source.Directory, "Connected", toneSuccess)
	default:
		d.flow.source.set(true, cfg.Source.Addr()+"  ›  "+cfg.Source.Directory, "Idle", toneNeutral)
	}
	switch {
	case !s.LocalEnabled:
		d.flow.local.set(false, "Turn on in Settings  ›  Local backup", "Off", toneNeutral)
	case s.LocalError != "":
		problem := strings.ToUpper(s.LocalError[:1]) + s.LocalError[1:]
		d.flow.local.set(true, problem+"  ·  "+shortPath(cfg.LocalBackup.Directory, 20), "Unavailable", toneDanger)
	case s.Running:
		d.flow.local.set(true, shortPath(cfg.LocalBackup.Directory, 30), "Watching", toneSuccess)
	default:
		d.flow.local.set(true, shortPath(cfg.LocalBackup.Directory, 30), "Idle", toneNeutral)
	}
	switch {
	case !s.TargetEnabled:
		d.flow.target.set(false, "Archive-only: no target server", "Off", toneNeutral)
	case s.TargetConnected:
		d.flow.target.set(true, cfg.Target.Addr()+"  ›  "+cfg.Target.Directory, "Connected", toneSuccess)
	default:
		d.flow.target.set(true, cfg.Target.Addr()+"  ›  "+cfg.Target.Directory, "Idle", toneNeutral)
	}
	d.syncStreams(s)

	// Figures.
	setText(d.relayed.value, fmt.Sprint(s.Today.Delivered), ColorForeground)
	var failedColor color.Color = ColorForeground
	if s.Today.Failed > 0 {
		failedColor = ColorError
	}
	setText(d.failed.value, fmt.Sprint(s.Today.Failed), failedColor)
	setText(d.volume.value, relay.HumanBytes(s.Today.Bytes), ColorForeground)
	if time.Since(d.chartAt) > time.Minute {
		d.refreshChart()
	}

	if d.lastRunning == nil || *d.lastRunning != s.Running {
		running := s.Running
		d.lastRunning = &running
		if running {
			d.toggle.SetText("Pause relay")
			d.toggle.SetIcon(theme.MediaPauseIcon())
			d.toggle.Importance = widget.MediumImportance
		} else {
			d.toggle.SetText("Start relay")
			d.toggle.SetIcon(theme.MediaPlayIcon())
			d.toggle.Importance = widget.HighImportance
		}
		d.toggle.Refresh()
		if d.onStateChange != nil {
			d.onStateChange(running)
		}
	}
}
