package ui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gdamore/tcell/v2"
)

// clipboard はシステムのクリップボード。macOS なら pbcopy/pbpaste、
// 無ければ端末の OSC 52 でコピーし、貼り付けはアプリ内で覚えたものを使う。
type clipboard struct {
	screen tcell.Screen
	last   string
}

func (c *clipboard) Copy(text string) {
	c.last = text
	if path, err := exec.LookPath("pbcopy"); err == nil {
		cmd := exec.Command(path)
		cmd.Stdin = strings.NewReader(text)
		if cmd.Run() == nil {
			return
		}
	}
	c.screen.SetClipboard([]byte(text))
}

func (c *clipboard) Paste() string {
	if path, err := exec.LookPath("pbpaste"); err == nil {
		if out, err := exec.Command(path).Output(); err == nil {
			return string(out)
		}
	}
	return c.last
}

// settings は次回の起動にも残す設定。
type settings struct {
	Theme string `json:"theme,omitempty"`
}

func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "memo", "settings.json"), nil
}

func loadSettings() (settings, error) {
	var s settings
	path, err := settingsPath()
	if err != nil {
		return s, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	return s, json.Unmarshal(data, &s)
}

func saveSettings(s settings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
