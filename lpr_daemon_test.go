package lprlib

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDaemonSingleConnection(t *testing.T) {
	SetDebugLogger(log.Print)
	var err error
	var out []byte
	var name string

	port := uint16(2345)

	text := "Text for the file"
	name, err = generateTempFile("", "", text)
	if err != nil {
		fmt.Println(err.Error())
	}

	fmt.Println("Tempfile:", name)

	var lprd LprDaemon
	var lprs LprSend

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = lprs.Init("127.0.0.1", name, port, "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration("raw")
	require.Nil(t, err)

	err = lprs.SendFile()
	require.Nil(t, err)

	time.Sleep(1 * time.Second)

	allcon := lprd.GetConnections()

	for _, iv := range allcon {
		out, err = ioutil.ReadFile(iv.SaveName)
		if err != nil {
			t.Error(err)
		} else {
			os.Remove(iv.SaveName)
			if text != string(out) {
				t.Fail()
			}
		}
	}

	time.Sleep(time.Second)

	lprd.Close()

	time.Sleep(time.Second)

	os.Remove(name)
}

func TestDaemonLargeFileConnection(t *testing.T) {
	SetDebugLogger(log.Print)

	var err error
	var out []byte
	var name string

	port := uint16(2346)

	sbyte := [10]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	var fbyte [10000000]byte

	for byteCount := uint32(0); byteCount < 100000; byteCount++ {
		fbyte[byteCount] = sbyte[byteCount%10]
	}

	sfbyte := fbyte[:]

	text := string(sfbyte)
	name, err = generateTempFile("", "", text)
	if err != nil {
		fmt.Println(err.Error())
	}

	fmt.Println("Tempfile:", name)

	var lprd LprDaemon
	var lprs LprSend

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = lprs.Init("127.0.0.1", name, port, "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration("raw")
	require.Nil(t, err)

	err = lprs.SendFile()
	require.Nil(t, err)

	time.Sleep(5 * time.Second)

	allcon := lprd.GetConnections()

	for _, iv := range allcon {
		out, err = ioutil.ReadFile(iv.SaveName)
		if err != nil {
			t.Error(err)
		} else {
			os.Remove(iv.SaveName)
			if text != string(out) {
				t.Fail()
			}
		}
	}

	time.Sleep(time.Second)

	lprd.Close()

	time.Sleep(time.Second)

	os.Remove(name)
}

func TestDaemonMultipleConnection(t *testing.T) {
	SetDebugLogger(log.Print)

	var err error
	var fcount int
	var out []byte
	var fileName1 string
	var fileName2 string
	var fileName3 string

	text1 := "Text for the file"
	text2 := "Text for next LprSend"
	text3 := "Text for the last LprSend"

	port := uint16(2347)

	fileName1, err = generateTempFile("", "", text1)
	require.Nil(t, err)
	fmt.Println("Tempfile:", fileName1)

	fileName2, err = generateTempFile("", "", text2)
	require.Nil(t, err)
	fmt.Println("Tempfile:", fileName2)

	fileName3, err = generateTempFile("", "", text3)
	require.Nil(t, err)
	fmt.Println("Tempfile:", fileName3)

	var lprd LprDaemon
	var lprs LprSend
	var lprs2 LprSend
	var lprs3 LprSend

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = lprs.Init("127.0.0.1", fileName1, port, "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs2.Init("127.0.0.1", fileName2, port, "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration("raw")
	require.Nil(t, err)

	err = lprs2.SendConfiguration("raw")
	require.Nil(t, err)

	err = lprs.SendFile()
	require.Nil(t, err)
	err = lprs2.SendFile()
	require.Nil(t, err)

	err = lprs3.Init("127.0.0.1", fileName3, port, "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs3.SendConfiguration("raw")
	require.Nil(t, err)

	err = lprs3.SendFile()
	require.Nil(t, err)

	time.Sleep(2 * time.Second)

	allcon := lprd.GetConnections()

	for _, iv := range allcon {
		out, err = ioutil.ReadFile(iv.SaveName)
		if err != nil {
			t.Error(err)
		} else {
			os.Remove(iv.SaveName)
			require.Equal(t, iv.UserIdentification, "TestUser")
			switch string(out) {
			case text1:
				fcount |= 0x1
			case text2:
				fcount |= 0x2
			case text3:
				fcount |= 0x4
			default:
				t.Fail()
			}
		}
	}

	if fcount != 0x7 {
		fmt.Println("fcount:", fcount)
		t.Fail()
	}

	time.Sleep(time.Second)

	lprd.Close()

	time.Sleep(time.Second)

	os.Remove(fileName1)
	os.Remove(fileName2)
	os.Remove(fileName3)

}

func generateTempFile(dir, prefix, text string) (string, error) {
	var err error
	var file *os.File

	file, err = ioutil.TempFile(dir, prefix)
	if err != nil {
		return "", err
	}

	_, err = fmt.Fprint(file, text)
	if err != nil {
		return "", err
	}

	file.Close()

	return file.Name(), nil
}

func TestDaemonTimeout(t *testing.T) {
	port := uint16(2346)
	sbyte := [10]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	var fbyte [10000000]byte
	for byteCount := uint32(0); byteCount < 100000; byteCount++ {
		fbyte[byteCount] = sbyte[byteCount%10]
	}

	sfbyte := fbyte[:]
	text := string(sfbyte)
	name, err := generateTempFile("", "", text)
	defer os.Remove(name)
	if err != nil {
		fmt.Println(err.Error())
	}

	fmt.Println("Tempfile:", name)

	var lprd LprDaemon
	var lprs LprSend

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = lprs.Init("127.0.0.1", name, port, "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration("raw")
	require.Nil(t, err)

	lprs.Timeout = 0
	err = lprs.SendFile()
	require.NotNil(t, err)
	require.True(t, strings.Contains(err.Error(), "timeout"))
}
