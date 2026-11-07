//go:build windows

package slides

import (
	"errors"
	"os/exec"
)

func lockExclusive(string) (func(), error) {
	return nil, errors.New("Windows ではスライドの画像を出せません")
}

func detach(*exec.Cmd) {}
