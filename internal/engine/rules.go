package engine

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Position says where inserted text goes.
type Position int

const (
	Before Position = iota // 앞
	After                  // 뒤
)

// DateSource says which date "날짜 넣기" uses.
type DateSource int

const (
	DateModified DateSource = iota // 수정한 날짜
	DateCreated                    // 만든 날짜
	DateTaken                      // 찍은 날짜 (없으면 수정한 날짜)
	DateToday                      // 오늘 날짜
)

// CaseMode changes upper/lower case of the name.
type CaseMode int

const (
	CaseKeep  CaseMode = iota // 그대로
	CaseLower                 // 모두 소문자
	CaseUpper                 // 모두 대문자
	CaseTitle                 // 단어 첫 글자만 대문자
)

// ExtMode changes the extension.
type ExtMode int

const (
	ExtKeep   ExtMode = iota // 그대로
	ExtLower                 // 소문자로
	ExtUpper                 // 대문자로
	ExtSet                   // 다른 확장자로 바꾸기
	ExtRemove                // 확장자 없애기
)

// ReplaceRule is "1. 찾아 바꾸기".
type ReplaceRule struct {
	On         bool   `json:"on"`
	Find       string `json:"find"`
	With       string `json:"with"`
	MatchCase  bool   `json:"matchCase"`
	Regex      bool   `json:"regex"`
	IncludeExt bool   `json:"includeExt"`
}

// TextRule is "2. 새 이름 · 글자 추가".
type TextRule struct {
	On      bool   `json:"on"`
	SetName bool   `json:"setName"` // replace the whole name with Name
	Name    string `json:"name"`
	Prefix  string `json:"prefix"`
	Suffix  string `json:"suffix"`
}

// DateRule is "3. 날짜 넣기".
type DateRule struct {
	On     bool       `json:"on"`
	Source DateSource `json:"source"`
	Format string     `json:"format"`
	Pos    Position   `json:"pos"`
	Sep    string     `json:"sep"`
}

// NumberRule is "4. 번호 붙이기".
type NumberRule struct {
	On        bool     `json:"on"`
	Start     int      `json:"start"`
	Step      int      `json:"step"`
	Digits    int      `json:"digits"`
	Pos       Position `json:"pos"`
	Sep       string   `json:"sep"`
	PerFolder bool     `json:"perFolder"`
}

// ExtraRule is "5. 기타".
type ExtraRule struct {
	On            bool     `json:"on"`
	Case          CaseMode `json:"case"`
	Ext           ExtMode  `json:"ext"`
	NewExt        string   `json:"newExt"`
	ComposeHangul bool     `json:"composeHangul"`
}

// Rules is the whole set of options, applied in this order:
// 찾아 바꾸기 → 새 이름·글자 추가 → (대소문자) → 날짜 → 번호 → 확장자.
type Rules struct {
	Replace ReplaceRule `json:"replace"`
	Text    TextRule    `json:"text"`
	Date    DateRule    `json:"date"`
	Number  NumberRule  `json:"number"`
	Extra   ExtraRule   `json:"extra"`
}

// DefaultRules returns the starting options.
func DefaultRules() Rules {
	return Rules{
		Date:   DateRule{Source: DateModified, Format: "YYYY-MM-DD", Pos: Before, Sep: "_"},
		Number: NumberRule{Start: 1, Step: 1, Digits: 3, Pos: After, Sep: "_"},
		Extra:  ExtraRule{Case: CaseKeep, Ext: ExtKeep},
	}
}

// AnyOn reports whether at least one rule is switched on.
func (r *Rules) AnyOn() bool {
	return r.Replace.On || r.Text.On || r.Date.On || r.Number.On || r.Extra.On
}

// Compiled is a validated, ready-to-run copy of Rules.
type Compiled struct {
	r   Rules
	re  *regexp.Regexp
	rep string
	now time.Time
}

// RuleError is a problem with the options the user typed.
type RuleError struct{ Msg string }

func (e *RuleError) Error() string { return e.Msg }

