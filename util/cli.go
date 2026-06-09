package util

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	CLIColorOrange       = "\033[38;5;208m"
	CLIColorTeal         = "\033[38;5;80m"
	CLIColorBrightYellow = "\033[38;5;227m"
	CLIColorReset        = "\033[0m"
)

var (
	// DebugMode enables verbose [FROM] prefix and error detail in log output.
	// Set from smminer.go's DEBUG_MODE constant at startup.
	DebugMode bool
	// LogToFile controls whether log entries are written to the log file on disk.
	// Set from smminer.go's LOG_TO_FILE constant at startup.
	LogToFile bool

	appLog   []string
	logMutex sync.Mutex // Mutex for thread-safe logging
	logFile  = "log.txt"
)

func AppLog(from string, message string, err string) {
	timeStr := time.Now().Format("2006-01-02 15:04:05")

	// Create log entry
	var logEntry string
	if DebugMode && err != "" {
		logEntry = fmt.Sprintf("[%s][%s] %s\n%s\n", timeStr, from, message, err)
		fmt.Print(logEntry)
	} else if !DebugMode {
		logEntry = fmt.Sprintf("[%s] %s\n", timeStr, message)
		fmt.Print(logEntry)
	} else {
		logEntry = fmt.Sprintf("[%s][%s] %s\n", timeStr, from, message)
		fmt.Print(logEntry)
	}

	// Add to in-memory log
	logMutex.Lock()
	appLog = append(appLog, logEntry)
	logMutex.Unlock()

	if LogToFile {
		writeToLogFile(logEntry)
	}
}

func AppDebug(from string, message string, err string) {
	timeStr := time.Now().Format("2006-01-02 15:04:05")

	if DebugMode {
		logEntry := fmt.Sprintf("[%s][%s] %s %s\n", timeStr, from, message, err)
		fmt.Print(logEntry)

		// Add to in-memory log
		logMutex.Lock()
		appLog = append(appLog, logEntry)
		logMutex.Unlock()

		if LogToFile {
			writeToLogFile(logEntry)
		}
	}
}

// writeToLogFile writes a log entry to the log file
func writeToLogFile(logEntry string) {
	logMutex.Lock()
	defer logMutex.Unlock()

	// Open or create log file (append mode)
	file, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		println("Failed to open log file:", err)
		return
	}
	defer file.Close()

	// Write the log entry
	if _, err := file.WriteString(logEntry); err != nil {
		println("Failed to write to log file:", err)
	}
}

// ClearLogFile clears the log file contents
func ClearLogFile() error {
	logMutex.Lock()
	defer logMutex.Unlock()

	// Truncate the file
	file, err := os.OpenFile(logFile, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	file.Close()

	// Clear in-memory log
	appLog = []string{}

	return nil
}

// GetAppLog returns a copy of the in-memory log
func GetAppLog() []string {
	logMutex.Lock()
	defer logMutex.Unlock()

	// Return a copy to avoid concurrent access issues
	logCopy := make([]string, len(appLog))
	copy(logCopy, appLog)
	return logCopy
}

// SetLogFilePath changes the log file location
func SetLogFilePath(path string) {
	logMutex.Lock()
	defer logMutex.Unlock()
	logFile = path
}
