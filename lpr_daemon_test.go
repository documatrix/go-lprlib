package lprlib

import (
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
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

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = Send(name, "127.0.0.1", port, "r\xE4w", "TestÜser", time.Minute)
	require.Nil(t, err)

	conn := <-lprd.FinishedConnections()

	require.Equal(t, "räw", conn.PrqName)
	require.Equal(t, "TestÜser", conn.UserIdentification)
	fi, err := os.Stat(conn.SaveName)
	require.Nil(t, err)
	require.Equal(t, fs.FileMode(0600), fi.Mode().Perm())

	out, err = ioutil.ReadFile(conn.SaveName)
	if err != nil {
		t.Error(err)
	} else {
		os.Remove(conn.SaveName)
		if text != string(out) {
			t.Fail()
		}
	}

	time.Sleep(time.Second)

	lprd.Close()

	time.Sleep(time.Second)

	os.Remove(name)

}

func TestDaemonChangeFilePermission(t *testing.T) {
	var err error
	var out []byte
	var name string

	port := uint16(2345)

	text := "Text for the file"
	name, err = generateTempFile("", "", text)
	require.Nil(t, err)

	fmt.Println("Tempfile:", name)

	var lprd LprDaemon

	err = lprd.Init(port, "")
	if err != nil {
		fmt.Println(err.Error())
		t.Fail()
		return
	}

	lprd.SetFileMask(0644)

	err = Send(name, "127.0.0.1", port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	conn := <-lprd.FinishedConnections()

	fi, err := os.Stat(conn.SaveName)
	require.Nil(t, err)
	require.Equal(t, fs.FileMode(0644), fi.Mode().Perm())

	out, err = ioutil.ReadFile(conn.SaveName)
	if err != nil {
		t.Error(err)
	} else {
		os.Remove(conn.SaveName)
		if text != string(out) {
			t.Fail()
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

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = Send(name, "127.0.0.1", port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	conn := <-lprd.FinishedConnections()

	out, err = ioutil.ReadFile(conn.SaveName)
	if err != nil {
		t.Error(err)
	} else {
		os.Remove(conn.SaveName)
		if text != string(out) {
			t.Fail()
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

	err = lprs.Init("127.0.0.1", fileName1, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs2.Init("127.0.0.1", fileName2, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	err = lprs2.SendConfiguration()
	require.Nil(t, err)

	err = lprs.SendFile()
	require.Nil(t, err)
	err = lprs2.SendFile()
	require.Nil(t, err)

	err = lprs3.Init("127.0.0.1", fileName3, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs3.SendConfiguration()
	require.Nil(t, err)

	err = lprs3.SendFile()
	require.Nil(t, err)

	require.Nil(t, lprs.Close())
	require.Nil(t, lprs2.Close())
	require.Nil(t, lprs3.Close())

	i := 0
	for conn := range lprd.FinishedConnections() {
		out, err = ioutil.ReadFile(conn.SaveName)
		if err != nil {
			t.Error(err)
		} else {
			os.Remove(conn.SaveName)
			require.Equal(t, conn.UserIdentification, "TestUser")
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

		i++
		if i >= 3 {
			break
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

	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	lprs.Timeout = 0
	err = lprs.SendFile()
	require.NotNil(t, err)
	require.True(t, strings.Contains(err.Error(), "timeout"))

	err = lprs.Close()
	require.Nil(t, err)

	conn := <-lprd.FinishedConnections()
	require.Empty(t, conn.SaveName)
	require.Equal(t, Error, conn.Status)
}

func TestDaemonInputFileSaveDirSetting(t *testing.T) {

	inputFileSaveDir, err := ioutil.TempDir("", "")
	require.Nil(t, err)

	var lprd LprDaemon
	defer lprd.Close()

	lprd.InputFileSaveDir = inputFileSaveDir
	port := uint16(2345)
	err = lprd.Init(port, "")
	require.Nil(t, err)

	text := "Text for the file"
	name, err := generateTempFile("", "", text)
	defer os.Remove(name)
	require.Nil(t, err)

	err = Send(name, "127.0.0.1", port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	conn := <-lprd.FinishedConnections()

	require.Equal(t, inputFileSaveDir, filepath.Dir(conn.SaveName))

	out, err := ioutil.ReadFile(conn.SaveName)
	if err != nil {
		t.Error(err)
	} else {
		os.Remove(conn.SaveName)
		if text != string(out) {
			t.Fail()
		}
	}
}

func TestDaemonClose(t *testing.T) {
	var lprd LprDaemon

	port := uint16(2345)
	err := lprd.Init(port, "")
	require.Nil(t, err)

	name, err := generateTempFile("", "", "Text for the file")
	defer os.Remove(name)
	require.Nil(t, err)

	var lprs LprSend
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	lprd.Close()

	err = lprs.SendFile()
	require.Nil(t, err)

	err = lprs.Close()
	require.Nil(t, err)

	// connection must be ok
	conn := <-lprd.FinishedConnections()
	require.Equal(t, End, conn.Status)

	// no new connection may be opened
	lprs = LprSend{}
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Minute)
	require.NotNil(t, err)
}

func TestDaemonFileSize(t *testing.T) {
	SetDebugLogger(log.Print)
	var err error
	var out []byte
	var name string

	port := uint16(2345)

	text := "Text for the file"
	name, err = generateTempFile("", "", text)
	require.Nil(t, err)

	fmt.Println("Tempfile:", name)

	var lprd LprDaemon
	var lprs LprSend

	err = lprd.Init(port, "")
	require.Nil(t, err)

	file, err := os.Open(name)
	require.Nil(t, err)
	defer file.Close()

	// send file with correct size
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	err = lprs.sendFile(file, int64(len(text)))
	require.Nil(t, err)
	err = lprs.Close()
	require.Nil(t, err)

	con := <-lprd.FinishedConnections()
	require.Equal(t, End, con.Status)
	out, err = ioutil.ReadFile(con.SaveName)
	require.Nil(t, err)
	err = os.Remove(con.SaveName)
	require.Nil(t, err)
	require.Equal(t, text, string(out))

	// send file with size 0
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	_, err = file.Seek(0, 0)
	require.Nil(t, err)
	err = lprs.sendFile(file, int64(len(text)))
	require.Nil(t, err)
	err = lprs.Close()
	require.Nil(t, err)

	con = <-lprd.FinishedConnections()
	require.Equal(t, End, con.Status)
	out, err = ioutil.ReadFile(con.SaveName)
	require.Nil(t, err)
	err = os.Remove(con.SaveName)
	require.Nil(t, err)
	require.Equal(t, text, string(out))

	// send file with too small size
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Second*2)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	_, err = file.Seek(0, 0)
	require.Nil(t, err)
	err = lprs.sendFile(file, 1)
	require.Nil(t, err)
	err = lprs.Close()
	require.Nil(t, err)

	con = <-lprd.FinishedConnections()
	require.Equal(t, End, con.Status)
	out, err = ioutil.ReadFile(con.SaveName)
	require.Nil(t, err)
	err = os.Remove(con.SaveName)
	require.Nil(t, err)
	require.Equal(t, text, string(out))

	// send file with incorrect size
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Second*2)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	_, err = file.Seek(0, 0)
	require.Nil(t, err)
	err = lprs.sendFile(file, 1024)
	require.NotNil(t, err)
	err = lprs.Close()
	require.Nil(t, err)

	con = <-lprd.FinishedConnections()
	require.Equal(t, Error, con.Status)
	err = os.Remove(con.SaveName)
	require.Nil(t, err)

	time.Sleep(time.Second)

	lprd.Close()
	os.Remove(name)
}

func TestDaemonSubCommandOrder(t *testing.T) {
	SetDebugLogger(log.Print)
	var err error
	var out []byte
	var name string

	port := uint16(2345)

	text := "Text for the file"
	name, err = generateTempFile("", "", text)
	require.Nil(t, err)

	fmt.Println("Tempfile:", name)

	var lprd LprDaemon
	var lprs LprSend

	err = lprd.Init(port, "")
	require.Nil(t, err)

	file, err := os.Open(name)
	require.Nil(t, err)
	defer file.Close()

	// send file with correct size
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.sendFile(file, int64(len(text)))
	require.Nil(t, err)
	err = lprs.SendConfiguration()
	require.Nil(t, err)
	err = lprs.Close()
	require.Nil(t, err)

	con := <-lprd.FinishedConnections()
	require.Equal(t, End, con.Status)
	out, err = ioutil.ReadFile(con.SaveName)
	require.Nil(t, err)
	err = os.Remove(con.SaveName)
	require.Nil(t, err)
	require.Equal(t, text, string(out))

	lprd.Close()
	os.Remove(name)
}

func TestCheckForUTF8StringEncoding(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		valid       bool
		result      string
		encoding    string
		expectError bool
	}{
		{
			name:        "UTF8-1",
			value:       "One2three",
			expectError: false,
			valid:       true,
			encoding:    "windows-1252",
			result:      "One2three",
		},
		{
			name:        "UTF8-2",
			value:       "ÄnsZwäDrä",
			expectError: false,
			valid:       true,
			encoding:    "windows-1252",
			result:      "ÄnsZwäDrä",
		},
		{
			name:        "UTF8-3",
			value:       "”ÄnsZ“w!äDrä",
			expectError: false,
			valid:       true,
			encoding:    "windows-1252",
			result:      "”ÄnsZ“w!äDrä",
		},
		{
			name:        "Windows1252-1",
			value:       "result-file-" + string([]byte{0xff, 0xfe, 0xfd}) + ".c",
			expectError: false,
			valid:       false,
			encoding:    "windows-1252",
			result:      "result-file-ÿþý.c",
		},
		{
			name:        "Windows1252-2",
			value:       "result-file-" + string([]byte{0x81}) + ".c",
			expectError: false,
			valid:       false,
			encoding:    "windows-1252",
			result:      "result-file-�.c",
		},
		{
			name:        "Windows1251-1",
			value:       "cyrillic-" + string([]byte{0xD4, 0xD5, 0xD6, 0xD7, 0xD8, 0xD9, 0x98}) + "�",
			expectError: false,
			valid:       false,
			encoding:    "windows-1251",
			result:      "cyrillic-ФХЦЧШЩ�пїЅ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			daemon := LprDaemon{}
			err := daemon.SetFallbackEncoding(tt.encoding)
			require.Nil(t, err)

			result, valid, err := daemon.ensureUTF8([]byte(tt.value))
			if tt.expectError {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}
			require.Equal(t, tt.valid, valid)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestDaemonMustNotCloseConnection(t *testing.T) {
	SetDebugLogger(log.Print)
	var err error
	var out []byte
	var name string

	port := uint16(2345)

	text := "Text for the file"
	name, err = generateTempFile("", "", text)
	require.Nil(t, err)

	fmt.Println("Tempfile:", name)

	var lprd LprDaemon
	var lprs LprSend

	err = lprd.Init(port, "")
	require.Nil(t, err)

	file, err := os.Open(name)
	require.Nil(t, err)
	defer file.Close()

	// send file with correct size
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Minute)
	require.Nil(t, err)

	err = lprs.sendFile(file, int64(len(text)))
	require.Nil(t, err)
	err = lprs.SendConfiguration()
	require.Nil(t, err)

	time.Sleep(1 * time.Second)

	one := make([]byte, 1)
	lprs.socket.SetReadDeadline(time.Now().Add(1 * time.Second))

	if _, err := lprs.socket.Read(one); err == io.EOF {
		t.Error("Daemon closed the connection")
	}

	err = lprs.Close()
	require.Nil(t, err)

	con := <-lprd.FinishedConnections()
	require.Equal(t, End, con.Status)
	out, err = ioutil.ReadFile(con.SaveName)
	require.Nil(t, err)
	err = os.Remove(con.SaveName)
	require.Nil(t, err)
	require.Equal(t, text, string(out))

	lprd.Close()
	os.Remove(name)
}
