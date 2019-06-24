package lprlib

import (
	"fmt"
	"io"
	"net"
	"time"
)

// GetStatus Reads the Status from the printer
func GetStatus(hostname string, port uint16, queue string, long bool, timeout time.Duration) (string, error) {

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

	// Set default time.Duration
	var timeoutDuration time.Duration
	if timeout == 0 {
		timeoutDuration = time.Second * 2
	} else {
		timeoutDuration = timeout
	}

	/* Connect to Server! */
	ipstring := net.JoinHostPort(hostname, fmt.Sprint(port))
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
	_, err = socket.Write([]byte(fmt.Sprintf("%c%s\n", code, queue)))
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
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return "", &LprError{"Error while reading status: " + err.Error()}
			}
		}
	}

	return ret, nil
}
