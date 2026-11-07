// Package live はライブプレビューの「見え方」を決める純粋なロジック。
//
// Markdown の1行を、元の文字位置 (raw) と画面上の表示 (display) の対応つきで描き直す。
//
//   - Unit: 元テキストの範囲 [RawStart, RawEnd) を、画面では Text として見せる最小単位。
//     記号を隠すなら Text="" 、置き換えるなら別の文字列、そのままなら元の1文字。
//     RawStart == RawEnd の Unit は表示だけの挿入 (表の桁揃えの空白など)。
//   - View: 1行ぶんの Unit 列と、行末までの塗り (コードブロックの背景など)。
//   - Block: 文書全体を見ないと決まらない文脈 (コードブロック内か、表の列幅)。
//
// 文字位置はすべて rune 単位。折り返し位置も元テキストの文字位置で表すので、
// カーソル移動・選択・編集は元テキストのまま扱える。
package live

import (
	"regexp"
	"slices"
	"strings"
)

// BlockKind は行が属する文脈の種類。
type BlockKind int

const (
	Prose BlockKind = iota
	FenceOpen
	FenceBody
	FenceClose
	TableRow
	FrontMatter // 先頭の `---` で囲んだ設定 (YAML)
	SlideBreak  // Marp のスライドの区切り `---`
	Directive   // Marp の指示コメント `<!-- _class: lead -->` だけの行
)

// Block は1行の文脈。コードブロックの言語や表の列幅を持つ。
type Block struct {
	Kind      BlockKind
	Language  string // FenceOpen / FenceBody
	Widths    []int  // TableRow: 各列の表示幅
	Header    bool   // TableRow: 見出し行 / FrontMatter: 開始の `---`
	Delimiter bool   // TableRow: `| --- |` の行 / FrontMatter: 終わりの `---`
	Open      int    // Fence*: コードブロックの開始行
	Close     int    // Fence*: コードブロックの終了行 (閉じていなければ最終行)
	// Marp のスライド (front matter に `marp: true` がある文書だけ)
	Marp   bool
	Slide  int // この行が属するスライドの番号 (1 から)
	Slides int // スライドの枚数
}

// IsDiagram は Mermaid の図を書くコードブロックの行か。
func (b Block) IsDiagram() bool {
	return (b.Kind == FenceOpen || b.Kind == FenceBody || b.Kind == FenceClose) && b.Language == "mermaid"
}

func (b Block) Equal(o Block) bool {
	return b.Kind == o.Kind && b.Language == o.Language && b.Open == o.Open && b.Close == o.Close && b.Header == o.Header && b.Delimiter == o.Delimiter && slices.Equal(b.Widths, o.Widths) &&
		b.Marp == o.Marp && b.Slide == o.Slide && b.Slides == o.Slides
}

var (
	fenceRe          = regexp.MustCompile("^\\s*(`{3,}|~{3,})\\s*([\\w+#.-]*)")
	tableRowRe       = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	tableDelimiterRe = regexp.MustCompile(`^\s*\|(\s*:?-+:?\s*\|)+\s*$`)
	marpOnRe         = regexp.MustCompile(`^marp:\s*true\s*$`)
	directiveRe      = regexp.MustCompile(`^\s*<!--.*-->\s*$`)
)

// Analyze は各行がどの文脈にあるかを返す。
func Analyze(lines []string) []Block {
	blocks := make([]Block, len(lines))
	fence := ""
	language := ""
	open := 0
	closeFence := func(last int) {
		for row := open; row <= last; row++ {
			blocks[row].Open, blocks[row].Close = open, last
		}
	}
	start, marp := frontMatter(lines)
	for row := range start {
		blocks[row] = Block{Kind: FrontMatter, Marp: marp, Header: row == 0, Delimiter: row == start-1}
	}
	for i := start; i < len(lines); {
		line := lines[i]
		if fence != "" {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, fence) && strings.TrimSpace(trimmed[len(fence):]) == "" {
				blocks[i] = Block{Kind: FenceClose, Language: language}
				fence = ""
				closeFence(i)
			} else {
				blocks[i] = Block{Kind: FenceBody, Language: language}
			}
			i++
			continue
		}
		if m := fenceRe.FindStringSubmatch(line); m != nil {
			fence, language, open = m[1], m[2], i
			blocks[i] = Block{Kind: FenceOpen, Language: language}
			i++
			continue
		}
		if marp && isRule([]rune(line)) {
			blocks[i] = Block{Kind: SlideBreak}
			i++
			continue
		}
		if marp && directiveRe.MatchString(line) {
			blocks[i] = Block{Kind: Directive}
			i++
			continue
		}
		if tableRowRe.MatchString(line) && i+1 < len(lines) && tableDelimiterRe.MatchString(lines[i+1]) {
			end := i + 2
			for end < len(lines) && tableRowRe.MatchString(lines[end]) {
				end++
			}
			widths := tableWidths(lines[i:end])
			for row := i; row < end; row++ {
				blocks[row] = Block{Kind: TableRow, Widths: widths, Header: row == i, Delimiter: row == i+1}
			}
			i = end
			continue
		}
		i++
	}
	if fence != "" {
		closeFence(len(lines) - 1)
	}
	if marp {
		numberSlides(blocks, start)
	}
	return blocks
}

// frontMatter は先頭の `---` 〜 `---` の行数 (無ければ 0) と、それが Marp の文書か。
func frontMatter(lines []string) (end int, marp bool) {
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return 0, false
	}
	for i := 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "---" || trimmed == "..." {
			return i + 1, marp
		}
		marp = marp || marpOnRe.MatchString(trimmed)
	}
	return 0, false // 閉じていなければ front matter ではない
}

// numberSlides は Marp の文書の各行に、属するスライドの番号と枚数を書き込む。
func numberSlides(blocks []Block, start int) {
	slide := 1
	for i := range blocks {
		if i >= start && blocks[i].Kind == SlideBreak {
			slide++
		}
		blocks[i].Marp, blocks[i].Slide = true, slide
	}
	for i := range blocks {
		blocks[i].Slides = slide
	}
}

func tableWidths(rows []string) []int {
	var widths []int
	for index, row := range rows {
		runes := []rune(row)
		for column, cell := range splitCells(runes) {
			for len(widths) <= column {
				widths = append(widths, 3)
			}
			if index == 1 {
				continue // 区切り行の `---` は幅に数えない
			}
			widths[column] = max(widths[column], InlineWidth(strings.TrimSpace(string(runes[cell.start:cell.end]))))
		}
	}
	return widths
}

type span struct{ start, end int }

// splitCells は `| a | b |` のセル範囲 (パイプの内側) を返す。`\|` はセル区切りにしない。
func splitCells(line []rune) []span {
	var pipes []int
	for i, r := range line {
		if r == '|' && (i == 0 || line[i-1] != '\\') {
			pipes = append(pipes, i)
		}
	}
	cells := make([]span, 0, max(0, len(pipes)-1))
	for i := 0; i+1 < len(pipes); i++ {
		cells = append(cells, span{pipes[i] + 1, pipes[i+1]})
	}
	return cells
}
