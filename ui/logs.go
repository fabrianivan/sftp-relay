package ui

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/logging"
)

const maxLogLines = 1000

type logView struct {
	w          *Window
	content    fyne.CanvasObject
	list       *widget.List
	all        []logging.Entry
	shown      []logging.Entry
	min        logging.Level
	autoScroll bool
	search     *widget.Entry
	empty      *fyne.Container
}

// logRow is one log line: time, level, message.
type logRow struct {
	widget.BaseWidget
	when  *canvas.Text
	level *canvas.Text
	msg   *widget.Label
	box   *fyne.Container
}

func newLogRow() *logRow {
	r := &logRow{
		when:  newText("00:00:00", 13, ColorMuted, styleMono),
		level: newText("INFO", 12, ColorMuted, fyne.TextStyle{Monospace: true, Bold: true}),
		msg:   widget.NewLabel(""),
	}
	r.msg.TextStyle = styleMono
	r.msg.Truncation = fyne.TextTruncateEllipsis
	left := hstack(14, pad(0, 0, 8, 0, vcenter(r.when)),
		container.NewGridWrap(fyne.NewSize(44, r.when.MinSize().Height), r.level))
	r.box = container.NewBorder(nil, nil, vcenter(left), nil, pad(-4, -4, 0, 0, r.msg))
	r.ExtendBaseWidget(r)
	return r
}

func (r *logRow) CreateRenderer() fyne.WidgetRenderer { return widget.NewSimpleRenderer(r.box) }

func (r *logRow) set(e logging.Entry) {
	setText(r.when, e.Time.Format("15:04:05"), ColorMuted)
	r.msg.Importance = widget.MediumImportance
	switch e.Level {
	case logging.Error:
		setText(r.level, "ERROR", ColorError)
		r.msg.Importance = widget.DangerImportance
	case logging.Warn:
		setText(r.level, "WARN", ColorWarning)
		r.msg.Importance = widget.WarningImportance
	case logging.Debug:
		setText(r.level, "DEBUG", ColorFaint)
		r.msg.Importance = widget.LowImportance
	default:
		setText(r.level, "INFO", ColorAccent)
	}
	r.msg.Text = e.Message
	r.msg.Refresh()
}

func newLogView(w *Window) *logView {
	v := &logView{w: w, min: logging.Info, autoScroll: true}
	v.list = widget.NewList(
		func() int { return len(v.shown) },
		func() fyne.CanvasObject { return newLogRow() },
		func(i widget.ListItemID, o fyne.CanvasObject) {
			if i < 0 || i >= len(v.shown) {
				return
			}
			o.(*logRow).set(v.shown[i])
		},
	)
	v.list.HideSeparators = true
	v.empty = emptyState("No log lines to show", "Nothing matches the current level and search.")

	level := widget.NewSelect([]string{"Errors", "Warnings", "Info", "Debug"}, func(s string) {
		v.min = map[string]logging.Level{"Errors": logging.Error, "Warnings": logging.Warn, "Info": logging.Info, "Debug": logging.Debug}[s]
		v.rebuild()
	})
	level.SetSelected("Info")

	auto := widget.NewCheck("Auto-scroll", func(b bool) { v.autoScroll = b })
	auto.SetChecked(true)

	v.search = searchEntry("Search in logs…", func(string) { v.rebuild() })

	clearBtn := widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), func() {
		v.all = nil
		v.rebuild()
	})
	open := widget.NewButtonWithIcon("Open log folder", theme.FolderOpenIcon(), func() {
		openPath(w.ctl.Log.Dir())
	})

	toolbar := container.NewBorder(nil, nil, nil, hstack(16, field("Level", level, 150), auto), v.search)
	body := container.NewBorder(pad(0, gap, 0, 0, toolbar), nil, nil, nil,
		panelInset(6, container.NewStack(v.list, v.empty)))

	v.content = page(pageHeader("Logs",
		mutedLabel(fmt.Sprintf("Live application log  ·  the most recent %d lines are kept in view.", maxLogLines)),
		clearBtn, open), body)

	v.all = w.ctl.Log.Recent()
	v.rebuild()
	fyne.Do(func() {
		go v.listen()
	})
	return v
}

func (v *logView) listen() {
	entries, _ := v.w.ctl.Log.Subscribe()
	for e := range entries {
		e := e
		fyne.Do(func() {
			v.all = append(v.all, e)
			if len(v.all) > maxLogLines {
				v.all = v.all[len(v.all)-maxLogLines:]
			}
			if e.Level >= v.min {
				v.shown = append(v.shown, e)
				if len(v.shown) > maxLogLines {
					v.shown = v.shown[len(v.shown)-maxLogLines:]
				}
				v.syncEmpty()
				v.list.Refresh()
				if v.autoScroll {
					v.list.ScrollToBottom()
				}
			}
		})
	}
}

func (v *logView) rebuild() {
	if v.list == nil {
		return
	}
	q := ""
	if v.search != nil {
		q = strings.ToLower(strings.TrimSpace(v.search.Text))
	}
	v.shown = v.shown[:0]
	for _, e := range v.all {
		if e.Level >= v.min {
			if q != "" && !strings.Contains(strings.ToLower(e.Message), q) {
				continue
			}
			v.shown = append(v.shown, e)
		}
	}
	v.syncEmpty()
	v.list.Refresh()
	if v.autoScroll {
		v.list.ScrollToBottom()
	}
}

// syncEmpty shows the placeholder when no line passes the filters.
func (v *logView) syncEmpty() {
	if v.empty == nil {
		return
	}
	if len(v.shown) == 0 {
		v.empty.Show()
	} else {
		v.empty.Hide()
	}
}

// openPath opens a file or folder with the OS default handler.
// Arguments are passed directly (no shell), so paths cannot inject commands.
func openPath(p string) {
	if p == "" {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", p)
	case "windows":
		cmd = exec.Command("explorer", p)
	default:
		cmd = exec.Command("xdg-open", p)
	}
	_ = cmd.Start()
}
