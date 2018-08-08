package lprlib

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/user"
	"path"
)

// LprError This errordomain contains some errors wich may occur when you work with LprSend or LprDaemon
type LprError struct {
	What string
}

func (e *LprError) Error() string {
	return fmt.Sprintf("%s", e.What)
}

// LprSend This struct includes all methods to read a LprSender
// It send files to the remote printer
type LprSend struct {

	/**
	 * This file is sent to the remote printer
	 */
	inputFileName string

	/**
	 * This socket for the connection
	 */
	socket net.Conn

	/**
	 * The max size of one transmit
	 */
	MaxSize uint64

	/**
	 * The configuration for the remote printer
	 */
	Config map[byte]string
}

// Init This Methode initializes the LprSender
// If lpr.MaxSize isn't set yet then it is 16*1024
// The port is per default 515
func (lpr *LprSend) Init(hostname, filePath string, port uint16) error {

	// init const
	if lpr.MaxSize == 0 {
		lpr.MaxSize = 16 * 1024
	}

	// Default port
	if port == 0 {
		port = 515
	}

	/* Set the input_file_name */
	lpr.inputFileName = filePath

	/* Set the basename of the for the config */
	_, inputFileBasename := path.Split(filePath)

	if inputFileBasename == "" {
		return &LprError{"NO_FILE_NAME"}
	}

	/* Initializes the config */
	lpr.Config = make(map[byte]string)

	/* Host name */
	osHostname, err := os.Hostname()
	if err != nil {
		return &LprError{"Can't resolve Hostname"}
	}
	lpr.Config['H'] = osHostname

	/* Name of source file */
	lpr.Config['N'] = inputFileBasename

	/* User identification */
	cuser, err := user.Current()
	if err != nil {
		return &LprError{"Can't resolve Username"}
	}
	lpr.Config['P'] = cuser.Name

	/* Print file with 'pr' format */
	lpr.Config['p'] = "dfA000" + osHostname

	/*
	 * Further configuration:
	 *
	 * lpr.Config['C'] = ""  // Class for banner page
	 * lpr.Config['I'] = ""  // Indent Printing
	 * lpr.Config['J'] = ""  // Job name for banner page
	 * lpr.Config['L'] = ""  // Print banner page
	 * lpr.Config['M'] = ""  // Mail When Printed
	 * lpr.Config['S'] = ""  // Symbolic link data
	 * lpr.Config['T'] = ""  // Title for pr
	 * lpr.Config['U'] = ""  // Unlink data file
	 * lpr.Config['W'] = ""  // Width of output
	 * lpr.Config['1'] = ""  // troff R font
	 * lpr.Config['2'] = ""  // troff I font
	 * lpr.Config['3'] = ""  // troff B font
	 * lpr.Config['4'] = ""  // troff S font
	 * lpr.Config['c'] = ""  // Plot CIF file
	 * lpr.Config['d'] = ""  // Print DVI file
	 * lpr.Config['f'] = ""  // Print formatted file
	 * lpr.Config['g'] = ""  // Plot file
	 * lpr.Config['k'] = ""  // Reserved for use by Kerberized LPR clients and servers
	 * lpr.Config['l'] = ""  // Print file leaving control characters
	 * lpr.Config['n'] = ""  // Print ditroff output file
	 * lpr.Config['o'] = ""  // Print Postscript output file
	 * lpr.Config['r'] = ""  // File to print with FORTRAN carriage control
	 * lpr.Config['t'] = ""  // Print troff output file
	 * lpr.Config['v'] = ""  // Print raster file
	 */

	/* Initializes the socket connection */
	// this.socket = new Socket( SocketFamily.IPV4, SocketType.STREAM, SocketProtocol.TCP );

	/* Set the IP-Address from the remote Server */
	ip, err := GetIP(hostname)
	if err != nil {
		return &LprError{err.Error()}
	}
	/* Connect to Server! */
	ipstring := fmt.Sprintf("%v:%d", ip.IP, port)
	lpr.socket, err = net.Dial("tcp", ipstring)
	if err != nil {
		// handle error
		return &LprError{err.Error()}
	}

	return nil
}

// GetIP Resolve the IP Address from the hostname
func GetIP(hostname string) (*net.IPAddr, error) {

	/* Try to resolve the hostname with default resolver */
	resolver := net.DefaultResolver

	/* Resolve the IP-Addresses */
	addrs, err := resolver.LookupIPAddr(context.Background(), hostname)
	if err != nil {
		return nil, &LprError{"HOSTNAME_NOT_FOUND " + err.Error()}
	}

	/* Get the first IP-Address */
	for _, ia := range addrs {
		return &ia, nil
	}
	return nil, &LprError{"HOSTNAME_NOT_FOUND"}
}

func (lpr *LprSend) writeByte(text []byte) (int, error) {
	return lpr.socket.Write(text)
}

func (lpr *LprSend) writeString(text string) (int, error) {
	btext := []byte(text)
	return lpr.writeByte(btext)
}

