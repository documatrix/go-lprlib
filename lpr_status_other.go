//go:build !windows
// +build !windows

package lprlib

import (
	"errors"
	"net"
	"syscall"
)

func isConnResetErr(err *net.OpError) bool {
	return errors.Is(err.Err, syscall.ECONNRESET)
}
