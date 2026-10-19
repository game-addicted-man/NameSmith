//go:build windows

package ui

import (
	"sort"
	"strings"

	"github.com/tailscale/walk"

	"github.com/game-addicted-man/NameSmith/internal/engine"
)

// Table columns.
const (
	colName = iota
	colNewName
	colStatus
	colModified
	colSize
	colFolder
)

// fileModel feeds the TableView. Its order is the numbering order.
type fileModel struct {
	walk.TableModelBase
	walk.SorterBase

	items []*engine.Item
	prev  []engine.Preview

	// sortCol is the column the list is currently sorted by, or -1 once the
	// user moved rows by hand.
	sortCol   int
	sortOrder walk.SortOrder

	onChange func() // called after checks change or rows are reordered
}

func newFileModel() *fileModel {
	return &fileModel{sortCol: -1}
}

func (m *fileModel) RowCount() int { return len(m.items) }

func (m *fileModel) Value(row, col int) interface{} {
	if row < 0 || row >= len(m.items) {
		return ""
	}
	it := m.items[row]
	switch col {
	case colName:
		return it.Name
	case colNewName:
		if row < len(m.prev) {
			p := m.prev[row]
			if p.Status == engine.StatusSkipped {
				return ""
			}
			return p.NewName
		}
		return it.Name
	case colStatus:
		if row < len(m.prev) {
			p := m.prev[row]
			if p.Note != "" {
				return p.Status.Label() + " · " + p.Note
			}
			return p.Status.Label()
		}
		return ""
	case colFolder:
		return it.Dir
	case colModified:
		return formatTime(it.Modified)
	case colSize:
		return formatSize(it.Size)
	}
	return ""
}

// Image shows the Explorer icon of each file in the first column.
func (m *fileModel) Image(row int) interface{} {
	if row < 0 || row >= len(m.items) {
		return nil
	}
	return m.items[row].Path()
}

func (m *fileModel) Checked(row int) bool {
	return row >= 0 && row < len(m.items) && m.items[row].Checked
}

func (m *fileModel) SetChecked(row int, checked bool) error {
	if row >= 0 && row < len(m.items) && m.items[row].Checked != checked {
		m.items[row].Checked = checked
		if m.onChange != nil {
			m.onChange()
		}
	}
	return nil
}

// ColumnSortable: the new name and status change with the rules, so sorting
// by them would make numbers jump around.
func (m *fileModel) ColumnSortable(col int) bool {
	return col == colName || col == colFolder || col == colModified || col == colSize
}

// Sort is called by the TableView after a header click, and again (with the
// same column) whenever rows change; it only reorders when something new
// is asked for.
func (m *fileModel) Sort(col int, order walk.SortOrder) error {
	if col >= 0 && m.ColumnSortable(col) && (col != m.sortCol || order != m.sortOrder) {
		m.sortCol, m.sortOrder = col, order
		m.sortItems()
		if m.onChange != nil {
			m.onChange()
		}
	}
	if col < 0 || !m.ColumnSortable(col) {
		col = m.sortCol
		order = m.sortOrder
	}
	return m.SorterBase.Sort(col, order)
}

// SortedColumn reports our own idea of the sort column (-1 = manual order).
func (m *fileModel) SortedColumn() int { return m.sortCol }

// SortOrder reports the current order.
func (m *fileModel) SortOrder() walk.SortOrder { return m.sortOrder }

func (m *fileModel) sortItems() {
	if m.sortCol < 0 {
		return
	}
	less := func(a, b *engine.Item) int {
		switch m.sortCol {
		case colFolder:
			if c := engine.NaturalCompare(a.Dir, b.Dir); c != 0 {
				return c
			}
			return engine.NaturalCompare(a.Name, b.Name)
		case colModified:
			switch {
			case a.Modified.Before(b.Modified):
				return -1
			case a.Modified.After(b.Modified):
				return 1
			}
			return engine.NaturalCompare(a.Name, b.Name)
		case colSize:
			switch {
			case a.Size < b.Size:
				return -1
			case a.Size > b.Size:
				return 1
			}
			return engine.NaturalCompare(a.Name, b.Name)
		default:
			if c := engine.NaturalCompare(a.Name, b.Name); c != 0 {
				return c
			}
			return engine.NaturalCompare(a.Dir, b.Dir)
		}
	}
	desc := m.sortOrder == walk.SortDescending
	sort.SliceStable(m.items, func(i, j int) bool {
		c := less(m.items[i], m.items[j])
		if desc {
			return c > 0
		}
		return c < 0
	})
}

