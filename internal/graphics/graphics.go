// Package graphics は kitty の画像表示 (graphics protocol) で、ターミナルに画像を出す。
//
// 画像は「Unicode プレースホルダー」で置く。画像そのものは一度だけ端末に送り、
// 画面には U+10EEEE という普通の文字を並べる。文字なので tcell や tmux が他の文字と同じように
// 描き直し・移動してくれ、画像はその文字の位置に出る。tmux の中では送る部分だけを素通しさせる。
package graphics

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Placeholder は画像を置くマスに書く文字。
const Placeholder = '\U0010EEEE'

// Terminal は画像を出せる端末。
type Terminal struct {
	tmux bool // tmux の中なら、送る命令を tmux に素通しさせる
}

// Detect は今の端末で画像を出せるか調べる。出せなければ理由を返す。
func Detect() (Terminal, error) {
	if os.Getenv("TMUX") == "" {
		if os.Getenv("TERM") == "xterm-kitty" || os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("TERM_PROGRAM") == "ghostty" {
			return Terminal{}, nil
		}
		return Terminal{}, errors.New("この端末は画像を出せません。\nkitty か Ghostty で使えます")
	}
	outer, err := exec.Command("tmux", "display", "-p", "#{client_termtype}").Output()
	if err != nil {
		return Terminal{}, fmt.Errorf("tmux の外側の端末を調べられません: %w", err)
	}
	if name := strings.ToLower(string(outer)); !strings.Contains(name, "kitty") && !strings.Contains(name, "ghostty") {
		return Terminal{}, fmt.Errorf("tmux の外側の端末 (%s) は画像を出せません", strings.TrimSpace(string(outer)))
	}
	passthrough, err := exec.Command("tmux", "show", "-Apv", "allow-passthrough").Output()
	if err != nil {
		return Terminal{}, fmt.Errorf("tmux の allow-passthrough を調べられません: %w", err)
	}
	if strings.TrimSpace(string(passthrough)) == "off" {
		return Terminal{}, errors.New("tmux が画像を通していません。\n~/.tmux.conf に次の1行を足して読み込み直すと出せます:\n\n  set -g allow-passthrough on")
	}
	return Terminal{tmux: true}, nil
}

// Show は path の PNG を画像 id として端末に送り、cols×rows マスに収まるように置く命令。
// 画面に出すのは、その範囲に Cell の文字を並べたとき。同じ id で送り直すと絵が差し替わる。
func (t Terminal) Show(id int, path string, cols, rows int) string {
	file := base64.StdEncoding.EncodeToString([]byte(path))
	return t.wrap(fmt.Sprintf("\x1b_Ga=T,t=f,f=100,U=1,q=2,i=%d,c=%d,r=%d;%s\x1b\\", id, cols, rows, file))
}

// Delete は画像 id を端末から消す命令。
func (t Terminal) Delete(id int) string {
	return t.wrap(fmt.Sprintf("\x1b_Ga=d,d=I,q=2,i=%d\x1b\\", id))
}

func (t Terminal) wrap(command string) string {
	if !t.tmux {
		return command
	}
	return "\x1bPtmux;" + strings.ReplaceAll(command, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// Cell は画像の (row, col) のマスに書く結合文字。行の先頭だけ番号を付け、残りは左のマスから続きと分かる。
// 文字色を画像 id (256 色の番号) にして描くこと。
func Cell(row, col int) []rune {
	if col > 0 || row >= len(diacritics) {
		return nil
	}
	return []rune{diacritics[row], diacritics[0]}
}

// MaxRows は置ける画像の行数の上限。
func MaxRows() int { return len(diacritics) }
