package lprlib

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

type QueueState func(queue string, list string, long bool) string

func init() {
	rand.Seed(time.Now().UnixMicro())
}

// LprDaemon structure
type LprDaemon struct {
	finishedConns chan *LprConnection

	ctx    context.Context
	cancel context.CancelFunc

	socket net.Listener

	// GetQueueState will be called if a client requests the queue state.
	// If not set, "Idle" will be returned.
	GetQueueState QueueState

	// InputFileSaveDir is the directory into which received files will be saved.
	// If empty, the default system temp directory will be used.
	// if nil set, a temp file will be used instead of the directory
	InputFileSaveDir string

	// Trace states if the LprDaemon should create a trace file for each connection.
	// The trace file will be saved into the InputFileSaveDir or system temp directory.
	Trace bool

	fallbackDecoder *encoding.Decoder

	fileMask os.FileMode
}

// Init is the constructor
// port ist the tcp port where the daemon should listen default 515
// ipAddress of the daemon default own ip
func (lpr *LprDaemon) Init(port uint16, ipAddress string) error {

	if port == 0 {
		port = 515
	}

	if err := lpr.SetFallbackEncoding("windows-1252"); err != nil {
		return err
	}

	lpr.fileMask = 0600

	lpr.ctx, lpr.cancel = context.WithCancel(context.Background())
	lpr.finishedConns = make(chan *LprConnection, 100)

	listenAddr := fmt.Sprintf(":%d", port)
	logDebugf("Listening on: %s", listenAddr)

	var err error
	lpr.socket, err = net.Listen("tcp", listenAddr)
	if err != nil {
		return &LprError{"Can't listen to " + listenAddr + " : " + err.Error()}
	}

	go lpr.Listen()

	return nil
}

// SetFileMask can be used to set the file mask which should be applied to the
// data file which is written by new connections.
func (lpr *LprDaemon) SetFileMask(fileMask os.FileMode) {
	lpr.fileMask = fileMask
}

// SetFallbackEncoding sets the given encoding as fallback encoding.
// Will be used to decode any received non-utf8 string values like Filename, PrqName, UserIdentification, etc.
// Will not be applied to any received file contents.
// Defaults to windows-1252.
func (lpr *LprDaemon) SetFallbackEncoding(encodingName string) error {
	encoding, err := ianaindex.IANA.Encoding(encodingName)
	if err != nil {
		return err
	}

	lpr.fallbackDecoder = encoding.NewDecoder()

	return nil
}

// Listen waits for a new connection and accept them
func (lpr *LprDaemon) Listen() {
	for {

		logDebug("Wait for Connections...")
		newConn, err := lpr.socket.Accept()

		select {
		case <-lpr.ctx.Done():
			if newConn != nil {
				newConn.Close()
			}
			logDebug("Listener closed")
			return
		default:
		}
		if err != nil {
			logError("Can't accept connection: " + err.Error())
		}
		logDebug("Accepted Client")

		var newLprcon LprConnection
		newLprcon.Init(newConn, 0, lpr, lpr.ctx)
	}
}

// Close Closes all LprConnections and the listener
func (lpr *LprDaemon) Close() {
	lpr.cancel()
	lpr.socket.Close()
}

// FinishedConnections returns a channel containing the finished connections.
// The ConnectionStatus may be END or ERROR.
// Will also contain LPR Queue State requests (check with SaveName != "").
func (lpr *LprDaemon) FinishedConnections() <-chan *LprConnection {
	return lpr.finishedConns
}

// ensureUTF8 checks if the given value contains valid UTF-8 encoded runes.
// If not, the function tries to decode the given value using the fallbackDecoder.
func (lpr *LprDaemon) ensureUTF8(value []byte) (string, bool, error) {
	valid := utf8.Valid(value)
	if !valid {
		decodedValue, err := lpr.fallbackDecoder.Bytes(value)
		if err != nil {
			return string(value), valid, err
		}
		value = decodedValue
	}

	return string(value), valid, nil
}

