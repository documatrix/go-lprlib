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

	err = Send(file, "127.0.0.1", port, "raw", "TestUser")
	require.Nil(t, err)

	time.Sleep(1 * time.Second)

	allcon := lprd.GetConnections()

	for _, iv := range allcon {
		out, err := ioutil.ReadFile(iv.SaveName)
		require.Nil(t, err)
		os.Remove(iv.SaveName)
		require.Equal(t, text, string(out))
	}

	time.Sleep(time.Second)

	lprd.Close()

	time.Sleep(time.Second)

	err = os.Remove(file)
	require.Nil(t, err)
}