// SendConfiguration Sends the configuration to the remote printer
func (lpr *LprSend) SendConfiguration(queue string) error {

	var err error
	/*
	 * Send Directory prefix for the output file
	 * config_transmit is the string which is sent to the remote Server
	 * A config transmit must have a new line command at the ending
	 */
	configTransmit := fmt.Sprintf("%c%s\n", 0x02, queue)
	_, err = lpr.writeString(configTransmit)
	if err != nil {
		return &LprError{"PRINTER_ERROR: " + err.Error()}
	}
	fmt.Print("Start Config:", configTransmit)

	/* receive_buffer is the buffer for the answer of the remote Server */
	receiveBuffer := make([]byte, 1)

	/*
	 * Receive answer ( 0 if there wasn't an error )
	 * length is length of the answer, maximum length is the receive_buffer size
	 */
	var length int
	length, err = lpr.socket.Read(receiveBuffer)
	if length != 0 {
		fmt.Printf("Empfangen: %d\n", receiveBuffer[0])
		if receiveBuffer[0] != 0 {
			errorstring := fmt.Sprint("PRINTER_ERROR Printer reported an error (", receiveBuffer[0], ")!")
			return &LprError{errorstring}
		}
	}

	/* Create config data string */
	var configData string
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
	fmt.Print("Config info:", configInfo)

	/*
	 * Receive answer ( 0 if there wasn't an error )
	 */
	length, err = lpr.socket.Read(receiveBuffer)
	if length != 0 {
		fmt.Printf("Empfangen: %d\n", receiveBuffer[0])
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
	fmt.Print("Config:\n", configData)

	/*
	 * Receive answer ( 0 if there wasn't an error )
	 */
	length, err = lpr.socket.Read(receiveBuffer)
	if length != 0 {
		fmt.Printf("Empfangen: %d\n", receiveBuffer[0])
		if receiveBuffer[0] != 0 {
			errorstring := fmt.Sprint("PRINTER_ERROR Printer reported an error (", receiveBuffer[0], ")!")
			return &LprError{errorstring}
		}
	}

	return nil
}

// SendFile Sends the file to the remote printer
func (lpr *LprSend) SendFile() error {

	/* Prepare the input file for reading */
	file, err := os.Open(lpr.inputFileName)
	if err != nil {
		return &LprError{"Can't open File: " + lpr.inputFileName}
	}

	/* Get the size of the input file */
	var fileInfo os.FileInfo
	fileInfo, err = os.Stat(lpr.inputFileName)
	if err != nil {
		return &LprError{"Can't open File: " + lpr.inputFileName}
	}

	fileSize := fileInfo.Size()

	if fileSize <= 0 {
		return &LprError{"Can't read file"}
	}

	/* Host name */
	osHostname, err := os.Hostname()
	if err != nil {
		return &LprError{"Can't resolve Hostname"}
	}

	/* Send the server the length of the input file */
	dataInfo := fmt.Sprintf("%c%d dfA000%s\n", 0x03, fileSize, osHostname)
	_, err = lpr.writeString(dataInfo)
	if err != nil {
		return &LprError{"PRINTER_ERROR: " + err.Error()}
	}
	fmt.Print("\nData info:", dataInfo)

	/* receive_buffer is the buffer for the answer of the remote Server */
	receiveBuffer := make([]byte, 1)

	/*
	 * Receive answer ( 0 if there wasn't an error )
	 */
	var length int
	length, err = lpr.socket.Read(receiveBuffer)
	if length != 0 {
		fmt.Printf("Empfangen: %d\n", receiveBuffer[0])
		if receiveBuffer[0] != 0 {
			errorstring := fmt.Sprint("PRINTER_ERROR Printer reported an error (", receiveBuffer[0], ")!")
			return &LprError{errorstring}
		}
	}

	/*
	 * Send the server the input file
	 * size of one transmit
	 */
	size := lpr.MaxSize

	var rsize int

	/* position of the file */
	var position uint64

	/* file_buffer is a part of the input_file */
	var fileBuffer []byte

	var prozent float32
	fmt.Println("Last transmit:")
	for size == lpr.MaxSize {
		fileBuffer = make([]byte, lpr.MaxSize)

		rsize, err = file.Read(fileBuffer)
		if err != nil {
			return &LprError{"Can't read File: " + lpr.inputFileName}
		}
		if rsize > 0 {
			size = uint64(rsize)
		}

		if size < lpr.MaxSize {
			fileBuffer[size] = 0
			size++
		}

		_, err = lpr.writeByte(fileBuffer[:size])
		if err != nil {
			return &LprError{"PRINTER_ERROR: " + err.Error()}
		}

		position += size

		prozent = float32(position*100) / float32(fileSize+1)
		fmt.Printf("Send file part: Position=%d, Size=%5d (%2.2f%%)\r", position, size, prozent)

		// fmt.Printf("%v  len:%d cap:%d rsize:%d size:%d    \n", fileBuffer, len(fileBuffer), cap(fileBuffer), rsize, size)
		// time.Sleep(10000 * time.Millisecond)

	}
	fmt.Printf("\n")

	/*
	 * Receive answer ( 0 if there wasn't an error )
	 */
	length, err = lpr.socket.Read(receiveBuffer)
	if length != 0 {
		fmt.Printf("Empfangen: %d\n", receiveBuffer[0])
		if receiveBuffer[0] != 0 {
			errorstring := fmt.Sprint("PRINTER_ERROR Printer reported an error (", receiveBuffer[0], ")!")
			return &LprError{errorstring}
		}
	}

	return nil
}

// Close Close the connection to the remote printer
func (lpr *LprSend) Close() error {
	return lpr.socket.Close()
}