type ConnectionStatus int16

const (
	// BEGIN start to read data
	BEGIN ConnectionStatus = 0

	// PRINTJOB_SUB_COMMANDS receive print job sub commands
	PRINTJOB_SUB_COMMANDS ConnectionStatus = 1

	// FILEDATA in filedata - block right now
	FILEDATA ConnectionStatus = 2

	// DATABLOCK in datablock-block right now
	DATABLOCK ConnectionStatus = 3

	// END end of the data
	END ConnectionStatus = 4

	// CLOSE the connection should be closed
	CLOSE ConnectionStatus = 5

	// ERROR Error
	ERROR ConnectionStatus = 0xff
)

// LprConnection Accepted connection
type LprConnection struct {

	// bufferString buffer uint8 array
	bufferString []uint8

	// tempFilesize the aktuell filesize ( it shows how many chars are left )
	tempFilesize int64

	// Connection connection
	Connection net.Conn

	// Hostname Hostname
	Hostname string

	// Filename Filename
	Filename string

	// PrqName PRQ - Name
	PrqName string

	// UserIdentification User Identification
	UserIdentification string

	// JobName Job name
	JobName string

	// BufferSize the size of the buffer
	BufferSize int64

	// TitleText Title
	TitleText string

	// ClassName Name of class for banner pages
	ClassName string

	// Filesize Filesize
	Filesize int64

	// Output output File
	Output *os.File

	// IntentingCount Indenting count
	IntentingCount int64

	// Status Status
	Status ConnectionStatus

	// PrintFileWithPr Print file with pr
	PrintFileWithPr string

	// SaveName The File name of the new file
	SaveName string

	// done is used to stop all go routines of the connection.
	done chan bool

	// ctx is the lpr daemon's context.
	// The connection must be closed once the context is canceled.
	ctx context.Context

	// daemon contains a reference to the LprDaemon
	daemon *LprDaemon
}

// Init is the constructor of LprConnection
// socket is the accepted connection
// bufferSize is per default 8192
func (lpr *LprConnection) Init(socket net.Conn, bufferSize int64, daemon *LprDaemon, ctx context.Context) {
	if bufferSize == 0 {
		bufferSize = 8192
	}
	lpr.Connection = socket
	lpr.BufferSize = bufferSize
	lpr.done = make(chan bool)
	lpr.daemon = daemon
	lpr.ctx = ctx

	go func() {
		select {
		case <-ctx.Done():
			err := lpr.Connection.Close()
			if err != nil {
				logErrorf("error closing connection: %v", err)
			}
		case <-lpr.done:
		}
	}()

	go lpr.RunConnection()
}

// RunConnection This method read the data from the client
func (lpr *LprConnection) RunConnection() {
	defer func() {
		close(lpr.done)
		lpr.daemon.finishedConns <- lpr
	}()

	var inData bool
	var buffer []uint8
	var length int
	var err error
	lpr.Status = FILEDATA

	// traceFile
	var traceFile *os.File
	if lpr.daemon.Trace {
		traceFile, err = ioutil.TempFile(lpr.daemon.InputFileSaveDir, "lpr_trace_*")
		if err != nil {
			logErrorf("failed to create trace file: %v", err)
		}
		defer traceFile.Close()
		logDebugf("Created trace file %s", traceFile.Name())
		traceFile.WriteString(fmt.Sprintf("LPR connection trace %s\n", time.Now()))
	}

	buffer = make([]uint8, lpr.BufferSize)
	for lpr.Status != ERROR {
		length = 0

		length, err = lpr.Connection.Read(buffer)
		select {
		case <-lpr.ctx.Done():
			lpr.Status = ERROR
			if lpr.Output != nil {
				lpr.Output.Close()
			}
			logDebug("Exit Connection")
			return
		default:
		}

		if traceFile != nil {
			traceFile.WriteString(fmt.Sprintf("received message %d:\n", length))
			if err != nil {
				traceFile.WriteString(fmt.Sprintf("error: %v\n", err))
			} else {
				traceFile.WriteString("-----\n")
				traceFile.Write(buffer[:length])
				traceFile.WriteString("\n-----\n")
			}
		}

		if err != nil {
			lpr.End(err)
			break
		} else {
			if length == 0 {
				lpr.End(nil)
				break
			}

			if length == -1 {
				logError("File could not be received!")
				lpr.Status = ERROR
				break
			}

			if lpr.Status == DATABLOCK {
				inData = true
			} else {
				inData = false
			}

			lpr.HandleData(buffer, int64(length))

			if lpr.Status == CLOSE {
				lpr.Close()
				lpr.Status = END
				break
			}

			if lpr.Status != DATABLOCK || !inData {
				_, err = lpr.Connection.Write([]byte{0})
				if err != nil {
					logErrorf("Sending failed: %s", err.Error())
					lpr.Status = ERROR
				}
			}
		}
	}
}

