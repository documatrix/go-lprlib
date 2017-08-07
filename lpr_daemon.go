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

// LprDaemon structure
type LprDaemon struct {

	/* All connections */
	connections []*LprConnection

	/* Used for closing the Listener */
	closing chan bool

	socket net.Listener
}

// Init is the constructor
// port ist the tcp port where the deamon should listen default 515
// ipAddress of the deamon default own ip
func (lpr *LprDaemon) Init(port uint16, ipAddress string) error {

	if port == 0 {
		port = 515
	}

	lpr.closing = make(chan bool, 1)

	if ipAddress == "" {
		as, err := net.InterfaceAddrs()
		if err != nil {
			return &LprError{"Can't load interfaces " + err.Error()}
		}
		if len(as) <= 0 {
			return &LprError{"No interfaces found"}
		}

		var found bool
		var ip net.IP
		for _, iaddr := range as {

			switch v := iaddr.(type) {
			case *net.IPNet:
				ip = v.IP
				found = true
			case *net.IPAddr:
				ip = v.IP
				found = true
			}
			if found == true {
				break
			}
		}

		ipAddress = ip.String()
	}

	addr, err := net.ResolveIPAddr("", ipAddress)
	if err != nil {
		return &LprError{"Can't resolve IP-Address: " + ipAddress}
	}
	fmt.Printf("\n\nYour IP-Adresse: %s\n", addr.IP.String())

	listenAddr := fmt.Sprintf("%s:%d", addr.IP.String(), port)

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

		fmt.Println("Wait for Connections...")
		newConn, err := lpr.socket.Accept()

		select {
		case <-lpr.closing:
			if newConn != nil {
				newConn.Close()
			}
			fmt.Println("Listener closed")
			return
		default:
		}
		if err != nil {
			fmt.Println("Can't accept: " + err.Error())
		}
		fmt.Println("Accepted Client")

		var newLprcon LprConnection
		newLprcon.Init(newConn, 0)

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
		if bufferConnections[i].Status != END && bufferConnections[i].Status != int16(ERROR) {
			lpr.connections = append(lpr.connections, bufferConnections[i])
		}
	}
}

// GetConnections returns all LprConnections
func (lpr *LprDaemon) GetConnections() []*LprConnection {
	return lpr.connections
}

const (
	// BEGIN start to read data
	BEGIN int16 = 0

	// FILEDATA in filedata - block right now
	FILEDATA int16 = 2

	// DATABLOCK in datablock-block right now
	DATABLOCK int16 = 3

	// END end of the data
	END int16 = 4

	// ERROR Error
	ERROR uint8 = 0xff
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
	Status int16

	// PrintFileWithPr Print file with pr
	PrintFileWithPr string

	// SaveName The File name of the new file
	SaveName string

	// closing Used for Closing the LprConnection
	closing chan bool
}

