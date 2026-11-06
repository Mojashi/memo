package ui

import (
	"strconv"
	"strings"

	"github.com/Mojashi/memo/internal/live"
	"github.com/Mojashi/memo/internal/theme"
)

func textWidth(s string) int { return live.Width(s) }

// truncate は表示幅 width に収まるよう末尾を「…」で切る。
func truncate(s string, width int) string {
	if textWidth(s) <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := textWidth(string(r))
		if used+w > width-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
}

// highlight は text の中の query (大文字小文字は区別しない) を目立たせた断片に分ける。
func highlight(text, query string, mark theme.Style) []segment {
	lowerText, lowerQuery := []rune(strings.ToLower(text)), []rune(strings.ToLower(query))
	runes := []rune(text)
	if len(lowerQuery) == 0 || len(lowerText) != len(runes) {
		return []segment{{text, theme.Style{}}}
	}
	var segments []segment
	start := 0
	for i := 0; i+len(lowerQuery) <= len(lowerText); {
		if string(lowerText[i:i+len(lowerQuery)]) == string(lowerQuery) {
			segments = append(segments, segment{string(runes[start:i]), theme.Style{}}, segment{string(runes[i : i+len(lowerQuery)]), mark})
			i += len(lowerQuery)
			start = i
			continue
		}
		i++
	}
	return append(segments, segment{string(runes[start:]), theme.Style{}})
}

// comma は 3 桁ごとにカンマを入れる。
func comma(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

const welcome = `# ようこそ memo へ ✎

ターミナルで書く、リッチな Markdown ノート。

## 使い方

- **Ctrl+N** 新しいノート / **Ctrl+S** 保存 / **Ctrl+O** ファイルを開く / **Ctrl+F** 全ノート検索
- **Ctrl+P** コマンドパレット (テーマ変更もここから)
- **F2** サイドバー / **Ctrl+E** サイドバーへ移動 / **F4** 集中モード / **F6** 記号を全部見せる
- **Ctrl+B** 太字 / **Ctrl+L** リンク / **Ctrl+T** チェック切替 / **Ctrl+Z** 元に戻す
- **Option+←→** 単語ごとに移動 / 上のツールバーはマウスでクリックできます

## リストは Enter で続く

- [x] チェックリスト (☐ をクリックしても切り替わる)
- [ ] Enter を押すと次の項目が出る

## カーソルのある行だけ記号が見える

**太字** や *斜体*、` + "`code`" + `、[リンク](https://example.com) は、カーソルを離すと完成形で表示されます。

| 操作 | キー | 説明 |
| --- | --- | --- |
| 保存 | Ctrl+S | 名前が無ければ付けて保存。名前があれば入力が落ち着いたときにも自動で保存します |
| 検索 | Ctrl+F | ファイル名と本文をまとめて探します |

` + "```go" + `
fmt.Println("コードブロックも色付きで")
` + "```" + `

> このバッファは名前がないので、Ctrl+S で名前を付けて保存できます。
`
