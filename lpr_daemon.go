package lprlib

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strconv"
	"strings"
)

type QueueState func(queue string, list string, long bool) string

// LprDaemon structure
type LprDaemon struct {

	/* All connections */
	connections []*LprConnection

	/* Used for closing the Listener */
	closing chan bool

	socket net.Listener

	// GetQueueState will be called if a client requests the queue state.
	// If not set, "Idle" will be returned.
	GetQueueState QueueState

	// InputFileSaveDir is the directory into which received files will be saved.
	// If empty, the default system temp directory will be used.
	// if nil set, a temp file will be used instead of the directory
	InputFileSaveDir string
}

// Init is the constructor
// port ist the tcp port where the deamon should listen default 515
// ipAddress of the deamon default own ip
func (lpr *LprDaemon) Init(port uint16, ipAddress string) error {

	if port == 0 {
		port = 515
	}

	lpr.closing = make(chan bool, 1)

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

// Listen waits for a new connection and accept them
func (lpr *LprDaemon) Listen() {
	for {

		logDebug("Wait for Connections...")
		newConn, err := lpr.socket.Accept()

		select {
		case <-lpr.closing:
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
		newLprcon.Init(newConn, 0, lpr)

		lpr.connections = append(lpr.connections, &newLprcon)
	}
}

// DelConnection deletes the LprConnection with the index
func (lpr *LprDaemon) DelConnection(index uint64) {
	var zeroValue *LprConnection
	copy(lpr.connections[index:], lpr.connections[index+1:])
	lpr.connections[len(lpr.connections)-1] = zeroValue
	lpr.connections = lpr.connections[:len(lpr.connections)-1]
}

// Close Closes all LprConnections and the listener
func (lpr *LprDaemon) Close() {
	lpr.closing <- true
	lpr.socket.Close()
	lpr.closing <- true
	for _, iv := range lpr.connections {
		iv.KillConnection()
	}
}

// DelFinishedConnection deletes all connection with status END or ERROR
func (lpr *LprDaemon) DelFinishedConnection() {
	var bufferConnections []*LprConnection
	var zeroValue []*LprConnection
	bufferConnections = lpr.connections
	lpr.connections = zeroValue
	for i := 0; i < len(bufferConnections); i++ {
		if bufferConnections[i].Status != END && bufferConnections[i].Status != ERROR {
			lpr.connections = append(lpr.connections, bufferConnections[i])
		}
	}
}

// GetConnections returns all LprConnections
func (lpr *LprDaemon) GetConnections() []*LprConnection {
	return lpr.connections
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

	// closing Used for Closing the LprConnection
	closing chan bool

	// daemon contains a reference to the LprDaemon
	daemon *LprDaemon
}

// Init is the constructor of LprConnection
// socet is the accepted connection
// bufferSize is per default 8192
func (lpr *LprConnection) Init(socket net.Conn, bufferSize int64, daemon *LprDaemon) {
	if bufferSize == 0 {
		bufferSize = 8192
	}
	lpr.Connection = socket
	lpr.BufferSize = bufferSize
	lpr.closing = make(chan bool, 1)
	lpr.daemon = daemon
	go lpr.RunConnection()
}

// KillConnection Closes the Connection and the outputfile
func (lpr *LprConnection) KillConnection() {
	lpr.closing <- true
	lpr.Connection.Close()
}

// RunConnection This method read the data from the client
func (lpr *LprConnection) RunConnection() {
	var inData bool
	var buffer []uint8
	var length int
	var err error
	lpr.Status = FILEDATA

	buffer = make([]uint8, lpr.BufferSize)
	for lpr.Status != ERROR {
		length = 0

		length, err = lpr.Connection.Read(buffer)
		select {
		case <-lpr.closing:
			lpr.Status = ERROR
			if lpr.Output != nil {
				lpr.Output.Close()
			}
			logDebug("Exit Connection")
			return
		default:
		}

		if err != nil {
			if err == io.EOF {
				if lpr.Status < END {
					logErrorf("Unexpected EOF: %s", err)
					lpr.Status = ERROR
				} else {
					logDebug("File was received!")
				}
			} else {
				logErrorf("Reading buffer failed: %s", err.Error())
				lpr.Status = ERROR
			}
			break
		} else {
			if length == 0 {
				if lpr.Status < END {
					lpr.Status = ERROR
				} else {
					logDebug("File was received!")
				}
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
				lpr.Status = END
				err = lpr.Connection.Close()
				if err != nil {
					logErrorf("Error closing connection: %s", err.Error())
				}
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
	if (lpr.tempFilesize - length) > 0 {
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
		if lpr.Output != nil {
			lpr.Output.Close()
			lpr.Output = nil
		}
		lpr.tempFilesize = lpr.tempFilesize - length
		lpr.Status = END
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
func (lpr *LprConnection) Interpret(data []uint8, length int64) error {
	firstSymbol := data[0]
	switch firstSymbol {
	/* Daemon commands */
	/* 01 - Print any waiting jobs */
	case 0x1:

	/* 02 - Receive a printer job */
	case 0x2:
		lpr.PrqName = string(data[1:length])
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

	return nil
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
		lpr.tempFilesize = lpr.Filesize

		lpr.Output, err = ioutil.TempFile(lpr.daemon.InputFileSaveDir, "")
		if err != nil {
			return fmt.Errorf("Error while creating temporary file at %s! %s", lpr.daemon.InputFileSaveDir, err)
		}

		lpr.SaveName = lpr.Output.Name()
		logDebugf("New data file: %s", lpr.SaveName)

	/* Control file lines */

	/* C - Class for banner page */
	case 'C':
		lpr.ClassName = string(data[1:length])
		logDebugf("Class name: %s", lpr.ClassName)

	/* H - Host name */
	case 'H':
		lpr.Hostname = string(data[1:length])
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
		lpr.JobName = string(data[1:length])
		logDebugf("Job name: %s", lpr.JobName)

	/* L - Print banner page */
	case 'L':
		break

	/* M - Mail When Printed */
	case 'M':
		break

	/* N - Name of source file */
	case 'N':
		lpr.Filename = string(data[1:length])
		logDebugf("Filename: %s", lpr.Filename)

	/* P - User identification */
	case 'P':
		lpr.UserIdentification = string(data[1:length])
		logDebugf("User identification: %s", lpr.UserIdentification)

	/* S - Symbolic link data */
	case 'S':

	/* T - Title for pr */
	case 'T':
		lpr.TitleText = string(data[1:length])
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