// Compile checks the rules and prepares them. now is used for "오늘 날짜".
func (r Rules) Compile(now time.Time) (*Compiled, error) {
	c := &Compiled{r: r, now: now}
	if r.Replace.On && r.Replace.Find != "" {
		pat := r.Replace.Find
		if !r.Replace.Regex {
			pat = regexp.QuoteMeta(pat)
		}
		if !r.Replace.MatchCase {
			pat = "(?i)" + pat
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, &RuleError{Msg: "찾을 글자의 정규식이 올바르지 않아요: " + regexpProblem(err)}
		}
		c.re = re
		if r.Replace.Regex {
			c.rep = normalizeReplacement(r.Replace.With)
		} else {
			// Plain mode: "$" in the replacement is literal text.
			c.rep = strings.ReplaceAll(r.Replace.With, "$", "$$")
		}
	}
	if r.Number.On {
		if r.Number.Step == 0 {
			c.r.Number.Step = 1
		}
		if c.r.Number.Digits < 1 {
			c.r.Number.Digits = 1
		}
		if c.r.Number.Digits > 12 {
			c.r.Number.Digits = 12
		}
	}
	if r.Date.On && strings.TrimSpace(r.Date.Format) == "" {
		c.r.Date.Format = "YYYY-MM-DD"
	}
	return c, nil
}

// regexpProblem explains a regular-expression error in Korean.
func regexpProblem(err error) string {
	var se *syntax.Error
	if !errors.As(err, &se) {
		return err.Error()
	}
	switch se.Code {
	case syntax.ErrMissingParen:
		return "닫는 괄호 ) 가 없어요"
	case syntax.ErrUnexpectedParen:
		return "여는 괄호 ( 없이 ) 가 있어요"
	case syntax.ErrMissingBracket:
		return "닫는 대괄호 ] 가 없어요"
	case syntax.ErrMissingRepeatArgument:
		return "*, +, ? 앞에 반복할 글자가 없어요"
	case syntax.ErrInvalidRepeatOp, syntax.ErrInvalidRepeatSize:
		return "반복 기호(*, +, ?, {})를 잘못 썼어요"
	case syntax.ErrInvalidEscape:
		return "\\ 뒤에 올 수 없는 글자예요"
	case syntax.ErrTrailingBackslash:
		return "맨 끝에 \\ 가 있어요"
	case syntax.ErrInvalidCharRange, syntax.ErrInvalidCharClass:
		return "[ ] 안의 글자 범위가 잘못됐어요"
	case syntax.ErrInvalidPerlOp, syntax.ErrInvalidNamedCapture:
		return "(? 뒤의 내용이 잘못됐어요"
	}
	return se.Error()
}

