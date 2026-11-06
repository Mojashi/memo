package live

import (
	"strings"

	"github.com/Mojashi/memo/internal/theme"
)

// Unit は元テキストの範囲を画面でどう見せるかの最小単位。
type Unit struct {
	RawStart, RawEnd int
	Text             string
	Style            theme.Style
	width            int
}

func newUnit(start, end int, text string, style theme.Style) Unit {
	return Unit{RawStart: start, RawEnd: end, Text: text, Style: style, width: Width(text)}
}

func (u Unit) Width() int { return u.width }

// View は1行ぶんの見え方。
type View struct {
	Units []Unit
	Fill  theme.Style // 行末まで塗る装飾 (コードブロックの背景)
}

type replacement struct {
	end   int
	text  string
	style theme.Style
}

type insertion struct {
	text  string
	style theme.Style
}

// canvas は元テキストの各文字に「隠す・装飾する・置き換える・前に挿入する」を書き込む下書き。
type canvas struct {
	line     []rune
	hidden   []bool
	styles   []theme.Style
	replaced map[int]replacement
	inserted map[int][]insertion
	marker   theme.Style // 元テキストのまま見せるとき、隠すはずだった記号の装飾
}

func newCanvas(line []rune, marker theme.Style) *canvas {
	return &canvas{
		line:     line,
		hidden:   make([]bool, len(line)),
		styles:   make([]theme.Style, len(line)),
		replaced: map[int]replacement{},
		inserted: map[int][]insertion{},
		marker:   marker,
	}
}

func (c *canvas) hide(start, end int) {
	for i := start; i < end; i++ {
		c.hidden[i] = true
	}
}

func (c *canvas) style(start, end int, style theme.Style) {
	for i := start; i < end; i++ {
		c.styles[i] = c.styles[i].Over(style)
	}
}

func (c *canvas) replace(start, end int, text string, style theme.Style) {
	c.replaced[start] = replacement{end, text, style}
}

func (c *canvas) insert(at int, text string, style theme.Style) {
	c.inserted[at] = append(c.inserted[at], insertion{text, style})
}

// concealed は記号を隠した完成形の Unit 列。
func (c *canvas) concealed() []Unit {
	units := make([]Unit, 0, len(c.line))
	display := 0
	for i := 0; ; {
		for _, ins := range c.inserted[i] {
			u := newUnit(i, i, ins.text, ins.style)
			units = append(units, u)
			display += u.width
		}
		if i >= len(c.line) {
			break
		}
		switch rep, ok := c.replaced[i]; {
		case ok:
			u := newUnit(i, rep.end, rep.text, rep.style)
			units = append(units, u)
			display += u.width
			i = rep.end
		case c.hidden[i]:
			units = append(units, Unit{RawStart: i, RawEnd: i + 1})
			i++
		default:
			u := newUnit(i, i+1, charText(c.line[i], display), c.styles[i])
			units = append(units, u)
			display += u.width
			i++
		}
	}
	return units
}

// revealed は元テキストをそのまま見せる Unit 列。装飾は残し、記号は控えめな色で見せる。
func (c *canvas) revealed() []Unit {
	symbol := make([]theme.Style, len(c.line))
	isSymbol := make([]bool, len(c.line))
	for start, rep := range c.replaced {
		for i := start; i < rep.end; i++ {
			isSymbol[i] = true
			symbol[i] = c.marker.Over(rep.style)
		}
	}
	units := make([]Unit, 0, len(c.line))
	display := 0
	for i, r := range c.line {
		style := c.styles[i]
		switch {
		case isSymbol[i]:
			style = style.Over(symbol[i])
		case c.hidden[i]:
			style = style.Over(c.marker)
		}
		u := newUnit(i, i+1, charText(r, display), style)
		units = append(units, u)
		display += u.width
	}
	return units
}

const tabWidth = 4

// charText は1文字の表示。タブは次のタブ位置までの空白にする。
func charText(r rune, display int) string {
	if r == '\t' {
		return strings.Repeat(" ", tabWidth-display%tabWidth)
	}
	return string(r)
}
