package lprlib

import (
	"errors"
	"net"
	"syscall"
)

func isConnResetErr(err *net.OpError) bool {
	return errors.Is(err.Err, syscall.ECONNRESET) || errors.Is(err.Err, syscall.WSAECONNRESET)
}
