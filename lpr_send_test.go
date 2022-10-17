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
