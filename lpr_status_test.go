package lprlib

import (
	"log"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGetStatus(t *testing.T) {
	SetDebugLogger(log.Print)

	rawQueue := "raw"
	port := uint16(2345)

	shortQueueState := "shortQueueState\n"
	getShortQueueState := func(queue string, list string, long bool) string {
		require.Equal(t, rawQueue, queue)
		require.Equal(t, false, long)
		return shortQueueState
	}

	longQueueState := "longQueueState\n"
	getLongQueueState := func(queue string, list string, long bool) string {
		require.Equal(t, rawQueue, queue)
		require.Equal(t, true, long)
		return longQueueState
	}

	var lprd LprDaemon
	err := lprd.Init(port, "")
	require.Nil(t, err)

	time.Sleep(1 * time.Second)

	status, err := GetStatus("127.0.0.1", port, rawQueue, false, 2*time.Second, false)
	require.Nil(t, err)
	require.NotEmpty(t, status)

	lprd.GetQueueState = getShortQueueState

	status, err = GetStatus("127.0.0.1", port, rawQueue, false, 2*time.Second, false)
	require.Nil(t, err)
	require.Equal(t, shortQueueState, status)

	lprd.GetQueueState = getLongQueueState

	status, err = GetStatus("127.0.0.1", port, rawQueue, true, 2*time.Second, false)
	require.Nil(t, err)
	require.Equal(t, longQueueState, status)

	i := 0
	for conn := range lprd.FinishedConnections() {
		require.Equal(t, End, conn.Status)
		require.Empty(t, conn.SaveName)

		i++
		if i >= 3 {
			break
		}
	}

	lprd.Close()
}

func TestGetStatus_ServerClosesImmediatelyAfterCommand(t *testing.T) {
	listener, err := net.Listen("tcp", net.JoinHostPort("localhost", "0"))
	require.NoError(t, err)

	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		require.NoError(t, err)

		defer conn.Close()

		// Read command byte
		buf := make([]byte, 1024)
		_, err = conn.Read(buf)
		require.NoError(t, err)

		// Close connection immediately
		err = conn.(*net.TCPConn).SetLinger(0)
		require.NoError(t, err)
	}()

	status, err := GetStatus("localhost", uint16(listener.Addr().(*net.TCPAddr).Port), "raw", false, 2*time.Second, true)
	require.NoError(t, err)
	require.Empty(t, status)

	_, err = GetStatus("localhost", uint16(listener.Addr().(*net.TCPAddr).Port), "raw", false, 2*time.Second, false)
	require.Error(t, err)
}