// sortBy sorts by a column chosen from the menu and keeps it as the sort.
func (m *fileModel) sortBy(col int, order walk.SortOrder) {
	m.sortCol = -2 // force a re-sort even if the same column is asked again
	_ = m.Sort(col, order)
}

// moveRows moves the selected rows up (delta -1) or down (+1), or to the
// top (-2) / bottom (+2). It returns the new indexes of the moved rows.
func (m *fileModel) moveRows(sel []int, delta int) []int {
	if len(sel) == 0 {
		return sel
	}
	sort.Ints(sel)
	selected := make(map[*engine.Item]bool, len(sel))
	for _, i := range sel {
		if i >= 0 && i < len(m.items) {
			selected[m.items[i]] = true
		}
	}
	items := m.items
	switch delta {
	case -2, 2:
		var picked, rest []*engine.Item
		for _, it := range items {
			if selected[it] {
				picked = append(picked, it)
			} else {
				rest = append(rest, it)
			}
		}
		if delta < 0 {
			items = append(picked, rest...)
		} else {
			items = append(rest, picked...)
		}
	case -1:
		for i := 1; i < len(items); i++ {
			if selected[items[i]] && !selected[items[i-1]] {
				items[i], items[i-1] = items[i-1], items[i]
			}
		}
	case 1:
		for i := len(items) - 2; i >= 0; i-- {
			if selected[items[i]] && !selected[items[i+1]] {
				items[i], items[i+1] = items[i+1], items[i]
			}
		}
	}
	m.items = items
	m.sortCol = -1 // manual order now
	var out []int
	for i, it := range m.items {
		if selected[it] {
			out = append(out, i)
		}
	}
	return out
}

// remove deletes the rows at the given indexes from the list (not from disk).
func (m *fileModel) remove(sel []int) {
	drop := make(map[int]bool, len(sel))
	for _, i := range sel {
		drop[i] = true
	}
	kept := m.items[:0]
	for i, it := range m.items {
		if !drop[i] {
			kept = append(kept, it)
		}
	}
	for i := len(kept); i < len(m.items); i++ {
		m.items[i] = nil
	}
	m.items = kept
	m.prev = nil
}

func (m *fileModel) sortLabel() string {
	if m.sortCol < 0 || len(m.items) == 0 {
		if len(m.items) == 0 {
			return ""
		}
		return "직접 정한 순서"
	}
	name := map[int]string{colName: "이름", colFolder: "폴더", colModified: "수정한 날짜", colSize: "크기"}[m.sortCol]
	arrow := "↑"
	if m.sortOrder == walk.SortDescending {
		arrow = "↓"
	}
	return name + " " + arrow + " 순서"
}

// statusText of a whole preview summary, for the bottom bar.
func summaryText(s engine.Summary, anyRule bool) string {
	if s.Total == 0 {
		return "파일을 추가하면 바뀔 이름을 바로 미리 보여 드려요."
	}
	if !anyRule {
		return countText(s.Total) + " · 오른쪽에서 바꿀 방법을 켜세요"
	}
	parts := []string{countText(s.Total) + " 중 " + itoa(s.Changed) + "개 바뀜"}
	if s.Conflict > 0 {
		parts = append(parts, "겹침 "+itoa(s.Conflict))
	}
	if s.Invalid > 0 {
		parts = append(parts, "오류 "+itoa(s.Invalid))
	}
	if s.Skipped > 0 {
		parts = append(parts, "제외 "+itoa(s.Skipped))
	}
	if s.Unchanged > 0 {
		parts = append(parts, "그대로 "+itoa(s.Unchanged))
	}
	return strings.Join(parts, " · ")
}

func countText(n int) string { return "파일 " + itoa(n) + "개" }
