//go:build windows

package discovery

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func setReusePort(c syscall.RawConn) error {
	var opErr error
	err := c.Control(func(fd uintptr) {
		if err := windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1); err != nil {
			opErr = err
			return
		}
	})
	if err != nil {
		return err
	}
	return opErr
}
