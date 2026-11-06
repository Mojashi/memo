// memo はターミナルで動くリッチな Markdown ノートエディタ。
//
//	memo            今のフォルダを開く
//	memo <folder>   フォルダを開く
//	memo <file>     ファイルを開く (その親フォルダが一覧に出る)
package main

import (
	"fmt"
	"os"

	"github.com/Mojashi/memo/internal/ui"
)

func main() {
	target := ""
	if len(os.Args) > 1 {
		target = os.Args[1]
	}
	if err := ui.Run(target); err != nil {
		fmt.Fprintln(os.Stderr, "memo:", err)
		os.Exit(1)
	}
}
