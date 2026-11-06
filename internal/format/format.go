// Package format は Markdown の書式操作の語彙。
//
//   - Surround: 選択範囲を記号で囲む (太字・斜体・コード・リンク)。囲まれていれば外す
//   - LinePrefix: 選択行の先頭に記号を付け外しする (見出し・リスト・引用)
//   - Block: カーソル位置に独立したブロックを差し込む (表・コードブロック・区切り線)
//
// どれも Apply(target) だけを持ち、エディタの選択状態から次の選択状態を決める。
package format

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Mojashi/memo/internal/textbuf"
)

type Pos = textbuf.Pos

// Target は書式を当てる相手 (エディタ)。
type Target interface {
	Selection() (start, end Pos)
	SelectedText() string
	Line(row int) string
	Replace(start, end Pos, text string) Pos
	Select(anchor, cursor Pos)
}

type Format interface{ Apply(t Target) }

func runes(s string) int { return utf8.RuneCountInString(s) }

// ── Surround ───────────────────────────────────────────

type Surround struct{ Left, Right, Placeholder string }

func (s Surround) Apply(t Target) {
	start, end := t.Selection()
	selected := t.SelectedText()
	if len(selected) >= len(s.Left+s.Right) && strings.HasPrefix(selected, s.Left) && strings.HasSuffix(selected, s.Right) {
		inner := selected[len(s.Left) : len(selected)-len(s.Right)]
		t.Select(start, t.Replace(start, end, inner))
		return
	}
	body := selected
	if body == "" {
		body = s.Placeholder
	}
	after := t.Replace(start, end, s.Left+body+s.Right)
	t.Select(Pos{Row: start.Row, Col: start.Col + runes(s.Left)}, Pos{Row: after.Row, Col: after.Col - runes(s.Right)})
}

// ── LinePrefix ─────────────────────────────────────────

// LinePrefix は選択行それぞれに Prefix(i) を付ける。全行に付いていれば外す (トグル)。
// Replaces に一致する既存の記号は付け替える (例: `## ` → `### `)。
type LinePrefix struct {
	Prefix   func(i int) string
	Replaces *regexp.Regexp
}

func (l LinePrefix) Apply(t Target) {
	start, end := t.Selection()
	top, bottom := start.Row, end.Row
	if bottom > top && end.Col == 0 {
		bottom--
	}
	var lines, prefixes []string
	already := true
	for row := top; row <= bottom; row++ {
		line, prefix := t.Line(row), l.Prefix(row-top)
		lines, prefixes = append(lines, line), append(prefixes, prefix)
		already = already && strings.HasPrefix(line, prefix)
	}
	updated := make([]string, len(lines))
	for i, line := range lines {
		switch {
		case already:
			updated[i] = line[len(prefixes[i]):]
		case l.Replaces != nil:
			if m := l.Replaces.FindStringIndex(line); m != nil {
				line = line[:m[0]] + line[m[1]:]
			}
			updated[i] = prefixes[i] + line
		default:
			updated[i] = prefixes[i] + line
		}
	}
	t.Replace(Pos{Row: top}, Pos{Row: bottom, Col: runes(lines[len(lines)-1])}, strings.Join(updated, "\n"))
	last := Pos{Row: bottom, Col: runes(updated[len(updated)-1])}
	if top == bottom {
		t.Select(last, last)
	} else {
		t.Select(Pos{Row: top}, last)
	}
}

// ── Block ──────────────────────────────────────────────

// Block はカーソル位置に独立したブロックを差し込む。前の行に文字があれば空行を挟む。
type Block struct{ Text string }

func (b Block) Apply(t Target) {
	start, end := t.Selection()
	before := ""
	switch {
	case start.Col > 0:
		before = "\n\n"
	case start.Row > 0 && strings.TrimSpace(t.Line(start.Row-1)) != "":
		before = "\n"
	}
	after := t.Replace(start, end, before+b.Text+"\n")
	t.Select(after, after)
}

// ── 書式の一覧 ─────────────────────────────────────────

var (
	headingMark = regexp.MustCompile(`^#{1,6}\s+`)
	listMark    = regexp.MustCompile(`^(\s*)([-*+]\s+\[[ xX]\]\s+|[-*+]\s+|\d+[.)]\s+|>\s?)`)
)

func heading(level int) LinePrefix {
	return LinePrefix{Prefix: constant(strings.Repeat("#", level) + " "), Replaces: headingMark}
}

func constant(prefix string) func(int) string { return func(int) string { return prefix } }

// Action はツールバーやコマンドパレットから選べる書式。
type Action struct {
	ID          string
	Label       string // ツールバーの表示
	Description string
	Format      Format
}

var Actions = []Action{
	{"h1", "H1", "見出し 1", heading(1)},
	{"h2", "H2", "見出し 2", heading(2)},
	{"h3", "H3", "見出し 3", heading(3)},
	{"bold", "B", "太字", Surround{"**", "**", "太字"}},
	{"italic", "I", "斜体", Surround{"*", "*", "斜体"}},
	{"strike", "S", "取り消し線", Surround{"~~", "~~", "取り消し"}},
	{"code", "</>", "インラインコード", Surround{"`", "`", "code"}},
	{"link", "Link", "リンク", Surround{"[", "](https://)", "リンク"}},
	{"bullet", "•", "箇条書き", LinePrefix{constant("- "), listMark}},
	{"number", "1.", "番号付きリスト", LinePrefix{func(i int) string { return strconv.Itoa(i+1) + ". " }, listMark}},
	{"task", "☐", "チェックリスト", LinePrefix{constant("- [ ] "), listMark}},
	{"quote", ">", "引用", LinePrefix{constant("> "), listMark}},
	{"codeblock", "{ }", "コードブロック", Block{"```\n\n```"}},
	{"table", "▦", "表", Block{"| 項目 | 内容 |\n| --- | --- |\n|  |  |"}},
	{"rule", "—", "区切り線", Block{"---"}},
}

// ToolbarGroups はツールバーの並び (グループの間に区切りを入れる)。
var ToolbarGroups = [][]string{
	{"h1", "h2", "h3"},
	{"bold", "italic", "strike", "code", "link"},
	{"bullet", "number", "task", "quote"},
	{"codeblock", "table", "rule"},
}

func ByID(id string) Action {
	for _, a := range Actions {
		if a.ID == id {
			return a
		}
	}
	panic("format: unknown action " + id)
}
