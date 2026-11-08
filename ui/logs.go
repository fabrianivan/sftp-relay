package ui

import (
	"os/exec"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
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
}

func newLogView(w *Window) *logView {
	v := &logView{w: w, min: logging.Info, autoScroll: true}
	v.list = widget.NewList(
		func() int { return len(v.shown) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.TextStyle = fyne.TextStyle{Monospace: true}
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(i widget.ListItemID, o fyne.CanvasObject) {
			l := o.(*widget.Label)
			e := v.shown[i]
			l.SetText(e.Time.Format("15:04:05") + "  " + e.Level.String() + "  " + e.Message)
			switch e.Level {
			case logging.Error:
				l.Importance = widget.DangerImportance
			case logging.Warn:
				l.Importance = widget.WarningImportance
			case logging.Debug:
				l.Importance = widget.LowImportance
			default:
				l.Importance = widget.MediumImportance
			}
			l.Refresh()
		},
	)

	level := widget.NewSelect([]string{"Errors", "Warnings", "Info", "Debug"}, func(s string) {
		v.min = map[string]logging.Level{"Errors": logging.Error, "Warnings": logging.Warn, "Info": logging.Info, "Debug": logging.Debug}[s]
		v.rebuild()
	})
	level.SetSelected("Info")
	auto := widget.NewCheck("Auto-scroll", func(b bool) { v.autoScroll = b })
	auto.SetChecked(true)
	clearBtn := widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), func() {
		v.all = nil
		v.rebuild()
	})
	open := widget.NewButtonWithIcon("Open log folder", theme.FolderOpenIcon(), func() {
		openPath(w.ctl.Log.Dir())
	})
	bar := container.NewHBox(widget.NewLabel("Level"), level, auto, layout.NewSpacer(), clearBtn, open)
	v.content = container.NewPadded(container.NewBorder(bar, nil, nil, nil, v.list))

	v.all = w.ctl.Log.Recent()
	v.rebuild()
	go v.listen()
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
	v.shown = v.shown[:0]
	for _, e := range v.all {
		if e.Level >= v.min {
			v.shown = append(v.shown, e)
		}
	}
	v.list.Refresh()
	if v.autoScroll {
		v.list.ScrollToBottom()
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
