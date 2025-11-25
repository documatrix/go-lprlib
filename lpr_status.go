package lprlib

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"time"
)

// GetStatus Reads the status of the given queue on the given host (and port).
// The timeout parameter specifies the maximum time to wait for the connection
// and for each read/write operation. If timeout is 0, a default of 2 seconds
// is used. The long parameter specifies whether to request a long listing
// (true) or a short listing (false). The ignoreForcefulClose parameter controls
// if the read-status should ignore forceful connection closures by the server (which
// sometimes happens).
func GetStatus(hostname string, port uint16, queue string, long bool, timeout time.Duration, ignoreForcefulClose bool) (string, error) {
	// Set default Port
	if port == 0 {
		port = 515
	}

	// Set default Queue
	if queue == "" {
		queue = "raw"
	}

	var code byte
	if long {
		code = byte(4)
	} else {
		code = byte(3)
	}

	logDebugf("Checking status of LPR printer %s, port %d, queue %s, long flag %v and timeout %v", hostname, port, queue, long, timeout)

	// Set default time.Duration
	var timeoutDuration time.Duration
	if timeout == 0 {
		timeoutDuration = time.Second * 2
	} else {
		timeoutDuration = timeout
	}

	/* Connect to Server! */
	ipstring := net.JoinHostPort(hostname, fmt.Sprint(port))
	logDebugf("Connecting to printer %s using timeout %d", ipstring, timeoutDuration)
	socket, err := net.DialTimeout("tcp", ipstring, timeoutDuration)
	if err != nil {
		return "", &LprError{"Can't reach printer: " + err.Error()}
	}

	defer socket.Close()

	// Command:
	/**
		*   Send queue state
		*
		*   +----+-------+----+------+----+
		*   | 03 | Queue | SP | List | LF |
		*   +----+-------+----+------+----+
		*   Command code - 3 (4 for long)
		*   Operand 1 - Printer queue name
		*   Other operands - User names or job numbers
		*
		*   If the user names or job numbers or both are supplied then only those
		*   jobs for those users or with those numbers will be sent.
		*
		*   The response is an ASCII stream which describes the printer queue.
		*   The stream continues until the connection closes.  Ends of lines are
		*   indicated with ASCII LF control characters.  The lines may also
		*   contain ASCII HT control characters.
	**/

	socket.SetWriteDeadline(time.Now().Add(timeoutDuration))
	// List items are not used because they only filter the output
	command := fmt.Sprintf("%c%s\n", code, queue)
	logDebugf("Sending command %s to printer", command)
	_, err = socket.Write([]byte(command))
	if err != nil {
		return "", &LprError{"Can't write to printer: " + err.Error()}
	}

	buffer := make([]byte, 4096)
	ret := ""
	var len int
	for {
		socket.SetReadDeadline(time.Now().Add(timeoutDuration))
		len, err = socket.Read(buffer)
		ret += string(buffer[:len])
		logDebugf("Intermediate result: %s", ret)
		if err != nil {
			if err == io.EOF {
				break
			} else {
				if ignoreForcefulClose {
					opErr := &net.OpError{}

					if errors.As(err, &opErr) {
						// Check for connection reset errors at the syscall level
						// This works cross-platform: ECONNRESET on Unix-like systems,
						// WSAECONNRESET on Windows
						if errors.Is(opErr, syscall.ECONNRESET) {
							logDebugf("Ignoring forceful connection closure by server")
							break
						}
					}
				}

				return "", &LprError{"Error while reading status: " + err.Error()}
			}
		}
	}

	logDebugf("Final result: %s", ret)
	return ret, nil
}
