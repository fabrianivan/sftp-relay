package ui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fabrianivan/sftp-relay/internal/history"
	"github.com/fabrianivan/sftp-relay/internal/relay"
)

var historyCols = []struct {
	title string
	width float32
}{
	{"Time", 150}, {"File", 260}, {"Size", 90}, {"Duration", 90}, {"Status", 90}, {"Details", 420},
}

type historyView struct {
	w       *Window
	content fyne.CanvasObject
	table   *widget.Table
	records []history.Record
	summary *widget.Label
	filter  *widget.Select
}

func newHistoryView(w *Window) *historyView {
	h := &historyView{w: w}
	h.table = widget.NewTableWithHeaders(
		func() (int, int) { return len(h.records), len(historyCols) },
		func() fyne.CanvasObject {
			l := widget.NewLabel("")
			l.Truncation = fyne.TextTruncateEllipsis
			return l
		},
		func(id widget.TableCellID, o fyne.CanvasObject) {
			l := o.(*widget.Label)
			if id.Row >= len(h.records) {
				l.SetText("")
				return
			}
			r := h.records[id.Row]
			l.Importance = widget.MediumImportance
			switch id.Col {
			case 0:
				l.SetText(r.Time.Local().Format("2006-01-02 15:04:05"))
			case 1:
				l.SetText(r.File)
			case 2:
				l.SetText(relay.HumanBytes(r.Size))
			case 3:
				l.SetText(r.Duration().Round(time.Millisecond).String())
			case 4:
				l.SetText(string(r.Status))
				if r.Status == history.Failed {
					l.Importance = widget.DangerImportance
				} else {
					l.Importance = widget.SuccessImportance
				}
			case 5:
				if r.Error != "" {
					l.SetText(r.Error)
				} else {
					l.SetText(fmt.Sprintf("→ %s · archived as %s", r.Target, r.Archive))
				}
			}
			l.Refresh()
		},
	)
	h.table.CreateHeader = func() fyne.CanvasObject {
		return widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	}
	h.table.UpdateHeader = func(id widget.TableCellID, o fyne.CanvasObject) {
		l := o.(*widget.Label)
		if id.Row < 0 && id.Col >= 0 {
			l.SetText(historyCols[id.Col].title)
		} else {
			l.SetText("")
		}
	}
	h.table.ShowHeaderColumn = false
	for i, c := range historyCols {
		h.table.SetColumnWidth(i, c.width)
	}

	h.summary = widget.NewLabel("")
	h.filter = widget.NewSelect([]string{"All", "Delivered", "Failed"}, func(string) { h.refresh() })
	h.filter.SetSelected("All")
	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), h.refresh)
	open := widget.NewButtonWithIcon("Open history file", theme.FolderOpenIcon(), func() {
		openPath(w.ctl.Engine.History().Path())
	})
	bar := container.NewBorder(nil, nil, container.NewHBox(widget.NewLabel("Show"), h.filter), container.NewHBox(refresh, open), h.summary)
	h.content = container.NewPadded(container.NewBorder(bar, nil, nil, nil, h.table))
	h.refresh()
	return h
}

func (h *historyView) refresh() {
	all := h.w.ctl.Engine.History().Records()
	var ok, failed int
	h.records = h.records[:0]
	for _, r := range all {
		if r.Status == history.Failed {
			failed++
		} else {
			ok++
		}
		switch h.filter.Selected {
		case "Delivered":
			if r.Status == history.Failed {
				continue
			}
		case "Failed":
			if r.Status != history.Failed {
				continue
			}
		}
		h.records = append(h.records, r)
	}
	h.summary.SetText(fmt.Sprintf("%d processed · %d failed (last %d entries kept)", ok, failed, len(all)))
	h.table.Refresh()
}

func (h *historyView) refreshIfVisible() {
	if h.w.tabs != nil && h.w.tabs.Selected() != nil && h.w.tabs.Selected().Text == "History" {
		h.refresh()
	}
}
