package lprlib

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDNS(t *testing.T) {
	fmt.Println("---Start TestDNS---")
	defer fmt.Println("--- End Test---")

	ip, err := GetIP("hq.documatrix.com")
	if err != nil {
		fmt.Println(err.Error())
		t.Fail()
	} else {
		fmt.Println(ip.IP, err)
	}
}

func TestSend(t *testing.T) {
	SetDebugLogger(log.Print)

	port := uint16(2345)

	text := "Text for the file"
	file, err := generateTempFile("", "", text)
	require.Nil(t, err)

	var lprd LprDaemon

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = Send(file, "127.0.0.1", port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	conn := <-lprd.FinishedConnections()
	out, err := ioutil.ReadFile(conn.SaveName)
	require.Nil(t, err)
	os.Remove(conn.SaveName)
	require.Equal(t, conn.UserIdentification, "TestUser")
	require.Equal(t, text, string(out))

	time.Sleep(time.Second)

	lprd.Close()

	time.Sleep(time.Second)

	err = os.Remove(file)
	require.Nil(t, err)
}

func TestSendWithExternalIDGeneration(t *testing.T) {
	SetDebugLogger(log.Print)

	port := uint16(2345)

	text := "Text for the file"
	file, err := generateTempFile("", "", text)
	require.Nil(t, err)
	defer os.Remove(file)

	var lprd LprDaemon

	nextExternalID := uint64(0)
	// set lprd callback function
	lprd.GetExternalID = func() uint64 {
		nextExternalID++
		if nextExternalID == 1 {
			// make sure that the second call has to wait for the first call
			time.Sleep(500 * time.Millisecond)
		}
		return nextExternalID
	}

	err = lprd.Init(port, "")
	require.Nil(t, err)

	go func() {
		err = Send(file, "127.0.0.1", port, "raw", "TestUser1", time.Minute)
		require.Nil(t, err)
	}()

	time.Sleep(100 * time.Millisecond)

	go func() {
		err = Send(file, "127.0.0.1", port, "raw", "TestUser2", time.Minute)
		require.Nil(t, err)
	}()

	time.Sleep(100 * time.Millisecond)

	go func() {
		err = Send(file, "127.0.0.1", port, "raw", "TestUser3", time.Minute)
		require.Nil(t, err)
	}()

	externalIDs := make([]uint64, 3)

	go func() {
		for conn := range lprd.FinishedConnections() {
			out, err := ioutil.ReadFile(conn.SaveName)
			require.Nil(t, err)
			os.Remove(conn.SaveName)
			require.Equal(t, text, string(out))
			switch conn.UserIdentification {
			case "TestUser1":
				externalIDs[0] = conn.ExternalID
			case "TestUser2":
				externalIDs[1] = conn.ExternalID
			case "TestUser3":
				externalIDs[2] = conn.ExternalID
			default:
				panic(fmt.Errorf("Invalid UserIdentification: %q", conn.UserIdentification))
			}
		}
	}()

	// wait for lprd.FinishedConnections
	time.Sleep(time.Second)

	// check order of generated external ids
	require.EqualValues(t, []uint64{1, 2, 3}, externalIDs)

	lprd.Close()
}
