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

// historyCols lists the table columns; the last one takes the leftover width.
var historyCols = []struct {
	title string
	width float32
}{
	{"Time", 190}, {"File", 210}, {"From", 78}, {"Size", 86}, {"Duration", 86}, {"Status", 96}, {"Details", 220},
}

type historyView struct {
	w       *Window
	content fyne.CanvasObject
	table   *widget.Table
	grid    *fyne.Container // the table, hidden while there is nothing to list
	records []history.Record
	summary *widget.Label
	filter  *widget.Select
	search  *widget.Entry
	empty   *fyne.Container
}

// tableFill stretches the last column of a table across the free width.
type tableFill struct {
	table *widget.Table
	fixed float32 // total width of the other columns
	min   float32
	col   int
	last  float32
}

func (l *tableFill) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	// Table columns are separated by one theme padding each.
	w := fyne.Max(l.min, size.Width-l.fixed-float32(l.col)*theme.Padding()-2)
	if w != l.last {
		l.last = w
		l.table.SetColumnWidth(l.col, w)
	}
	for _, o := range objs {
		o.Move(fyne.NewPos(0, 0))
		o.Resize(size)
	}
}

func (l *tableFill) MinSize([]fyne.CanvasObject) fyne.Size { return fyne.NewSize(320, 160) }

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
			l.TextStyle = fyne.TextStyle{}
			switch id.Col {
			case 0:
				l.Importance = widget.LowImportance
				l.TextStyle = styleMono
				l.Text = r.Time.Local().Format("2006-01-02 15:04:05")
			case 1:
				l.Text = r.File
			case 2:
				l.Importance = widget.LowImportance
				if r.Source == relay.FeedLocal {
					l.Text = "Local"
				} else {
					l.Text = "Server"
				}
			case 3:
				l.Text = relay.HumanBytes(r.Size)
			case 4:
				l.Text = r.Duration().Round(time.Millisecond).String()
			case 5:
				l.TextStyle = styleBold
				if r.Status == history.Failed {
					l.Importance = widget.DangerImportance
					l.Text = "Failed"
				} else {
					l.Importance = widget.SuccessImportance
					l.Text = "Delivered"
				}
			case 6:
				l.Importance = widget.LowImportance
				switch {
				case r.Error != "":
					l.Importance = widget.DangerImportance
					l.Text = r.Error
				case r.Archive == "" && r.Target != "":
					l.Text = fmt.Sprintf("→ %s  ·  original kept", r.Target)
				case r.Target == "":
					l.Text = fmt.Sprintf("archived as %s", r.Archive)
				default:
					l.Text = fmt.Sprintf("→ %s  ·  archived as %s", r.Target, r.Archive)
				}
			}
			l.Refresh()
		},
	)
	h.table.CreateHeader = func() fyne.CanvasObject {
		l := widget.NewLabelWithStyle("", fyne.TextAlignLeading, styleBold)
		l.Importance = widget.LowImportance
		return l
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
	last := len(historyCols) - 1
	fill := &tableFill{table: h.table, col: last, min: historyCols[last].width}
	for i, c := range historyCols {
		h.table.SetColumnWidth(i, c.width)
		if i < last {
			fill.fixed += c.width
		}
	}

	h.summary = mutedLabel("")
	h.filter = widget.NewSelect([]string{"All", "Delivered", "Failed"}, func(string) { h.refresh() })
	h.filter.SetSelected("All")
	h.search = searchEntry("Search by file name or target…", func(string) { h.refresh() })
	h.empty = emptyState("No transfers to show", "Relayed and backed-up files are recorded here.")
	h.grid = container.New(fill, h.table)

	refresh := widget.NewButtonWithIcon("Refresh", theme.ViewRefreshIcon(), h.refresh)
	open := widget.NewButtonWithIcon("Open history file", theme.FolderOpenIcon(), func() {
		openPath(w.ctl.Engine.History().Path())
	})

	toolbar := container.NewBorder(nil, nil, nil, field("Status", h.filter, 150), h.search)
	body := container.NewBorder(pad(0, gap, 0, 0, toolbar), nil, nil, nil,
		panelInset(6, container.NewStack(h.grid, h.empty)))

	h.content = page(pageHeader("History", h.summary, refresh, open), body)
	h.refresh()
	return h
}

func (h *historyView) refresh() {
	if h.table == nil || h.filter == nil || h.summary == nil {
		return
	}
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
	h.summary.SetText(fmt.Sprintf("%d delivered  ·  %d failed  ·  showing %d of %d transfers", ok, failed, len(h.records), len(all)))
	if h.empty != nil && h.grid != nil {
		if len(h.records) == 0 {
			h.grid.Hide()
			h.empty.Show()
		} else {
			h.empty.Hide()
			h.grid.Show()
		}
	}
	h.table.Refresh()
}

func (h *historyView) refreshIfVisible() {
	if h.w.current == pageHistory {
		h.refresh()
	}
}
