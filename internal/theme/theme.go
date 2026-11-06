// Package theme は配色と文字装飾の語彙。
//
// Style は「上から重ねられる装飾」で、未指定の色は下の色を残す。
// Theme は画面全体の基本色で、そこから各部品の色を導く。
package theme

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
)

// Style は重ね合わせられる文字装飾。ゼロ値は「何も変えない」。
type Style struct {
	Fg, Bg    tcell.Color
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
}

// Over は s の上に top を重ねる。top で指定された色・装飾が勝つ。
func (s Style) Over(top Style) Style {
	if top.Fg != tcell.ColorDefault {
		s.Fg = top.Fg
	}
	if top.Bg != tcell.ColorDefault {
		s.Bg = top.Bg
	}
	s.Bold = s.Bold || top.Bold
	s.Italic = s.Italic || top.Italic
	s.Underline = s.Underline || top.Underline
	s.Strike = s.Strike || top.Strike
	return s
}

func (s Style) Cell() tcell.Style {
	return tcell.StyleDefault.
		Foreground(s.Fg).
		Background(s.Bg).
		Bold(s.Bold).
		Italic(s.Italic).
		Underline(s.Underline).
		StrikeThrough(s.Strike)
}

// Theme は画面全体の基本色。
type Theme struct {
	Name       string
	Background tcell.Color
	Surface    tcell.Color // サイドバー・ダイアログ
	Panel      tcell.Color // ツールバー・ステータスバー
	Foreground tcell.Color
	Primary    tcell.Color
	Secondary  tcell.Color
	Accent     tcell.Color
	Success    tcell.Color
	Warning    tcell.Color
	Error      tcell.Color
}

// Muted は背景と文字色の中間。記号や補助情報に使う。
func (t Theme) Muted() tcell.Color { return Blend(t.Background, t.Foreground, 0.5) }

// CodeBackground はコードを地の文から少し浮かせる背景。
func (t Theme) CodeBackground() tcell.Color { return Blend(t.Background, t.Foreground, 0.07) }

// Selection は選択範囲の背景。
func (t Theme) Selection() tcell.Color { return Blend(t.Background, t.Primary, 0.35) }

func (t Theme) Text() Style  { return Style{Fg: t.Foreground, Bg: t.Background} }
func (t Theme) Faint() Style { return Style{Fg: t.Muted()} }

// Blend は a から b へ ratio だけ寄せた色。
func Blend(a, b tcell.Color, ratio float64) tcell.Color {
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	mix := func(x, y int32) int32 { return x + int32(float64(y-x)*ratio) }
	return tcell.NewRGBColor(mix(ar, br), mix(ag, bg), mix(ab, bb))
}

func hex(s string) tcell.Color {
	var r, g, b int32
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		panic(fmt.Sprintf("theme: bad color %q", s))
	}
	return tcell.NewRGBColor(r, g, b)
}

func define(name, background, surface, panel, foreground, primary, secondary, accent, success, warning, errorColor string) Theme {
	return Theme{
		Name:       name,
		Background: hex(background),
		Surface:    hex(surface),
		Panel:      hex(panel),
		Foreground: hex(foreground),
		Primary:    hex(primary),
		Secondary:  hex(secondary),
		Accent:     hex(accent),
		Success:    hex(success),
		Warning:    hex(warning),
		Error:      hex(errorColor),
	}
}

// All は選べるテーマ。先頭が既定。
var All = []Theme{
	define("tokyo-night", "#1a1b26", "#1f2335", "#292e42", "#c0caf5", "#7aa2f7", "#bb9af7", "#ff9e64", "#9ece6a", "#e0af68", "#f7768e"),
	define("catppuccin", "#1e1e2e", "#181825", "#313244", "#cdd6f4", "#89b4fa", "#cba6f7", "#fab387", "#a6e3a1", "#f9e2af", "#f38ba8"),
	define("nord", "#2e3440", "#3b4252", "#434c5e", "#d8dee9", "#88c0d0", "#81a1c1", "#b48ead", "#a3be8c", "#ebcb8b", "#bf616a"),
	define("gruvbox", "#282828", "#32302f", "#3c3836", "#ebdbb2", "#83a598", "#d3869b", "#fe8019", "#b8bb26", "#fabd2f", "#fb4934"),
	define("dracula", "#282a36", "#21222c", "#343746", "#f8f8f2", "#bd93f9", "#8be9fd", "#ff79c6", "#50fa7b", "#ffb86c", "#ff5555"),
	define("paper", "#fbfaf7", "#f2f0ea", "#e6e2d8", "#2b2b2b", "#2457c5", "#7a3eb1", "#c2410c", "#15803d", "#a16207", "#b91c1c"),
	define("solarized-light", "#fdf6e3", "#eee8d5", "#e4ddc8", "#586e75", "#268bd2", "#6c71c4", "#cb4b16", "#859900", "#b58900", "#dc322f"),
}

// Named は名前でテーマを探す。見つからなければ既定のテーマ。
func Named(name string) Theme {
	for _, t := range All {
		if t.Name == name {
			return t
		}
	}
	return All[0]
}
