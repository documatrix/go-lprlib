package lprlib

import (
	"log"
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

	status, err := GetStatus("127.0.0.1", port, rawQueue, false, 2*time.Second)
	require.Nil(t, err)
	require.NotEmpty(t, status)

	lprd.GetQueueState = getShortQueueState

	status, err = GetStatus("127.0.0.1", port, rawQueue, false, 2*time.Second)
	require.Nil(t, err)
	require.Equal(t, shortQueueState, status)

	lprd.GetQueueState = getLongQueueState

	status, err = GetStatus("127.0.0.1", port, rawQueue, true, 2*time.Second)
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