func (lpr *LprConnection) End(err error) {
	if lpr.Status < END {
		if (lpr.Status == DATABLOCK && lpr.Filesize == 0) ||
			(lpr.Status == PRINTJOB_SUB_COMMANDS && lpr.SaveName != "" && lpr.Output == nil) {
			logDebug("File was received!")
			lpr.Status = END
		} else if err == io.EOF {
			logErrorf("Unexpected EOF: %s", err)
			lpr.Status = ERROR
		} else if err != nil {
			logErrorf("Reading buffer failed: %s", err.Error())
			lpr.Status = ERROR
		} else {
			logDebug("Received unexpected end!")
			lpr.Status = ERROR
		}
	} else {
		logDebug("File was received!")
		lpr.Status = END
	}
	lpr.Close()
}

func (lpr *LprConnection) Close() {
	if lpr.Output != nil {
		lpr.Output.Close()
		lpr.Output = nil
	}

	err := lpr.Connection.Close()
	if err != nil {
		logErrorf("Error closing connection: %s", err.Error())
	}
}

// HandleData This method choose if the data should go to the file or to the interpreter
func (lpr *LprConnection) HandleData(data []uint8, length int64) {
	if lpr.Status != DATABLOCK {
		tstring := string(data[:length])
		dataArray := strings.Split(tstring, "\n")
		for _, iv := range dataArray {
			ivLen := len(iv)
			if ivLen > 0 {
				if lpr.Status != PRINTJOB_SUB_COMMANDS {
					lpr.Interpret([]byte(iv), int64(ivLen))
				} else {
					err := lpr.InterpretJobSubCommand([]byte(iv), int64(ivLen))
					if err != nil {
						logError(err)
					}
				}
			}
		}
	} else {
		lpr.AddToFile(data, length)
	}
}

// AddToFile This method add the data to the output file
func (lpr *LprConnection) AddToFile(data []uint8, length int64) {
	var err error
	var test []uint8
	end := false
	if lpr.Filesize == 0 {
		// file size is unknown, stop if last byte is \0
		if length != 0 && data[length-1] == 0 {
			length--
			end = true
		}
		test = data[:length]
		_, err = lpr.Output.Write(test)
		if err != nil {
			logErrorf("Write failed: %s", err.Error())
		}
	} else if (lpr.tempFilesize - length) > 0 {
		lpr.tempFilesize = lpr.tempFilesize - length
		test = data[:length]
		_, err = lpr.Output.Write(test)
		if err != nil {
			logErrorf("Write failed: %s", err.Error())
		}
	} else {
		test = data[:lpr.tempFilesize]
		_, err = lpr.Output.Write(test)
		if err != nil {
			logErrorf("Write failed: %s", err.Error())
			return
		}
		end = true
	}
	if end {
		if lpr.Output != nil {
			lpr.Output.Close()
			lpr.Output = nil
		}
		lpr.tempFilesize = lpr.tempFilesize - length
		lpr.Status = PRINTJOB_SUB_COMMANDS
	}

	// pro := float32(100.0) - float32(lpr.tempFilesize*100)/float32(lpr.Filesize)
	// fmt.Print("<")
	// for i := 0; i < 50; i++ {
	// 	if int(pro/2) < i {
	// 		fmt.Print(" ")
	// 	} else {
	// 		fmt.Print("-")
	// 	}
	// }
	// fmt.Printf("> %f %%\r", pro)
}

