package live

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Mojashi/memo/internal/theme"
)

// Palette はライブプレビューの配色。
type Palette struct {
	Heading     [6]theme.Style // 見出しレベル 1..6
	Marker      theme.Style
	Bullet      theme.Style
	TaskOpen    theme.Style
	TaskDone    theme.Style
	DoneText    theme.Style
	QuoteBar    theme.Style
	QuoteText   theme.Style
	Code        theme.Style
	CodeBlock   theme.Style
	CodeLabel   theme.Style
	Link        theme.Style
	Rule        theme.Style
	TableBorder theme.Style
	TableHeader theme.Style
	Bold        theme.Style
	Italic      theme.Style
	Strike      theme.Style
	// CodeColors はコードブロックの1行を言語に応じて色付けする。文字ごとの装飾を返す (分からなければ nil)。
	CodeColors func(language, line string) []theme.Style
}

// Render は1行の見え方を決める。concealed なら記号を隠した完成形、そうでなければ元テキストのまま。
func Render(line string, block Block, width int, concealed bool, palette *Palette) View {
	c := newCanvas([]rune(line), palette.Marker)
	fill := theme.Style{}
	switch block.Kind {
	case FenceOpen:
		label := block.Language
		if label == "" {
			label = "code"
		}
		c.replace(0, len(c.line), " "+label+" ", palette.CodeLabel)
		fill = palette.CodeBlock
	case FenceClose:
		c.replace(0, len(c.line), "", palette.Marker)
		fill = palette.CodeBlock
	case FenceBody:
		c.style(0, len(c.line), palette.CodeBlock)
		if palette.CodeColors != nil {
			for i, s := range palette.CodeColors(block.Language, line) {
				if i < len(c.line) {
					c.styles[i] = c.styles[i].Over(s)
				}
			}
		}
		fill = palette.CodeBlock
	case TableRow:
		tableRow(c, block, FitColumns(block.Widths, width), palette)
	case Prose:
		prose(c, width, palette)
	case FrontMatter:
		fill = palette.CodeBlock
		switch {
		case block.Header:
			label := "front matter"
			if block.Marp {
				label = "marp"
			}
			c.replace(0, len(c.line), " "+label+" ", palette.CodeLabel)
		case block.Delimiter:
			c.replace(0, len(c.line), "", palette.Marker)
		default:
			c.style(0, len(c.line), palette.Marker)
		}
	case SlideBreak:
		label := fmt.Sprintf(" スライド %d / %d ", block.Slide, block.Slides)
		c.replace(0, len(c.line), "──"+label+strings.Repeat("─", max(3, width-3-Width(label))), palette.Rule)
	case Directive:
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "<!--"), "-->"))
		c.replace(0, len(c.line), "⚙ "+inner, palette.Marker.Over(theme.Style{Italic: true}))
	}
	if concealed {
		return View{Units: c.concealed(), Fill: fill}
	}
	return View{Units: c.revealed(), Fill: fill}
}

var (
	headingRe        = regexp.MustCompile(`^(#{1,6})(\s+)`)
	headingTrailerRe = regexp.MustCompile(`\s+#+\s*$`)
	taskRe           = regexp.MustCompile(`^(\s*)([-*+]) \[([ xX])\] `)
	bulletRe         = regexp.MustCompile(`^(\s*)([-*+]) `)
	orderedRe        = regexp.MustCompile(`^(\s*)(\d+[.)]) `)
	quoteRe          = regexp.MustCompile(`^(\s*)((?:> ?)+)`)
	imageRe          = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]*)\)`)
	linkRe           = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	urlRe            = regexp.MustCompile(`https?://[^\s)>\]]+`)
)

var bullets = []string{"•", "◦", "▪"}

// mask は処理済みの記号を潰す印。後段のパターンに拾わせない。
const mask = '\x00'

