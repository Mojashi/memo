// Package syntax はコードの色付け。字句解析は chroma に任せ、色はアプリのテーマから決める。
package syntax

import (
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/Mojashi/memo/internal/theme"
)

// Colors はトークンの種類ごとの色。
type Colors struct{ t theme.Theme }

func NewColors(t theme.Theme) Colors { return Colors{t} }

func (c Colors) of(token chroma.TokenType) theme.Style {
	t := c.t
	switch {
	case token.InCategory(chroma.Comment):
		return theme.Style{Fg: t.Muted(), Italic: true}
	case token.InCategory(chroma.Keyword):
		return theme.Style{Fg: t.Secondary, Bold: token != chroma.KeywordType}
	case token.InSubCategory(chroma.LiteralString):
		return theme.Style{Fg: t.Success}
	case token.InSubCategory(chroma.LiteralNumber), token == chroma.KeywordConstant:
		return theme.Style{Fg: t.Warning}
	case token == chroma.NameFunction, token == chroma.NameClass, token == chroma.NameTag:
		return theme.Style{Fg: t.Primary}
	case token == chroma.NameBuiltin, token == chroma.NameAttribute, token == chroma.NameDecorator:
		return theme.Style{Fg: t.Accent}
	case token == chroma.NameTag:
		return theme.Style{Fg: t.Primary}
	case token.InCategory(chroma.Generic) && token == chroma.GenericHeading:
		return theme.Style{Fg: t.Primary, Bold: true}
	}
	return theme.Style{}
}

// Lines は text を字句解析し、行ごと・文字ごとの装飾を返す。言語が分からなければ nil。
func (c Colors) Lines(lexer chroma.Lexer, text string) [][]theme.Style {
	if lexer == nil {
		return nil
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, text)
	if err != nil {
		return nil
	}
	lines := [][]theme.Style{nil}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		style := c.of(token.Type)
		for i, part := range strings.Split(token.Value, "\n") {
			if i > 0 {
				lines = append(lines, nil)
			}
			last := &lines[len(lines)-1]
			for range utf8.RuneCountInString(part) {
				*last = append(*last, style)
			}
		}
	}
	return lines
}

// ForFile はファイル名から言語を決める。分からなければ nil。
func ForFile(path string) chroma.Lexer {
	return lexers.Match(path)
}

// ForLanguage はコードブロックの言語名 (```python など) から言語を決める。
func ForLanguage(name string) chroma.Lexer {
	if name == "" {
		return nil
	}
	return lexers.Get(name)
}

// FenceColors はコードブロックの1行を色付けする関数を返す (live.Palette.CodeColors)。
func (c Colors) FenceColors() func(language, line string) []theme.Style {
	cache := map[[2]string][]theme.Style{}
	return func(language, line string) []theme.Style {
		key := [2]string{language, line}
		if styles, ok := cache[key]; ok {
			return styles
		}
		if len(cache) > 4096 {
			clear(cache)
		}
		var styles []theme.Style
		if lines := c.Lines(ForLanguage(language), line); len(lines) > 0 {
			styles = lines[0]
		}
		cache[key] = styles
		return styles
	}
}