// normalizeReplacement lets users write $1, ${1}, \1 or $name for groups.
// Go would read "$1_x" as the group named "1_x", so numbered groups are braced.
func normalizeReplacement(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch == '$' || ch == '\\') && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '9' {
			j := i + 1
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			b.WriteString("${" + s[i+1:j] + "}")
			i = j - 1
			continue
		}
		if ch == '$' && i+1 < len(s) && s[i+1] == '$' {
			b.WriteString("$$")
			i++
			continue
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// Rules returns the (normalized) options this was compiled from.
func (c *Compiled) Rules() Rules { return c.r }

// DateFor returns the date the date rule would use for it, and whether a
// photo/video date was wanted but missing (so the modified date is used).
func (c *Compiled) DateFor(it *Item) (t time.Time, fellBack bool) {
	switch c.r.Date.Source {
	case DateCreated:
		if !it.Created.IsZero() {
			return it.Created, false
		}
		return it.Modified, false
	case DateTaken:
		if !it.Taken.IsZero() {
			return it.Taken, false
		}
		return it.Modified, true
	case DateToday:
		return c.now, false
	default:
		return it.Modified, false
	}
}

// NewName builds the new file name for it. number is the value for the
// numbering rule (already counted by the caller).
func (c *Compiled) NewName(it *Item, number int) string {
	r := &c.r
	orig := it.Name
	base, ext := SplitName(orig)

	if r.Extra.On && r.Extra.ComposeHangul {
		base = ComposeHangul(base)
		ext = ComposeHangul(ext)
	}

	// 1. 찾아 바꾸기
	if c.re != nil {
		if r.Replace.IncludeExt {
			base, ext = SplitName(c.re.ReplaceAllString(base+ext, c.rep))
		} else {
			base = c.re.ReplaceAllString(base, c.rep)
		}
	}

	// 2. 새 이름 · 글자 추가
	if r.Text.On {
		if r.Text.SetName {
			base = r.Text.Name
		}
		base = r.Text.Prefix + base + r.Text.Suffix
	}

	// 대소문자
	if r.Extra.On {
		base = applyCase(base, r.Extra.Case)
	}

	// 3. 날짜 넣기
	if r.Date.On {
		t, _ := c.DateFor(it)
		base = insertPiece(base, FormatDate(t, r.Date.Format), r.Date.Sep, r.Date.Pos)
	}

	// 4. 번호 붙이기
	if r.Number.On {
		base = insertPiece(base, FormatNumber(number, r.Number.Digits), r.Number.Sep, r.Number.Pos)
	}

	// 확장자
	if r.Extra.On {
		ext = applyExt(ext, r.Extra)
	}

	name := base + ext
	if name == orig {
		return orig
	}
	return cleanName(name)
}

// FormatNumber pads n with zeros to at least digits digits.
func FormatNumber(n, digits int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	for len(s) < digits {
		s = "0" + s
	}
	if neg {
		s = "-" + s
	}
	return s
}

func insertPiece(base, piece, sep string, pos Position) string {
	if piece == "" {
		return base
	}
	if base == "" {
		return piece
	}
	if pos == After {
		return base + sep + piece
	}
	return piece + sep + base
}

func applyCase(s string, mode CaseMode) string {
	switch mode {
	case CaseLower:
		return strings.ToLower(s)
	case CaseUpper:
		return strings.ToUpper(s)
	case CaseTitle:
		var b strings.Builder
		start := true
		for _, ch := range s {
			if unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '\'' {
				if start {
					b.WriteRune(unicode.ToUpper(ch))
				} else {
					b.WriteRune(unicode.ToLower(ch))
				}
				start = false
			} else {
				b.WriteRune(ch)
				start = true
			}
		}
		return b.String()
	}
	return s
}

func applyExt(ext string, r ExtraRule) string {
	switch r.Ext {
	case ExtLower:
		return strings.ToLower(ext)
	case ExtUpper:
		return strings.ToUpper(ext)
	case ExtSet:
		e := strings.TrimSpace(r.NewExt)
		e = strings.TrimLeft(e, ".")
		if e == "" {
			return ""
		}
		return "." + e
	case ExtRemove:
		return ""
	}
	return ext
}

// cleanName trims what Windows would silently drop: spaces around the
// base name and spaces or dots at the very end.
func cleanName(name string) string {
	base, ext := SplitName(name)
	base = strings.TrimSpace(base)
	name = base + ext
	return strings.TrimRight(name, " .")
}

// reserved device names that Windows refuses as file names.
var reservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// ValidateName returns "" when name is a valid Windows file name, or a short
// Korean reason otherwise.
func ValidateName(name string) string {
	if name == "" {
		return "이름이 비어 있어요"
	}
	base, _ := SplitName(name)
	if strings.TrimSpace(base) == "" {
		return "이름이 비어 있어요 (확장자만 남아요)"
	}
	var bad []string
	seen := map[rune]bool{}
	for _, ch := range name {
		if ch < 32 {
			if !seen[ch] {
				bad = append(bad, "제어 문자")
				seen[ch] = true
			}
			continue
		}
		if strings.ContainsRune(`\/:*?"<>|`, ch) && !seen[ch] {
			bad = append(bad, string(ch))
			seen[ch] = true
		}
	}
	if len(bad) > 0 {
		return "쓸 수 없는 글자가 있어요: " + strings.Join(bad, " ")
	}
	stem := name
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if reservedNames[strings.ToUpper(strings.TrimRight(stem, " "))] {
		return "Windows가 쓰는 예약된 이름이에요 (" + strings.ToUpper(stem) + ")"
	}
	if n := utf16Len(name); n > 255 {
		return fmt.Sprintf("이름이 너무 길어요 (%d자, 최대 255자)", n)
	}
	return ""
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 && utf8.ValidRune(r) {
			n += 2
		} else {
			n++
		}
	}
	return n
}
