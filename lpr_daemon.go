package lprlib

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"
)

type ConnectionType int

const (
	ConnectionTypePrintAnyWaitingJobs ConnectionType = 0
	ConnectionTypeReceivePrintJob     ConnectionType = 1
	ConnectionTypeSendQueueStateShort ConnectionType = 2
	ConnectionTypeSendQueueStateLog   ConnectionType = 3
	ConnectionTypeRemoveJobs          ConnectionType = 4
	ConnectionTypeUnknown             ConnectionType = 5
)

type QueueState func(queue string, list string, long bool) string

type ExternalIDCallbackFunc func() (uint64, error)

func init() {
	rand.Seed(time.Now().UnixMicro())
}

// LprDaemon structure
type LprDaemon struct {
	finishedConns chan *LprConnection
	connections   chan *LprConnection

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

	GetExternalID ExternalIDCallbackFunc
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
	lpr.connections = make(chan *LprConnection, 100)

	listenAddr := fmt.Sprintf(":%d", port)
	logDebugf("Listening on: %s", listenAddr)

	var err error
	lpr.socket, err = net.Listen("tcp", listenAddr)
	if err != nil {
		return &LprError{"Can't listen to " + listenAddr + " : " + err.Error()}
	}

	go lpr.externalIDGenerator()
	go lpr.Listen()

	return nil
}

func (lpr *LprDaemon) externalIDGenerator() {
	for {
		select {
		case conn := <-lpr.connections:
			lpr.generateExternalID(conn)
		case <-lpr.ctx.Done():
			// quit id generator routine
			return
		}
	}
}

func (lpr *LprDaemon) generateExternalID(conn *LprConnection) {
	defer close(conn.externalIDChan)

	connectionType := <-conn.typeChan
	if connectionType != ConnectionTypeReceivePrintJob {
		return
	}

	extID := uint64(0)
	if lpr.GetExternalID != nil {
		var err error
		extID, err = lpr.GetExternalID()
		if err != nil {
			conn.externalIDErrorChan <- err
			// TODO externalIDErrorChan unused... thing through
		}
	}
	conn.externalIDChan <- extID
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
		newLprcon.Init(newConn, 0, lpr, lpr.ctx, lpr.GetExternalID)
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
	// DaemonCommand means, that the LPR daemon wants to receive a Deamon command (see RFC-1179, chapter 5)
	DaemonCommand ConnectionStatus = 0

	// JobSubCommand means, that the LPR daemon wants to receive a job sub-command (see RFC-1179, chapter 6)
	JobSubCommand ConnectionStatus = 1

	// End end of request processing
	End ConnectionStatus = 4

	// Error Error
	Error ConnectionStatus = 0xff
)

// LprConnection Accepted connection
type LprConnection struct {
	// buffer contains read data from the socket
	buffer []uint8

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

	// dataFileReceived tells if the data file was already received
	dataFileReceived bool

	// controlFileReceived tells if the control file was already received
	controlFileReceived bool

	// ExternalID describes a reference of a print job id
	ExternalID uint64

	typeChan            chan ConnectionType
	externalIDChan      chan uint64
	externalIDErrorChan chan error
}

