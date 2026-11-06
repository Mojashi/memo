package live

import (
	"sort"
	"strings"

	"github.com/rivo/uniseg"
)

// Width は文字列の表示幅。曖昧幅の文字 (☐ • ─ など) は 1 として数える (kitty と同じ)。
func Width(s string) int {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 || s[i] < 0x20 {
			ascii = false
			break
		}
	}
	if ascii {
		return len(s)
	}
	return uniseg.StringWidth(s)
}

// Wrap は表示幅で折り返し、折り返し位置を元テキストの文字位置で返す。空白の直後で折るのを優先する。
func Wrap(units []Unit, width int) []int {
	if width <= 0 {
		return nil
	}
	var breaks []int
	sectionStart, column := 0, 0
	afterSpace, consumed := -1, 0
	for _, u := range units {
		w := u.width
		if column+w > width && column > 0 {
			cut := u.RawStart
			// 英単語の途中なら直前の空白の後ろで折る。和文はどこで折ってもよい
			if afterSpace > sectionStart && isWordPart(u.Text) {
				cut = afterSpace
				column -= consumed
			} else {
				column = 0
			}
			if cut > sectionStart {
				breaks = append(breaks, cut)
				sectionStart = cut
			}
			afterSpace = -1
		}
		column += w
		if strings.HasSuffix(u.Text, " ") && u.RawEnd > u.RawStart {
			afterSpace, consumed = u.RawEnd, column
		}
	}
	return breaks
}

// SectionOf は元テキストの位置 col が折り返しの何段目にあるか。
func SectionOf(breaks []int, col int) int {
	return sort.Search(len(breaks), func(i int) bool { return breaks[i] > col })
}

// SectionUnits は折り返しの section 段目に並ぶ Unit。
func SectionUnits(units []Unit, breaks []int, section int) []Unit {
	start, end := 0, len(units)
	for i, u := range units {
		s := SectionOf(breaks, u.RawStart)
		if s < section {
			start = i + 1
		} else if s > section {
			end = i
			break
		}
	}
	return units[start:end]
}

// XOf は元テキストの位置 col が画面上で何段目・何桁目に見えるか。
func XOf(units []Unit, breaks []int, col int) (section, x int) {
	section = SectionOf(breaks, col)
	for _, u := range SectionUnits(units, breaks, section) {
		if u.RawStart >= col && !(u.RawStart == u.RawEnd && u.RawStart == col) {
			break
		}
		x += u.width
	}
	return section, x
}

// ColumnAt は折り返しの section 段目・表示上の x 桁目に対応する、元テキストの文字位置。
func ColumnAt(units []Unit, breaks []int, section, x, lineLength int) int {
	column := 0
	for _, u := range SectionUnits(units, breaks, section) {
		if u.width == 0 {
			continue
		}
		if x < column+u.width {
			// 1文字ぶんの単位ならその文字の前、記号の置き換え (例: "- " → "• ") なら記号の後ろ
			if u.RawEnd-u.RawStart <= 1 {
				return u.RawStart
			}
			return u.RawEnd
		}
		column += u.width
	}
	if section >= len(breaks) {
		return lineLength
	}
	// 段の右端より右: 次の段の先頭ではなく、この段の最後の文字の前 (たいてい折り返した空白)
	start := 0
	if section > 0 {
		start = breaks[section-1]
	}
	return max(start, breaks[section]-1)
}

// isWordPart は英単語の一部になる1文字か (ここで折ると単語が割れる)。
func isWordPart(s string) bool {
	return len(s) == 1 && s[0] > ' ' && s[0] < 0x7f
}
