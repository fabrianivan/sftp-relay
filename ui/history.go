package ui

import (
	"fmt"
	"strings"
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
	{"Time", 150}, {"File", 260}, {"Size", 90}, {"Duration", 90}, {"Status", 100}, {"Details", 420},
}

type historyView struct {
	w       *Window
	content fyne.CanvasObject
	table   *widget.Table
	records []history.Record
	summary *widget.Label
	filter  *widget.Select
	search  *widget.Entry
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
				if r.Status == history.Failed {
					l.SetText("✗ Failed")
					l.Importance = widget.DangerImportance
				} else {
					l.SetText("✓ Delivered")
					l.Importance = widget.SuccessImportance
				}
			case 5:
				if r.Error != "" {
					l.SetText(r.Error)
				} else {
					l.SetText(fmt.Sprintf("→ %s · archived as %s", r.Target, r.Archive))
				}
			}
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

	h.summary = widget.NewLabelWithStyle("", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	h.filter = widget.NewSelect([]string{"All", "Delivered", "Failed"}, func(string) { h.refresh() })
	h.filter.SetSelected("All")
	h.search = widget.NewEntry()
	h.search.SetPlaceHolder("Search file name or target…")
	h.search.OnChanged = func(string) { h.refresh() }

	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), h.refresh)
	open := widget.NewButtonWithIcon("Open file", theme.FolderOpenIcon(), func() {
		openPath(w.ctl.Engine.History().Path())
	})
	leftBar := container.NewHBox(widget.NewLabel("Filter:"), h.filter, widget.NewLabel("Search:"), container.NewGridWrap(fyne.NewSize(200, 36), h.search))
	rightBar := container.NewHBox(refresh, open)
	topBar := container.NewBorder(nil, nil, leftBar, rightBar, container.NewCenter(h.summary))
	h.content = container.NewPadded(container.NewBorder(topBar, nil, nil, nil, h.table))
	h.refresh()
	return h
}

func (h *historyView) refresh() {
	all := h.w.ctl.Engine.History().Records()
	var ok, failed int
	h.records = h.records[:0]
	q := ""
	if h.search != nil {
		q = strings.ToLower(strings.TrimSpace(h.search.Text))
	}
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
		if q != "" && !strings.Contains(strings.ToLower(r.File), q) && !strings.Contains(strings.ToLower(r.Target), q) {
			continue
		}
		h.records = append(h.records, r)
	}
	h.summary.SetText(fmt.Sprintf("%d Delivered  ·  %d Failed  (Showing %d of %d)", ok, failed, len(h.records), len(all)))
	h.table.Refresh()
}

func (h *historyView) refreshIfVisible() {
	if h.w.tabs != nil && h.w.tabs.Selected() != nil && h.w.tabs.Selected().Text == "History" {
		h.refresh()
	}
}