// Init is the constructor of LprConnection
// socket is the accepted connection
// bufferSize is per default 8192
func (lpr *LprConnection) Init(socket net.Conn, bufferSize int64, daemon *LprDaemon, ctx context.Context, getExternalID ExternalIDCallbackFunc) {
	if bufferSize == 0 {
		bufferSize = 8192
	}

	lpr.buffer = make([]byte, bufferSize)
	lpr.Connection = socket
	lpr.BufferSize = bufferSize
	lpr.done = make(chan bool)
	lpr.daemon = daemon
	lpr.ctx = ctx
	lpr.typeChan = make(chan ConnectionType, 1)
	lpr.externalIDChan = make(chan uint64, 1)
	lpr.externalIDErrorChan = make(chan error, 1)

	daemon.connections <- lpr

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

// ReadCommand reads from the socket until the newline character occurs, but only a maximum number of len(buffer) bytes.
// The command returned does not include the LF character.
func (lpr *LprConnection) ReadCommand() ([]byte, error) {
	offset := 0

	for {
		logDebugf("Reading next block from socket, offset: %d", offset)
		bytesRead, err := lpr.Connection.Read(lpr.buffer[offset:])
		if err != nil {
			return nil, fmt.Errorf("error reading from LPR connection: %w", err)
		}

		logDebugf("Read %d bytes from socket", bytesRead)

		for i, b := range lpr.buffer {
			if b == '\n' {
				if i != (offset+bytesRead)-1 {
					logErrorf("Garbage at data from socket after byte %d (offset %d): %s", i, offset, string(lpr.buffer[i+1:]))
				}

				return lpr.buffer[:i], nil
			}
		}

		offset += bytesRead
	}
}

// RunConnection This method read the data from the client
func (lpr *LprConnection) RunConnection() {
	defer func() {
		close(lpr.done)
		close(lpr.typeChan)
		select {
		case lpr.ExternalID = <-lpr.externalIDChan:
		case <-lpr.ctx.Done():
		}
		lpr.daemon.finishedConns <- lpr
	}()

	var err error
	lpr.Status = DaemonCommand

	// traceFile
	var traceFile *os.File
	if lpr.daemon.Trace {
		traceFile, err = os.CreateTemp(lpr.daemon.InputFileSaveDir, "lpr_trace_*")
		if err != nil {
			logErrorf("failed to create trace file: %v", err)
		}
		defer traceFile.Close()
		logDebugf("Created trace file %s", traceFile.Name())
		traceFile.WriteString(fmt.Sprintf("LPR connection trace %s\n", time.Now()))
	}

	for lpr.Status != Error && lpr.Status != End {
		command, err := lpr.ReadCommand()

		select {
		case <-lpr.ctx.Done():
			lpr.end(fmt.Errorf("exit connection"))
			return
		default:
		}

		if traceFile != nil {
			traceFile.WriteString(fmt.Sprintf("received message %d:\n", len(command)))
			if err != nil {
				traceFile.WriteString(fmt.Sprintf("error: %v\n", err))
			} else {
				traceFile.WriteString("-----\n")
				traceFile.Write(command)
				traceFile.WriteString("\n-----\n")
			}
		}

		if err != nil {
			lpr.end(err)
			break
		} else {
			if len(command) == 0 {
				lpr.end(nil)
				break
			}

			switch lpr.Status {
			case DaemonCommand:
				err = lpr.parseDaemonCommand(command)
				if err != nil {
					logErrorf("Error parsing daemon command: %s", err.Error())
					lpr.end(err)
				}

			case JobSubCommand:
				err = lpr.parseJobSubCommand(command)
				if err != nil {
					logErrorf("Error parsing job sub command: %s", err.Error())
					lpr.end(err)
				}

			case Error:
				// do nothing

			default:
				logErrorf("Unexpected connection status %d", lpr.Status)
			}

			if lpr.dataFileReceived && lpr.controlFileReceived {
				lpr.end(nil)
				break
			}
		}
	}
}

// end should be called when processing a request is done to set the connection status to "End" and
// close the output file and network connection.
func (lpr *LprConnection) end(err error) {
	if err != nil {
		logErrorf("Error processing: %s", err.Error())
		lpr.Status = Error
	} else {
		logDebug("Request processed")
		lpr.Status = End
	}

	lpr.close()
}

// close closes the output file (if any is open) and the network connection.
func (lpr *LprConnection) close() {
	if lpr.Output != nil {
		err := lpr.Output.Close()
		if err != nil {
			logErrorf("Error closing output file %s: %s", lpr.Output.Name(), err)
		}
		lpr.Output = nil
	}

	err := lpr.Connection.Close()
	if err != nil {
		logErrorf("Error closing connection: %s", err.Error())
	}
}

// parseDaemonCommand parses the specified command
func (lpr *LprConnection) parseDaemonCommand(command []byte) error {
	firstSymbol := command[0]

	switch firstSymbol {
	/* Daemon commands */
	/* 01 - Print any waiting jobs */
	case 0x1:
		lpr.typeChan <- ConnectionTypePrintAnyWaitingJobs

	/* 02 - Receive a printer job */
	case 0x2:
		lpr.typeChan <- ConnectionTypeReceivePrintJob
		var err error
		lpr.PrqName, _, err = lpr.daemon.ensureUTF8(command[1:])
		if err != nil {
			logErrorf("Invalid printer queue name %q: %v", lpr.PrqName, err)
		}
		lpr.Status = JobSubCommand

		return lpr.sendAck()

	/* 03 - Send queue state (short) */
	/* | 03 | Queue | SP | List | LF | */
	case 0x3:
		lpr.typeChan <- ConnectionTypeSendQueueStateShort
		fallthrough

	/* 04 - Send queue state (long) */
	/* | 04 | Queue | SP | List | LF | */
	case 0x4:
		lpr.typeChan <- ConnectionTypeSendQueueStateLog
		parts := operands(command[1:], 2)
		queue := parts[0]
		list := ""
		if len(parts) > 1 {
			list = parts[1]
		}

		lpr.replyQueueState(queue, list, firstSymbol == 0x4)

	/* 05 - Remove jobs */
	case 0x5:
		lpr.typeChan <- ConnectionTypeRemoveJobs

	default:
		lpr.typeChan <- ConnectionTypeUnknown
		return fmt.Errorf("unknown Daemon command %02x (%c) :: %s", command[0], command[0], string(command))

	}

	return nil
}

var asciiSpace = [256]byte{' ': 1, '\t': 1, '\v': 1, '\f': 1}

func operands(data []byte, max int) []string {
	var opers = make([]string, 0)

	oper := []byte{}
	for i, b := range data {
		if asciiSpace[b] == 1 {
			opers = append(opers, string(oper))

			if len(opers) == max {
				return append(opers, string(data[i+1:]))
			}

			oper = []byte{}
		} else {
			oper = append(oper, b)
		}
	}

	return append(opers, string(oper))
}

// parseJobSubCommand parses the specified command
func (lpr *LprConnection) parseJobSubCommand(command []byte) error {
	firstSymbol := command[0]

	switch firstSymbol {
	/* 01 - Abort job */
	case 0x1:
		return errors.New("job aborted")

	/* 02 - Receive Control File */
	case 0x2:
		operands := operands(command[1:], 2)
		if len(operands) != 2 {
			return fmt.Errorf("received job sub command %s, but got %d operands (and expected 2)", string(command), len(operands))
		}

		controlFileSize, err := strconv.ParseUint(operands[0], 10, 64)
		if err != nil {
			return fmt.Errorf("error parsing control file size %q: %w", operands[0], err)
		}

		err = lpr.sendAck()
		if err != nil {
			return err
		}

		err = lpr.receiveControlFile(operands[1], controlFileSize)
		if err != nil {
			return fmt.Errorf("error receiving control file: %w", err)
		}

		err = lpr.sendAck()
		if err != nil {
			return err
		}

		lpr.controlFileReceived = true

	/* 03 - Receive Data File */
	case 0x3:
		operands := operands(command[1:], 2)
		if len(operands) != 2 {
			return fmt.Errorf("received job sub command %s, but got %d operands (and expected 2)", string(command), len(operands))
		}

		dataFileSize, err := strconv.ParseUint(operands[0], 10, 64)
		if err != nil {
			return fmt.Errorf("error parsing data file size %q: %w", operands[0], err)
		}

		err = lpr.sendAck()
		if err != nil {
			return err
		}

		err = lpr.receiveDataFile(operands[1], dataFileSize)
		if err != nil {
			return fmt.Errorf("error receiving data file: %w", err)
		}

		err = lpr.sendAck()
		if err != nil {
			return err
		}

		lpr.dataFileReceived = true

	default:
		return fmt.Errorf("unknown Job Sub command %02x (%c) :: %s", command[0], command[0], string(command))
	}

	return nil
}

func (lpr *LprConnection) receiveControlFile(fileName string, bytes uint64) error {
	logDebugf("Receiving control file %q with %d bytes", fileName, bytes)

	// +1, because the sender will add a 0x00 byte to the control file
	buffer := make([]byte, bytes+1)

	_, err := io.ReadFull(lpr.Connection, buffer)
	if err != nil {
		return fmt.Errorf("error reading control file %s with %d bytes: %w", fileName, bytes, err)
	}

	line := []byte{}

	for _, b := range buffer {
		if b == '\n' {
			// end of control file line
			err = lpr.parseControlFileLine(line)
			if err != nil {
				return fmt.Errorf("error parsing control file line %q: %w", string(line), err)
			}

			line = make([]byte, 0)
		} else {
			line = append(line, b)
		}
	}

	return nil
}

func (lpr *LprConnection) parseControlFileLine(line []byte) error {
	var err error

	switch line[0] {
	/* C - Class for banner page */
	case 'C':
		lpr.ClassName, _, err = lpr.daemon.ensureUTF8(line[1:])
		if err != nil {
			return fmt.Errorf("invalid class name %q: %v", lpr.ClassName, err)
		}
		logDebugf("Class name: %s", lpr.ClassName)

	/* H - Host name */
	case 'H':
		lpr.Hostname, _, err = lpr.daemon.ensureUTF8(line[1:])
		if err != nil {
			return fmt.Errorf("invalid hostname %q: %v", lpr.Hostname, err)
		}
		logDebugf("Hostname: %s", lpr.Hostname)

	/* I - Indent Printing */
	case 'I':
		lpr.IntentingCount, err = strconv.ParseInt(string(line[1:]), 10, 64)
		if err != nil {
			return err
		}
		logDebugf("indenting_count: %d", lpr.IntentingCount)

	/* J - Job name for banner page */
	case 'J':
		lpr.JobName, _, err = lpr.daemon.ensureUTF8(line[1:])
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
		lpr.Filename, _, err = lpr.daemon.ensureUTF8(line[1:])
		if err != nil {
			return fmt.Errorf("invalid filename %q: %v", lpr.Filename, err)
		}
		logDebugf("Filename: %s", lpr.Filename)

	/* P - User identification */
	case 'P':
		lpr.UserIdentification, _, err = lpr.daemon.ensureUTF8(line[1:])
		if err != nil {
			return fmt.Errorf("invalid user identification %q: %v", lpr.UserIdentification, err)
		}
		logDebugf("User identification: %s", lpr.UserIdentification)

	/* S - Symbolic link data */
	case 'S':

	/* T - Title for pr */
	case 'T':
		lpr.TitleText, _, err = lpr.daemon.ensureUTF8(line[1:])
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
		lpr.PrintFileWithPr = string(line[1:])
		logDebugf("p: %s", lpr.PrintFileWithPr)

	/* r - File to print with FORTRAN carriage control */
	case 'r':

	/* t - Print troff output file */
	case 't':

	/* v - Print raster file */
	case 'v':

	case 0x00:

	default:
		return fmt.Errorf("unknown control file line %02x (%c) :: %s", line[0], line[0], string(line))

	}

	return nil
}

func (lpr *LprConnection) receiveDataFile(fileName string, bytes uint64) error {
	logDebugf("Receiving data file %q with %d bytes", fileName, bytes)

	var err error

	lpr.Filesize = int64(bytes)

	if lpr.Filesize > 2147483648 {
		lpr.Filesize = 0
		logDebug("Filesize > 2GB, won't check received byte count")
	}

	lpr.tempFilesize = lpr.Filesize

	lpr.Output, err = lpr.createTempFile()
	if err != nil {
		return fmt.Errorf("error while creating temporary file at %s! %w", lpr.daemon.InputFileSaveDir, err)
	}

	lpr.SaveName = lpr.Output.Name()
	logDebugf("New data file: %s", lpr.SaveName)

	for {
		select {
		case <-lpr.ctx.Done():
			return fmt.Errorf("exit connection")

		default:
		}

		bytes, err := lpr.Connection.Read(lpr.buffer)
		if err != nil {
			return fmt.Errorf("error reading data: %w", err)
		}

		endReached, err := lpr.addToFile(lpr.buffer[:bytes])
		if err != nil {
			return fmt.Errorf("error writing %d bytes to output file: %w", bytes, err)
		}

		if endReached {
			break
		}
	}

	return nil
}

func (lpr *LprConnection) sendAck() error {
	_, err := lpr.Connection.Write([]byte{0})
	if err != nil {
		logErrorf("Sending failed: %s", err.Error())
		return fmt.Errorf("sending ACK byte failed: %w", err)
	}

	return nil
}

// addToFile This method add the data to the output file
func (lpr *LprConnection) addToFile(data []uint8) (bool, error) {
	var err error

	end := false
	if lpr.Filesize == 0 {
		// file size is unknown, stop if last byte is \0
		if len(data) != 0 && data[len(data)-1] == 0 {
			data = data[:len(data)-1]
			end = true
		}

	} else if (lpr.tempFilesize - int64(len(data))) > 0 {
		lpr.tempFilesize = lpr.tempFilesize - int64(len(data))

	} else {
		data = data[:lpr.tempFilesize]

		end = true
	}

	_, err = lpr.Output.Write(data)
	if err != nil {
		return false, fmt.Errorf("write failed: %w", err)
	}

	if end {
		if lpr.Output != nil {
			lpr.Output.Close()
			lpr.Output = nil
		}
		lpr.tempFilesize = lpr.tempFilesize - int64(len(data))
		lpr.Status = JobSubCommand
	}

	return end, nil
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

func (lpr *LprConnection) replyQueueState(queue string, list string, long bool) error {
	state := "Idle\n"
	if lpr.daemon.GetQueueState != nil {
		state = lpr.daemon.GetQueueState(queue, list, long)
	}

	_, err := lpr.Connection.Write([]byte(state))
	if err != nil {
		logErrorf("Sending queue state failed: %s", err.Error())
	}

	lpr.end(nil)

	return nil
}