// Interpret interprets the LPR daemon commands
func (lpr *LprConnection) Interpret(data []uint8, length int64) {
	firstSymbol := data[0]
	switch firstSymbol {
	/* Daemon commands */
	/* 01 - Print any waiting jobs */
	case 0x1:

	/* 02 - Receive a printer job */
	case 0x2:
		var err error
		lpr.PrqName, _, err = lpr.daemon.ensureUTF8(data[1:length])
		if err != nil {
			logErrorf("Invalid printer queue name %q: %v", lpr.PrqName, err)
		}
		lpr.Status = PRINTJOB_SUB_COMMANDS

	/* 03 - Send queue state (short) */
	/* | 03 | Queue | SP | List | LF | */
	case 0x3:
		fallthrough

	/* 04 - Send queue state (long) */
	/* | 04 | Queue | SP | List | LF | */
	case 0x4:
		content := string(data[1:length])
		parts := strings.SplitN(content, " ", 2)
		queue := parts[0]
		list := ""
		if len(parts) > 1 {
			list = parts[1]
		}

		lpr.replyQueueState(queue, list, firstSymbol == 0x4)

	/* 05 - Remove jobs */
	case 0x5:

	default:
		logErrorf("First Element: %02x (%c)", data[0], data[0])
		logErrorf("Unknown Code: %s", string(data[:length]))
		break
	}
}

func (lpr *LprConnection) createTempFile() (*os.File, error) {
	try := 0
	for {
		fileName := filepath.Join(lpr.daemon.InputFileSaveDir, strconv.FormatUint(uint64(rand.Int63()), 16))

		f, err := os.OpenFile(fileName, os.O_RDWR|os.O_CREATE|os.O_EXCL, lpr.daemon.fileMask)
		if os.IsExist(err) {
			if try++; try < 10000 {
				continue
			}
			return nil, fmt.Errorf("error creating temporary file! Giving up after %d tries", try)
		}
		return f, err
	}
}

