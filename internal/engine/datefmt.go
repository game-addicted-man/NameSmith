package engine

import (
	"strconv"
	"strings"
	"time"
)

// DatePreset is a ready-made date format offered in the UI.
type DatePreset struct {
	Format string // pattern with tokens
	Label  string // what the user sees
}

// DatePresets are the formats in the "형식" drop-down.
var DatePresets = []DatePreset{
	{"YYYY-MM-DD", "2026-05-01"},
	{"YYYYMMDD", "20260501"},
	{"YYYY.MM.DD", "2026.05.01"},
	{"YYMMDD", "260501"},
	{"YYYY-MM-DD HH.mm.ss", "2026-05-01 13.45.30"},
	{"YYYYMMDD_HHmmss", "20260501_134530"},
	{"YYYY년 M월 D일", "2026년 5월 1일"},
	{"YYYY-MM", "2026-05"},
}

var weekdaysKo = [...]string{"일", "월", "화", "수", "목", "금", "토"}

// FormatDate writes t using a simple pattern:
//
//	YYYY 2026   YY 26   MM 05   M 5   DD 01   D 1
//	HH 13 (24시간)  H 13   mm 45 (분)   ss 30 (초)   ddd 월 (요일)
//
// Text inside 'single quotes' is kept as is; anything else that is not a
// token (Korean letters, -, _, . and so on) is copied unchanged.
func FormatDate(t time.Time, pattern string) string {
	var b strings.Builder
	two := func(n int) string {
		if n < 10 {
			return "0" + strconv.Itoa(n)
		}
		return strconv.Itoa(n)
	}
	p := pattern
	for len(p) > 0 {
		switch {
		case p[0] == '\'':
			end := strings.IndexByte(p[1:], '\'')
			if end < 0 {
				b.WriteString(p[1:])
				p = ""
			} else {
				b.WriteString(p[1 : 1+end])
				p = p[end+2:]
			}
		case strings.HasPrefix(p, "YYYY"):
			b.WriteString(strconv.Itoa(t.Year()))
			p = p[4:]
		case strings.HasPrefix(p, "YY"):
			b.WriteString(two(t.Year() % 100))
			p = p[2:]
		case strings.HasPrefix(p, "MM"):
			b.WriteString(two(int(t.Month())))
			p = p[2:]
		case strings.HasPrefix(p, "M"):
			b.WriteString(strconv.Itoa(int(t.Month())))
			p = p[1:]
		case strings.HasPrefix(p, "DD"):
			b.WriteString(two(t.Day()))
			p = p[2:]
		case strings.HasPrefix(p, "D"):
			b.WriteString(strconv.Itoa(t.Day()))
			p = p[1:]
		case strings.HasPrefix(p, "ddd"):
			b.WriteString(weekdaysKo[t.Weekday()])
			p = p[3:]
		case strings.HasPrefix(p, "HH"):
			b.WriteString(two(t.Hour()))
			p = p[2:]
		case strings.HasPrefix(p, "H"):
			b.WriteString(strconv.Itoa(t.Hour()))
			p = p[1:]
		case strings.HasPrefix(p, "mm"):
			b.WriteString(two(t.Minute()))
			p = p[2:]
		case strings.HasPrefix(p, "ss"):
			b.WriteString(two(t.Second()))
			p = p[2:]
		default:
			// copy one whole UTF-8 character
			n := 1
			for n < len(p) && p[n]&0xC0 == 0x80 {
				n++
			}
			b.WriteString(p[:n])
			p = p[n:]
		}
	}
	return b.String()
}
