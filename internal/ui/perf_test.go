package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

func TestLargeNoteStaysResponsive(t *testing.T) {
	section := "## 見出し\n\n- [ ] やること **太字** と `code` と [リンク](https://example.com)\n本文の段落。とても長い日本語の文章がここに続きます。" + strings.Repeat("あ", 80) + "\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n```go\nfmt.Println(1)\n```\n\n"
	body := strings.Repeat(section, 300)
	began := time.Now()
	h := start(t, map[string]string{"big.md": body}, "big.md")
	opened := time.Since(began)
	began = time.Now()
	const n = 200
	for i := range n {
		h.app.handleKey(tcell.NewEventKey(tcell.KeyRune, 'あ', 0))
		if i%20 == 0 {
			h.app.handleKey(tcell.NewEventKey(tcell.KeyDown, 0, 0))
		}
		h.app.draw()
	}
	perKey := time.Since(began) / n
	t.Logf("%d 行: 開く %v / 1キー %v", strings.Count(body, "\n"), opened, perKey)
	if perKey > 30*time.Millisecond {
		t.Errorf("1キーあたり %v は遅すぎます", perKey)
	}
}