// InterpretJobSubCommand interprets the job sub commands which are received after
// the "02 - Receive a printer job" command was read
func (lpr *LprConnection) InterpretJobSubCommand(data []uint8, length int64) error {
	var err error
	var tstring string
	firstSymbol := data[0]
	switch firstSymbol {
	/* Daemon commands */
	/* 01 - Abort job */
	case 0x1:

	/* 02 - Receive control file */
	case 0x2:

	/* 03 - Receive data file */
	case 0x3:
		lpr.Status = DATABLOCK
		lpr.bufferString = nil
		for i := int64(1); i < length && data[i] != ' '; i++ {
			lpr.bufferString = append(lpr.bufferString, data[i])
		}
		tstring = string(lpr.bufferString)
		lpr.Filesize, err = strconv.ParseInt(tstring, 10, 64)
		if err != nil {
			return fmt.Errorf("Error while parsing %s to integer! %s", tstring, err)
		}
		logDebugf("Filesize: %d", lpr.Filesize)
		if lpr.Filesize > 2147483648 {
			lpr.Filesize = 0
			logDebug("Filesize > 2GB, won't check received byte count")
		}
		lpr.tempFilesize = lpr.Filesize

		lpr.Output, err = lpr.createTempFile()
		if err != nil {
			return fmt.Errorf("Error while creating temporary file at %s! %s", lpr.daemon.InputFileSaveDir, err)
		}

		lpr.SaveName = lpr.Output.Name()
		logDebugf("New data file: %s", lpr.SaveName)

	/* Control file lines */

	/* C - Class for banner page */
	case 'C':
		lpr.ClassName, _, err = lpr.daemon.ensureUTF8(data[1:length])
		if err != nil {
			return fmt.Errorf("invalid class name %q: %v", lpr.ClassName, err)
		}
		logDebugf("Class name: %s", lpr.ClassName)

	/* H - Host name */
	case 'H':
		var err error
		lpr.Hostname, _, err = lpr.daemon.ensureUTF8(data[1:length])
		if err != nil {
			return fmt.Errorf("invalid hostname %q: %v", lpr.Hostname, err)
		}
		logDebugf("Hostname: %s", lpr.Hostname)

	/* I - Indent Printing */
	case 'I':
		lpr.IntentingCount, err = strconv.ParseInt(string(data[1:length]), 10, 64)
		if err != nil {
			return err
		}
		logDebugf("indenting_count: %d", lpr.IntentingCount)

	/* J - Job name for banner page */
	case 'J':
		lpr.JobName, _, err = lpr.daemon.ensureUTF8(data[1:length])
		if err != nil {
			return fmt.Errorf("invalid job name %q: %v", lpr.JobName, err)
		}
		logDebugf("Job name: %s", lpr.JobName)

	/* L - Print banner page */
	case 'L':
		break

	/* M - Mail When Printed */
	case 'M':
		break

	/* N - Name of source file */
	case 'N':
		var err error
		lpr.Filename, _, err = lpr.daemon.ensureUTF8(data[1:length])
		if err != nil {
			return fmt.Errorf("invalid filename %q: %v", lpr.Filename, err)
		}
		logDebugf("Filename: %s", lpr.Filename)

	/* P - User identification */
	case 'P':
		lpr.UserIdentification, _, err = lpr.daemon.ensureUTF8(data[1:length])
		if err != nil {
			return fmt.Errorf("invalid user identification %q: %v", lpr.UserIdentification, err)
		}
		logDebugf("User identification: %s", lpr.UserIdentification)

	/* S - Symbolic link data */
	case 'S':

	/* T - Title for pr */
	case 'T':
		lpr.TitleText, _, err = lpr.daemon.ensureUTF8(data[1:length])
		if err != nil {
			return fmt.Errorf("invalid title text %q: %v", lpr.TitleText, err)
		}
		logDebugf("Title text: %s", lpr.TitleText)

	/* U - Unlink data file */
	case 'U':

	/* W - Width of output */
	case 'W':

	/* 1 - troff R font */
	case '1':

	/* 2 - troff I font */
	case '2':

	/* 3 - troff B font */
	case '3':

	/* 4 - troff S font */
	case '4':

	/* c - Plot CIF file */
	case 'c':

	/* d - Print DVI file */
	case 'd':

	/* f - Print formatted file */
	case 'f':

	/* g - Plot file */
	case 'g':

	/* l - Print file leaving control characters */
	case 'l':

	/* n - Print ditroff output file */
	case 'n':

	/* o - Print Postscript output file */
	case 'o':

	/* p - Print file with 'pr' format */
	case 'p':
		lpr.PrintFileWithPr = string(data[1:length])
		logDebugf("p: %s", lpr.PrintFileWithPr)

	/* r - File to print with FORTRAN carriage control */
	case 'r':

	/* t - Print troff output file */
	case 't':

	/* v - Print raster file */
	case 'v':

	case 0x00:

	default:
		logErrorf("First Element: %02x (%c)", data[0], data[0])
		logErrorf("Unknown Code: %s", string(data[:length]))
		break

	}
	return nil
}

func (lpr *LprConnection) replyQueueState(queue string, list string, long bool) error {

	state := "Idle\n"
	if lpr.daemon.GetQueueState != nil {
		state = lpr.daemon.GetQueueState(queue, list, long)
	}

	_, err := lpr.Connection.Write([]byte(state))
	if err != nil {
		logErrorf("Sending queue state failed: %s", err.Error())
	}

	lpr.Status = CLOSE

	return nil
}