func prose(c *canvas, width int, palette *Palette) {
	line := string(c.line)
	n := len(c.line)
	if isRule(c.line) {
		c.replace(0, n, strings.Repeat("─", max(3, width-1)), palette.Rule)
		return
	}

	if m := runeMatch(headingRe, line); m != nil {
		level := m[3] - m[2] // # の数
		bodyStart, bodyEnd := m[1], n
		c.hide(0, bodyStart)
		if t := runeMatch(headingTrailerRe, string(c.line[bodyStart:])); t != nil {
			c.hide(bodyStart+t[0], n)
			bodyEnd = bodyStart + t[0]
		}
		c.style(bodyStart, bodyEnd, palette.Heading[level-1])
		inline(c, bodyStart, bodyEnd, palette)
		return
	}

	start := 0
	if m := runeMatch(quoteRe, line); m != nil {
		depth := strings.Count(string(c.line[m[4]:m[5]]), ">")
		c.replace(m[4], m[5], strings.Repeat("▌ ", depth), palette.QuoteBar)
		c.style(m[5], n, palette.QuoteText)
		start = m[5]
	}

	rest := string(c.line[start:])
	if m := runeMatch(taskRe, rest); m != nil {
		done := c.line[start+m[6]] != ' '
		marker, style := "☐ ", palette.TaskOpen
		if done {
			marker, style = "☑ ", palette.TaskDone
			c.style(start+m[1], n, palette.DoneText)
		}
		c.replace(start+m[3], start+m[1], marker, style)
		start += m[1]
	} else if m := runeMatch(bulletRe, rest); m != nil {
		depth := len(strings.ReplaceAll(string(c.line[start+m[2]:start+m[3]]), "\t", "  ")) / 2
		c.replace(start+m[4], start+m[1], bullets[depth%len(bullets)]+" ", palette.Bullet)
		start += m[1]
	} else if m := runeMatch(orderedRe, rest); m != nil {
		c.style(start+m[4], start+m[5], palette.Bullet)
		start += m[1]
	}

	inline(c, start, n, palette)
}

// isRule は `---` `***` `___` (間に空白可) の区切り線か。
func isRule(line []rune) bool {
	indent := 0
	for indent < len(line) && line[indent] == ' ' {
		indent++
	}
	if indent > 3 || indent == len(line) {
		return false
	}
	mark := line[indent]
	if mark != '-' && mark != '*' && mark != '_' {
		return false
	}
	count := 0
	for _, r := range line[indent:] {
		switch {
		case r == mark:
			count++
		case unicode.IsSpace(r):
		default:
			return false
		}
	}
	return count >= 3
}

// inline は行内の装飾。処理済みの記号は mask で潰し、後段のパターンに拾わせない。
func inline(c *canvas, start, end int, palette *Palette) {
	chars := make([]rune, len(c.line))
	for i, r := range c.line {
		if start <= i && i < end {
			chars[i] = r
		} else {
			chars[i] = mask
		}
	}
	maskRange := func(a, b int) {
		for i := a; i < b; i++ {
			chars[i] = mask
		}
	}

	for _, m := range codeSpans(chars) {
		c.hide(m.open, m.body.start)
		c.hide(m.body.end, m.close)
		c.style(m.body.start, m.body.end, palette.Code)
		maskRange(m.open, m.close)
	}

	for _, m := range runeMatchAll(imageRe, chars) {
		c.replace(m[0], m[2], "▣ ", palette.Link)
		c.hide(m[3], m[1])
		c.style(m[2], m[3], palette.Link)
		maskRange(m[0], m[1])
	}

	for _, m := range runeMatchAll(linkRe, chars) {
		c.hide(m[0], m[2])
		c.hide(m[3], m[1])
		c.style(m[2], m[3], palette.Link)
		maskRange(m[0], m[2])
		maskRange(m[3], m[1])
	}

	for _, d := range []struct {
		find  func([]rune) []delimited
		style theme.Style
	}{
		{func(s []rune) []delimited { return findDelimited(s, []string{"**", "__"}, false) }, palette.Bold},
		{func(s []rune) []delimited { return findDelimited(s, []string{"~~"}, false) }, palette.Strike},
		{func(s []rune) []delimited { return findDelimited(s, []string{"*", "_"}, true) }, palette.Italic},
	} {
		for _, m := range d.find(chars) {
			c.hide(m.open, m.body.start)
			c.hide(m.body.end, m.close)
			c.style(m.body.start, m.body.end, d.style)
			maskRange(m.open, m.body.start)
			maskRange(m.body.end, m.close)
		}
	}

	for _, m := range runeMatchAll(urlRe, chars) {
		c.style(m[0], m[1], palette.Link)
	}
}

