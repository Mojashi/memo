// Package diagram は Mermaid の図を文字で描く (mermaid-ascii を使う)。
//
// 対応しているのは flowchart / graph・sequenceDiagram・erDiagram。それ以外はエラーになる。
package diagram

import (
	"fmt"
	"io"
	"strings"

	mermaid "github.com/AlexanderGrooff/mermaid-ascii/pkg/diagram"
	"github.com/AlexanderGrooff/mermaid-ascii/pkg/render"
	"github.com/mattn/go-runewidth"
	"github.com/sirupsen/logrus"
)

func init() {
	// 描画中のログが端末に出ると画面が崩れるので捨てる
	logrus.SetOutput(io.Discard)
	// ☐ や罫線などの曖昧幅の文字は 1 マス (kitty と同じ)。日本語ロケールでも 2 マスにしない
	runewidth.DefaultCondition.EastAsianWidth = false
}

// Render は Mermaid のソースを width 桁に収まるよう描き、行ごとに返す。
func Render(source string, width int) (lines []string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("図を描けません: %v", r)
		}
	}()
	config := mermaid.DefaultConfig()
	config.MaxWidth = width
	out, err := render.RenderDiagram(source, config)
	if err != nil {
		return nil, err
	}
	out = strings.TrimRight(out, "\n")
	lines = strings.Split(out, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return lines, nil
}