// Init is the constructor of LprConnection
// socet is the accepted connection
// bufferSize is per default 8192
func (lpr *LprConnection) Init(socket net.Conn, bufferSize int64) {
	if bufferSize == 0 {
		bufferSize = 8192
	}
	lpr.Connection = socket
	lpr.BufferSize = bufferSize
	lpr.closing = make(chan bool, 1)
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
	for lpr.Status != int16(ERROR) {
		buffer = make([]uint8, lpr.BufferSize)
		length = 0

		length, err = lpr.Connection.Read(buffer)
		select {
		case <-lpr.closing:
			lpr.Status = int16(ERROR)
			if lpr.Output != nil {
				lpr.Output.Close()
			}
			fmt.Println("Exit Connection")
			return
		default:
		}

		if err != nil && err != io.EOF {
			fmt.Printf("\nReading Buffer Failed: %s\n", err.Error())
			lpr.Status = int16(ERROR)
		} else {
			if length == 0 {
				if lpr.Status < END {
					lpr.Status = int16(ERROR)
				} else {
					fmt.Printf("\nFile was received!\n")
				}
				break
			}

			if length == -1 {
				fmt.Printf("\nFile could not be received!\n")
				lpr.Status = int16(ERROR)
				break
			}

			if lpr.Status == DATABLOCK {
				inData = true
			} else {
				inData = false
			}

			lpr.HandleData(buffer, int64(length))

			one := []byte{}
			if _, err = lpr.Connection.Read(one); err == io.EOF {
				fmt.Printf("\nConnection is closed.\n")
				lpr.Connection.Close()
				lpr.Connection = nil
				if lpr.Status < END {
					lpr.Status = int16(ERROR)
				}
				break
			} else {

				if lpr.Status != DATABLOCK || !inData {
					_, err = lpr.Connection.Write([]byte{0})
					if err != nil {
						fmt.Printf("\nSending Failed: %s\n", err.Error())
						lpr.Status = int16(ERROR)
					}
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
			if len(iv) > 0 {
				lpr.Interprete([]byte(iv), int64(len(iv)))
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
		test = data[0:length]
		_, err = lpr.Output.Write(test)
		if err != nil {
			fmt.Printf("\nWrite failed: %s\n", err.Error())
		}
	} else {
		test = data[:lpr.tempFilesize]
		_, err = lpr.Output.Write(test)
		if err != nil {
			fmt.Printf("\nWrite failed: %s\n", err.Error())
			return
		}
		if lpr.Output != nil {
			lpr.Output.Close()
			lpr.Output = nil
		}
		lpr.tempFilesize = lpr.tempFilesize - length
		lpr.Status = END
	}

	pro := float32(100.0) - float32(lpr.tempFilesize*100)/float32(lpr.Filesize)
	fmt.Print("<")
	for i := 0; i < 50; i++ {
		if int(pro/2) < i {
			fmt.Print(" ")
		} else {
			fmt.Print("-")
		}
	}
	fmt.Printf("> %f %%\r", pro)
}

// Interprete This method interpret the data and set the variables
func (lpr *LprConnection) Interprete(data []uint8, length int64) error {
	var err error
	var tstring string
	firstSymbol := data[0]
	switch firstSymbol {
	/* Daemon commands */
	/* 01 - Print any waiting jobs */
	case 0x1:

	case 0x2:
		lpr.bufferString = nil
		isPrqName := true
		for i := int64(1); i < length; i++ {
			if data[i] == ' ' {
				isPrqName = false
			}
			lpr.bufferString = append(lpr.bufferString, data[i])
		}
		lpr.bufferString = append(lpr.bufferString, 0)

		if isPrqName {
			/* Receive a printer job */
			lpr.PrqName = string(lpr.bufferString[:len(lpr.bufferString)])
		} else {
			/* Receive control file */
		}

	/* 03 - Send queue state (short) */
	case 0x3:
		lpr.Status = DATABLOCK
		lpr.bufferString = nil
		for i := int64(1); i < length && data[i] != ' '; i++ {
			lpr.bufferString = append(lpr.bufferString, data[i])
		}
		tstring = string(lpr.bufferString)
		lpr.Filesize, err = strconv.ParseInt(tstring, 10, 64)
		if err != nil {
			return err
		}
		fmt.Printf("Filesize: %d\n", lpr.Filesize)
		lpr.tempFilesize = lpr.Filesize
		lpr.Output, err = ioutil.TempFile("", "")
		if err != nil {
			return err
		}
		lpr.SaveName = lpr.Output.Name()
		fmt.Printf("New Data File: %s\n", lpr.SaveName)

	/* 04 - Send queue state (long) */
	case 0x4:

	/* 05 - Remove jobs */
	case 0x5:

	/* Control file lines */

	/* C - Class for banner page */
	case 'C':
		lpr.ClassName = string(data[1:length])
		fmt.Printf("Class name: %s\n", lpr.ClassName)

	/* H - Host name */
	case 'H':
		lpr.Hostname = string(data[1:length])
		fmt.Printf("\nHostname: %s\n", lpr.Hostname)

	/* I - Indent Printing */
	case 'I':
		lpr.bufferString = nil
		for i := int64(1); i < length && data[i] != ' '; i++ {
			lpr.bufferString = append(lpr.bufferString, data[i])
		}
		lpr.bufferString = append(lpr.bufferString, 0)
		tstring = string(lpr.bufferString)
		lpr.IntentingCount, err = strconv.ParseInt(tstring, 10, 64)
		if err != nil {
			return err
		}
		fmt.Printf("indenting_count: %d\n", lpr.IntentingCount)

	/* J - Job name for banner page */
	case 'J':
		lpr.JobName = string(data[1:length])
		fmt.Printf("\nJob name: %s\n", lpr.JobName)

	/* L - Print banner page */
	case 'L':
		break

	/* M - Mail When Printed */
	case 'M':
		break

	/* N - Name of source file */
	case 'N':
		lpr.Filename = string(data[1:length])
		fmt.Printf("\nFilename: %s\n", lpr.Filename)

	/* P - User identification */
	case 'P':
		lpr.UserIdentification = string(data[1:length])
		fmt.Printf("\nUser Identification: %s\n", lpr.UserIdentification)

	/* S - Symbolic link data */
	case 'S':

	/* T - Title for pr */
	case 'T':
		lpr.TitleText = string(data[1:length])
		fmt.Printf("\nTitle Text: %s\n", lpr.TitleText)

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
		lpr.bufferString = nil
		for i := int64(1); i < length && data[i] != ' '; i++ {
			lpr.bufferString = append(lpr.bufferString, data[i])
		}
		lpr.bufferString = append(lpr.bufferString, 0)
		lpr.PrintFileWithPr = string(lpr.bufferString)
		fmt.Printf("p: %s\n", lpr.PrintFileWithPr)

	/* r - File to print with FORTRAN carriage control */
	case 'r':

	/* t - Print troff output file */
	case 't':

	/* v - Print raster file */
	case 'v':

	case 0x00:

	default:
		fmt.Printf("First Element: %02x (%c)\n", data[0], data[0])
		fmt.Printf("Unknown Code: %s\n", string(data[:length]))
		break

	}
	return nil
}