// delimited は「開き記号 本文 閉じ記号」の一致。
type delimited struct {
	open, close int
	body        span
}

// codeSpans は `code` / “code“ を探す (同じ数のバッククォートで閉じる)。
func codeSpans(s []rune) []delimited {
	var found []delimited
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		ticks := 0
		for i+ticks < len(s) && s[i+ticks] == '`' {
			ticks++
		}
		closeAt := -1
		for j := i + ticks + 1; j+ticks <= len(s); j++ {
			if allRunes(s[j:j+ticks], '`') {
				closeAt = j
				break
			}
		}
		if closeAt < 0 {
			i += ticks
			continue
		}
		found = append(found, delimited{open: i, close: closeAt + ticks, body: span{i + ticks, closeAt}})
		i = closeAt + ticks
	}
	return found
}

// findDelimited は `**本文**` のような強調を探す。本文の両端は空白でない。
// flanking なら記号の外側が同じ記号でないことも求め、`_` は単語の途中も避ける (`snake_case` を斜体にしない)。
func findDelimited(s []rune, marks []string, flanking bool) []delimited {
	var found []delimited
	boundaryOf := func(mark rune) func(int) bool {
		return func(i int) bool {
			if i < 0 || i >= len(s) {
				return true
			}
			if mark == '_' {
				return !(s[i] == '_' || unicode.IsLetter(s[i]) || unicode.IsDigit(s[i]))
			}
			return s[i] != mark
		}
	}
	for i := 0; i < len(s); {
		matched := false
		for _, mark := range marks {
			d := []rune(mark)
			L := len(d)
			boundary := boundaryOf(d[0])
			if !hasRunes(s, i, d) || i+L >= len(s) || unicode.IsSpace(s[i+L]) || (flanking && !boundary(i-1)) {
				continue
			}
			for j := i + L + 1; j+L <= len(s); j++ {
				if hasRunes(s, j, d) && !unicode.IsSpace(s[j-1]) && (!flanking || boundary(j+L)) {
					found = append(found, delimited{open: i, close: j + L, body: span{i + L, j}})
					i = j + L
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if !matched {
			i++
		}
	}
	return found
}

func hasRunes(s []rune, at int, want []rune) bool {
	if at+len(want) > len(s) {
		return false
	}
	for k, r := range want {
		if s[at+k] != r {
			return false
		}
	}
	return true
}

func allRunes(s []rune, r rune) bool {
	for _, x := range s {
		if x != r {
			return false
		}
	}
	return true
}

// FitColumns は表が width に収まるよう、広い列から順に縮めた列幅を返す (最小 3)。
// 表の行は折り返すと崩れるので、はみ出すセルは末尾を「…」で切る。
func FitColumns(widths []int, width int) []int {
	fitted := slices.Clone(widths)
	total := func() int {
		sum := 1
		for _, w := range fitted {
			sum += w + 3
		}
		return sum
	}
	for total() > width {
		widest := 0
		for i, w := range fitted {
			if w > fitted[widest] {
				widest = i
			}
		}
		if fitted[widest] <= 3 {
			break
		}
		fitted[widest]--
	}
	return fitted
}

func tableRow(c *canvas, row Block, widths []int, palette *Palette) {
	n := len(c.line)
	if row.Delimiter {
		parts := make([]string, len(widths))
		for i, w := range widths {
			parts[i] = strings.Repeat("─", w+2)
		}
		c.replace(0, n, "├"+strings.Join(parts, "┼")+"┤", palette.TableBorder)
		return
	}
	cells := splitCells(c.line)
	firstPipe := 0
	for firstPipe < n && c.line[firstPipe] != '|' {
		firstPipe++
	}
	c.hide(0, firstPipe)
	c.replace(firstPipe, firstPipe+1, "│", palette.TableBorder)
	for _, cell := range cells {
		c.replace(cell.end, cell.end+1, "│", palette.TableBorder)
	}
	for column, cell := range cells {
		bodyStart, bodyEnd := cell.start, cell.start
		if strings.TrimSpace(string(c.line[cell.start:cell.end])) != "" {
			bodyStart = cell.start
			for unicode.IsSpace(c.line[bodyStart]) {
				bodyStart++
			}
			bodyEnd = cell.end
			for unicode.IsSpace(c.line[bodyEnd-1]) {
				bodyEnd--
			}
		}
		c.hide(cell.start, bodyStart)
		c.hide(bodyEnd, cell.end)
		if row.Header {
			c.style(bodyStart, bodyEnd, palette.TableHeader)
		}
		inline(c, bodyStart, bodyEnd, palette)
		width := 0
		if column < len(widths) {
			width = widths[column]
		}
		used := c.truncate(bodyStart, bodyEnd, width)
		c.insert(bodyStart, " ", theme.Style{})
		c.insert(bodyEnd, strings.Repeat(" ", max(0, width-used))+" ", theme.Style{})
	}
	if len(cells) > 0 && cells[len(cells)-1].end+1 < n {
		c.hide(cells[len(cells)-1].end+1, n)
	}
	for column := len(cells); column < len(widths); column++ {
		c.insert(n, strings.Repeat(" ", widths[column]+2)+"│", palette.TableBorder)
	}
}

// truncate は [start, end) の見た目が width に収まるよう、はみ出す部分を隠して「…」を付ける。見た目の幅を返す。
func (c *canvas) truncate(start, end, width int) int {
	widthAt := func(i int) (w, next int) {
		if rep, ok := c.replaced[i]; ok {
			return Width(rep.text), rep.end
		}
		if c.hidden[i] {
			return 0, i + 1
		}
		return Width(string(c.line[i])), i + 1
	}
	total := 0
	for i := start; i < end; {
		w, next := widthAt(i)
		total += w
		i = next
	}
	if total <= width {
		return total
	}
	used := 0
	for i := start; i < end; {
		w, next := widthAt(i)
		if used+w > width-1 {
			for j := i; j < end; j++ {
				delete(c.replaced, j)
			}
			c.hide(i, end)
			c.insert(end, "…", theme.Style{})
			return used + 1
		}
		used += w
		i = next
	}
	return used
}

var plainPalette = &Palette{}

// InlineWidth は行内装飾の記号を隠したときの表示幅。
func InlineWidth(text string) int {
	c := newCanvas([]rune(text), theme.Style{})
	inline(c, 0, len(c.line), plainPalette)
	width := 0
	for _, u := range c.concealed() {
		width += u.width
	}
	return width
}

// runeMatch は正規表現の一致位置を rune 単位で返す (サブマッチも含む)。
func runeMatch(re *regexp.Regexp, s string) []int {
	m := re.FindStringSubmatchIndex(s)
	if m == nil {
		return nil
	}
	return toRuneIndexes(s, m)
}

func runeMatchAll(re *regexp.Regexp, chars []rune) [][]int {
	s := string(chars)
	all := re.FindAllStringSubmatchIndex(s, -1)
	for k, m := range all {
		all[k] = toRuneIndexes(s, m)
	}
	return all
}

func toRuneIndexes(s string, byteIndexes []int) []int {
	out := make([]int, len(byteIndexes))
	for k, b := range byteIndexes {
		if b < 0 {
			out[k] = -1
			continue
		}
		out[k] = utf8.RuneCountInString(s[:b])
	}
	return out
}
