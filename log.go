package lprlib

import (
	"fmt"
	"log"
)

type Logger func(...interface{})

var (
	debugLog Logger
	errorLog Logger = log.Print
)

// SetDebugLogger sets logger as debug logging function
func SetDebugLogger(logger Logger) {
	debugLog = logger
}

// SetErrorLogger sets logger as error logging function
func SetErrorLogger(logger Logger) {
	errorLog = logger
}

func logDebug(v ...interface{}) {
	if debugLog != nil {
		debugLog(v...)
	}
}

func logDebugf(format string, v ...interface{}) {
	if debugLog != nil {
		debugLog(fmt.Sprintf(format, v...))
	}
}

func logError(v ...interface{}) {
	if errorLog != nil {
		errorLog(v...)
	}
}

func logErrorf(format string, v ...interface{}) {
	if errorLog != nil {
		errorLog(fmt.Sprintf(format, v...))
	}
}
