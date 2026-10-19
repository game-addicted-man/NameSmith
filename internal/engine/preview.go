package engine

import "time"

// Preview is what will happen to one item.
type Preview struct {
	NewName string
	Status  Status
	Note    string // extra detail shown next to the status
}

// Summary counts previews by status.
type Summary struct {
	Total, Changed, Unchanged, Conflict, Invalid, Skipped int
}

// DirLister returns the names (any case) of files and folders in dir.
type DirLister func(dir string) ([]string, error)

// BuildPreview applies rules to every item in list order and checks the
// results against each other and against files already in each folder.
func BuildPreview(items []*Item, rules Rules, now time.Time, list DirLister) ([]Preview, Summary, error) {
	c, err := rules.Compile(now)
	if err != nil {
		return nil, Summary{}, err
	}
	ps := c.Preview(items, list)
	return ps, summarize(ps), nil
}

// Preview runs compiled rules over the items. See BuildPreview.
func (c *Compiled) Preview(items []*Item, list DirLister) []Preview {
	r := &c.r
	out := make([]Preview, len(items))

	counter := r.Number.Start
	perDir := map[string]int{}

	for i, it := range items {
		p := &out[i]
		if !it.Checked {
			p.NewName, p.Status = it.Name, StatusSkipped
			continue
		}
		num := 0
		if r.Number.On {
			if r.Number.PerFolder {
				k := DirKey(it.Dir)
				n, ok := perDir[k]
				if !ok {
					n = r.Number.Start
				}
				num = n
				perDir[k] = n + r.Number.Step
			} else {
				num = counter
				counter += r.Number.Step
			}
		}
		p.NewName = c.NewName(it, num)
		if p.NewName == it.Name {
			p.Status = StatusUnchanged
			continue
		}
		if why := ValidateName(p.NewName); why != "" {
			p.Status, p.Note = StatusInvalid, why
			continue
		}
		p.Status = StatusChanged
		if r.Date.On {
			if _, fellBack := c.DateFor(it); fellBack {
				p.Note = "찍은 날짜 없음 → 수정한 날짜"
			}
		}
	}

	markConflicts(items, out, list)
	return out
}

// markConflicts turns StatusChanged into StatusConflict where the new name
// would clash. It repeats until stable because an item that cannot move keeps
// its old name occupied, which may block another item.
func markConflicts(items []*Item, out []Preview, list DirLister) {
	// existing names per folder (only folders with changes)
	existing := map[string]map[string]bool{}
	for i, it := range items {
		if out[i].Status != StatusChanged {
			continue
		}
		dk := DirKey(it.Dir)
		if _, ok := existing[dk]; ok {
			continue
		}
		set := map[string]bool{}
		if list != nil {
			if names, err := list(it.Dir); err == nil {
				for _, n := range names {
					set[foldKey(n)] = true
				}
			}
		}
		existing[dk] = set
	}

	for {
		changed := false

		// who ends up holding which name
		holders := make(map[string][]int, len(items))
		listed := map[string]bool{} // current names of every listed item
		for i, it := range items {
			listed[pathKey(it.Dir, it.Name)] = true
			final := it.Name
			if out[i].Status == StatusChanged {
				final = out[i].NewName
			}
			k := pathKey(it.Dir, final)
			holders[k] = append(holders[k], i)
		}

		for i, it := range items {
			if out[i].Status != StatusChanged {
				continue
			}
			k := pathKey(it.Dir, out[i].NewName)
			reason := ""
			if len(holders[k]) > 1 {
				reason = "같은 새 이름이 또 있어요"
			} else if existing[DirKey(it.Dir)][foldKey(out[i].NewName)] && !listed[k] {
				// A file we don't manage already uses the name. (Names of listed
				// files are covered by the holders check above.)
				reason = "폴더에 같은 이름이 이미 있어요"
			}
			if reason != "" {
				out[i].Status, out[i].Note = StatusConflict, reason
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// Summarize counts previews by status.
func Summarize(ps []Preview) Summary { return summarize(ps) }

func summarize(ps []Preview) Summary {
	s := Summary{Total: len(ps)}
	for _, p := range ps {
		switch p.Status {
		case StatusChanged:
			s.Changed++
		case StatusConflict:
			s.Conflict++
		case StatusInvalid:
			s.Invalid++
		case StatusSkipped:
			s.Skipped++
		default:
			s.Unchanged++
		}
	}
	return s
}
