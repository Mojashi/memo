// memo はターミナルで動くリッチな Markdown ノートエディタ。
//
//	memo            今のフォルダを開く
//	memo <folder>   フォルダを開く
//	memo <file>     ファイルを開く (その親フォルダが一覧に出る)
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/Mojashi/memo/internal/slides"
	"github.com/Mojashi/memo/internal/ui"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == slides.ServerFlag {
		serveSlides()
		return
	}
	target := ""
	if len(os.Args) > 1 {
		target = os.Args[1]
	}
	if err := ui.Run(target); err != nil {
		fmt.Fprintln(os.Stderr, "memo:", err)
		os.Exit(1)
	}
}

// serveSlides は Marp のスライドを描く描画サーバーとして動く (memo が裏で起動する)。
func serveSlides() {
	paths, err := slides.DefaultPaths()
	if err == nil {
		err = slides.Serve(paths)
	}
	if err != nil && !errors.Is(err, slides.ErrServerRunning) {
		fmt.Fprintln(os.Stderr, "memo slide server:", err)
		os.Exit(1)
	}
}
