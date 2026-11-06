// Package notes はノート本文の解析: 統計と目次。
package notes

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	cjkRe     = regexp.MustCompile(`[\x{3040}-\x{30ff}\x{3400}-\x{4dbf}\x{4e00}-\x{9fff}\x{f900}-\x{faff}\x{ff66}-\x{ff9f}]`)
	wordRe    = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9_'’-]*`)
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.+?)\s*#*\s*$`)
	fenceRe   = regexp.MustCompile("^\\s*(```|~~~)")
)

const (
	cjkCharsPerMinute = 500
	wordsPerMinute    = 220
)

type Stats struct {
	Chars          int // 空白を除いた文字数
	Words          int // 英単語数 + 和文文字数
	Lines          int
	ReadingMinutes float64
}

func Measure(text string) Stats {
	cjk := len(cjkRe.FindAllStringIndex(text, -1))
	latin := len(wordRe.FindAllStringIndex(text, -1))
	chars := 0
	for _, r := range text {
		if !unicode.IsSpace(r) {
			chars++
		}
	}
	lines := 0
	if text != "" {
		lines = strings.Count(text, "\n") + 1
	}
	return Stats{
		Chars:          chars,
		Words:          latin + cjk,
		Lines:          lines,
		ReadingMinutes: float64(cjk)/cjkCharsPerMinute + float64(latin)/wordsPerMinute,
	}
}

type Heading struct {
	Level int
	Title string
	Line  int
}

// Outline は見出し一覧。コードブロック内の `#` は見出しとして扱わない。
func Outline(lines []string) []Heading {
	var headings []Heading
	inFence := false
	for i, line := range lines {
		if fenceRe.MatchString(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRe.FindStringSubmatch(line); m != nil {
			headings = append(headings, Heading{len(m[1]), m[2], i})
		}
	}
	return headings
}
