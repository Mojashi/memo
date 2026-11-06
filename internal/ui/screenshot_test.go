package ui

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/Mojashi/memo/internal/textbuf"
)

// TestScreenshot は README 用のスクリーンショットを SVG で書き出す。
//
//	MEMO_SCREENSHOT=$PWD/docs/screenshot.svg go test ./internal/ui -run Screenshot
func TestScreenshot(t *testing.T) {
	out := os.Getenv("MEMO_SCREENSHOT")
	if out == "" {
		t.Skip("MEMO_SCREENSHOT が無いので書き出さない")
	}
	h := start(t, map[string]string{
		"notes/週次ノート.md":         screenshotNote,
		"notes/ideas/アイデア.md":    "# アイデア\n",
		"notes/ideas/読書メモ.md":    "# 読書メモ\n",
		"notes/journal/11-05.md": "# 11/05\n",
		"notes/tools/deploy.sh":  "#!/bin/sh\n",
	}, "notes/週次ノート.md")
	t.Setenv("HOME", filepath.Dir(h.app.vault.Root)) // 一覧の上に ~/notes と出す
	h.screen.SetSize(118, 40)
	h.app.sidebar.expanded[filepath.Join(h.app.vault.Root, "ideas")] = true
	h.app.sidebar.reload()
	h.app.editor.MoveTo(textbuf.Pos{Row: 8, Col: 20}, false)
	h.app.draw()
	if err := os.WriteFile(out, []byte(svgOf(h)), 0o644); err != nil {
		t.Fatal(err)
	}
}

const screenshotNote = "# 週次ノート\n\n" +
	"## 今週やること\n\n" +
	"- [x] 講演資料の構成を決める\n" +
	"- [ ] デモ動画を撮る **金曜まで**\n" +
	"- [ ] [申込フォーム](https://example.com) を共有\n\n" +
	"カーソルのある行だけ **記号** が見え、ほかの行は *完成形* で表示されます。\n\n" +
	"## 進捗\n\n" +
	"| 項目 | 状態 | メモ |\n| --- | --- | --- |\n| 資料 | 8割 | 図をあと2枚 |\n| デモ | 未着手 | 環境は用意済み |\n\n" +
	"```mermaid\ngraph LR\n  A[構成] --> B[資料] --> C[リハーサル]\n```\n\n" +
	"> 引用もこのとおり。`code` もそのまま。\n"

func hexColor(c tcell.Color, fallback string) string {
	if c == tcell.ColorDefault {
		return fallback
	}
	return fmt.Sprintf("#%06x", c.Hex())
}

func svgOf(h *harness) string {
	const cw, ch = 9.0, 20.0
	cells, w, height := h.screen.GetContents()
	t := h.app.theme
	fg0, bg0 := hexColor(t.Foreground, "#fff"), hexColor(t.Background, "#000")
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" font-family="Menlo, 'Hiragino Sans', monospace" font-size="15">`, cw*float64(w)+24, ch*float64(height)+24)
	fmt.Fprintf(&b, `<rect width="100%%" height="100%%" rx="10" fill="%s"/><g transform="translate(12 12)">`, bg0)
	for y := range height {
		for x := 0; x < w; x++ {
			c := cells[y*w+x]
			fg, bg, attr := c.Style.Decompose()
			width := 1
			if len(c.Runes) > 0 {
				width = max(1, textWidth(string(c.Runes)))
			}
			if bgc := hexColor(bg, bg0); bgc != bg0 {
				fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`, float64(x)*cw, float64(y)*ch, float64(width)*cw+0.5, ch+0.5, bgc)
			}
			text := strings.TrimSpace(string(c.Runes))
			if text != "" {
				style := ""
				if attr&tcell.AttrBold != 0 {
					style += ` font-weight="bold"`
				}
				if attr&tcell.AttrItalic != 0 {
					style += ` font-style="italic"`
				}
				decoration := []string{}
				if c.Style.GetUnderlineStyle() != tcell.UnderlineStyleNone {
					decoration = append(decoration, "underline")
				}
				if attr&tcell.AttrStrikeThrough != 0 {
					decoration = append(decoration, "line-through")
				}
				if len(decoration) > 0 {
					style += fmt.Sprintf(` text-decoration="%s"`, strings.Join(decoration, " "))
				}
				fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" fill="%s"%s textLength="%.1f" lengthAdjust="spacingAndGlyphs">%s</text>`,
					float64(x)*cw, float64(y)*ch+15, hexColor(fg, fg0), style, float64(width)*cw, html.EscapeString(text))
			}
			x += width - 1
		}
	}
	if x, y, visible := h.screen.GetCursor(); visible {
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="2" height="%.1f" fill="%s"/>`, float64(x)*cw, float64(y)*ch+1, ch-2, fg0)
	}
	b.WriteString("</g></svg>\n")
	return b.String()
}
