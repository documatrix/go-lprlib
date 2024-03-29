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

	// send file with negative size
	err = lprs.Init("127.0.0.1", name, port, "raw", "TestUser", time.Second*2)
	require.Nil(t, err)

	err = lprs.SendConfiguration()
	require.Nil(t, err)

	_, err = file.Seek(0, 0)
	require.Nil(t, err)
	err = lprs.sendFile(file, -1024)
	require.Nil(t, err)
	err = lprs.Close()
	require.Nil(t, err)

	con = <-lprd.FinishedConnections()
	require.Equal(t, End, con.Status)
	out, err = os.ReadFile(con.SaveName)
	require.Nil(t, err)
	err = os.Remove(con.SaveName)
	require.Nil(t, err)
	require.Equal(t, text, string(out))

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

func TestDaemonWithInvalidControlFileContent(t *testing.T) {
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
		return nextExternalID
	}

	err = lprd.Init(port, "")
	require.Nil(t, err)

	err = customSendFunc("", file, "127.0.0.1", port, "raw", "TestUser1\n\n", time.Minute)
	require.Nil(t, err)

	conn := <-lprd.FinishedConnections()
	out, err := os.ReadFile(conn.SaveName)
	require.Nil(t, err)
	require.Nil(t, os.Remove(conn.SaveName))
	require.Equal(t, text, string(out))
	require.Equal(t, uint64(1), conn.ExternalID)
	require.Equal(t, "TestUser1", conn.UserIdentification)

	err = customSendFunc("\n", file, "127.0.0.1", port, "raw", "TestUser2", time.Minute)
	require.Nil(t, err)

	conn = <-lprd.FinishedConnections()
	out, err = os.ReadFile(conn.SaveName)
	require.Nil(t, err)
	require.Nil(t, os.Remove(conn.SaveName))
	require.Equal(t, text, string(out))
	require.Equal(t, uint64(2), conn.ExternalID)
	require.Equal(t, "TestUser2", conn.UserIdentification)

	lprd.Close()
}

func TestClosedConnectionCases(t *testing.T) {
	SetDebugLogger(log.Print)

	port := uint16(2345)

	text := "Text for the file"
	file, err := generateTempFile("", "", text)
	require.Nil(t, err)
	defer os.Remove(file)

	var lprd LprDaemon

	nextExternalID := uint64(10)
	// set lprd callback function
	lprd.GetExternalID = func() uint64 {
		nextExternalID++
		return nextExternalID
	}

	err = lprd.Init(port, "")
	require.Nil(t, err)

	// Connection closed without sending commands
	lpr := &LprSend{}
	err = lpr.Init("127.0.0.1", file, port, "", "", time.Minute)
	require.Nil(t, err)

	// Close connection
	err = lpr.Close()
	require.Nil(t, err)

	connection := <-lprd.FinishedConnections()
	// Connection results in End
	require.Equal(t, End, connection.Status)
	// ExternalID is not set
	require.Equal(t, uint64(0), connection.ExternalID)
	// SaveName is not set
	require.Empty(t, connection.SaveName)

	//////////////////
	// Connection closed after print job command
	lpr = &LprSend{}

	err = lpr.Init("127.0.0.1", file, port, "", "", time.Minute)
	require.Nil(t, err)

	// write 0x02 - Print job command
	_, err = lpr.writeByte([]byte{'\x02', '\n'})
	require.Nil(t, err)

	// Close connection without sending subcommand
	err = lpr.Close()
	require.Nil(t, err)

	connection = <-lprd.FinishedConnections()
	// Connection results in Error
	require.Equal(t, Error, connection.Status)
	// ExternalID is set
	require.Equal(t, uint64(11), connection.ExternalID)

	lprd.Close()
}

func customSendFunc(configPrefix string, file string, hostname string, port uint16, queue string, username string, timeout time.Duration) (err error) {
	lpr := &LprSend{}

	err = lpr.Init(hostname, file, port, queue, username, timeout)
	if err != nil {
		err = fmt.Errorf("Error initializing connection to LPR printer %s, port %d, queue: %s! %s", hostname, port, queue, err)
		return
	}

	defer func() {
		cerr := lpr.Close()
		if err == nil {
			err = cerr
		}
	}()

	err = customSendConfiguration(lpr, configPrefix)
	if err != nil {
		err = fmt.Errorf("Error sending configuration to LPR printer %s, port %d, queue: %s! %s", hostname, port, queue, err)
		return
	}

	err = lpr.SendFile()
	if err != nil {
		err = fmt.Errorf("Error sending file to LPR printer %s, port %d, queue: %s! %s", hostname, port, queue, err)
		return
	}

	return
}

func customSendConfiguration(lpr *LprSend, customConfigPrefix string) error {

	if err := lpr.startPrintJob(); err != nil {
		return err
	}

	/* receive_buffer is the buffer for the answer of the remote Server */
	receiveBuffer := make([]byte, 1)

	/* Create config data string */
	configData := customConfigPrefix
	for i, ia := range lpr.Config {
		configData += fmt.Sprintf("%c%s\n", i, ia)
	}

	if configData == "" {
		return &LprError{"CONFIG_NOT_FOUND Cannot found printer configuration"}
	}

	/* Host name */
	osHostname, err := os.Hostname()
	if err != nil {
		return &LprError{"Can't resolve Hostname"}
	}

	/* Send the server the length of the configuration */
	configInfo := fmt.Sprintf("%c%d cfA000%s\n", 0x02, len(configData), osHostname)
	_, err = lpr.writeString(configInfo)
	if err != nil {
		return &LprError{"PRINTER_ERROR: " + err.Error()}
	}
	logDebug("Config info:", configInfo)

	/*
	 * Receive answer ( 0 if there wasn't an error )
	 */
	length, err := lpr.readByte(receiveBuffer)
	if err != nil {
		return &LprError{err.Error()}
	}
	if length != 0 {
		logDebugf("Received: %d", receiveBuffer[0])
		if receiveBuffer[0] != 0 {
			errorstring := fmt.Sprint("PRINTER_ERROR Printer reported an error (", receiveBuffer[0], ")!")
			return &LprError{errorstring}
		}
	}

	/*
	 * Send the server the configuration
	 * A data transmit must have a 0 byte at the ending
	 */
	sendBuffer := configData + "\x00"

	_, err = lpr.writeString(sendBuffer)
	if err != nil {
		return &LprError{"PRINTER_ERROR: " + err.Error()}
	}
	logDebug("Config:\n", configData)

	/*
	 * Receive answer ( 0 if there wasn't an error )
	 */
	length, err = lpr.readByte(receiveBuffer)
	if err != nil {
		return &LprError{err.Error()}
	}
	if length != 0 {
		logDebugf("Received: %d", receiveBuffer[0])
		if receiveBuffer[0] != 0 {
			errorstring := fmt.Sprint("PRINTER_ERROR Printer reported an error (", receiveBuffer[0], ")!")
			return &LprError{errorstring}
		}
	}

	return nil
}
