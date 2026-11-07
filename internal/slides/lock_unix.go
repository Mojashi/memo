//go:build unix

package slides

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// lockExclusive は描画サーバーが1つだけ動くようにロックを取る。もう動いていれば ErrServerRunning。
func lockExclusive(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrServerRunning
		}
		return nil, err
	}
	return func() { f.Close() }, nil
}

// detach は描画サーバーを memo から切り離して起動する (memo を閉じても残る)。
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
