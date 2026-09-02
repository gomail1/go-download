package logger

import (
	"os"
	"strings"

	"github.com/sirupsen/logrus"
)

var (
	// Log is the global logger instance
	Log *logrus.Logger
)

// Init initializes the logger with the given level
func Init(level string) {
	// Create a new logger instance
	Log = logrus.New()

	// Set output to stdout
	Log.SetOutput(os.Stdout)

	// Set log format
	Log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "2006-01-02 15:04:05",
	})

	// Set log level
	setLogLevel(level)
}

// setLogLevel sets the log level based on the string input
func setLogLevel(level string) {
	level = strings.ToLower(level)
	switch level {
	case "debug":
		Log.SetLevel(logrus.DebugLevel)
	case "info":
		Log.SetLevel(logrus.InfoLevel)
	case "warn":
		Log.SetLevel(logrus.WarnLevel)
	case "error":
		Log.SetLevel(logrus.ErrorLevel)
	case "fatal":
		Log.SetLevel(logrus.FatalLevel)
	default:
		Log.SetLevel(logrus.InfoLevel)
		Log.Warnf("Unknown log level: %s, using default info level", level)
	}
}

// Debug logs a message at debug level
func Debug(args ...interface{}) {
	Log.Debug(args...)
}

// Debugf logs a formatted message at debug level
func Debugf(format string, args ...interface{}) {
	Log.Debugf(format, args...)
}

// Info logs a message at info level
func Info(args ...interface{}) {
	Log.Info(args...)
}

// Infof logs a formatted message at info level
func Infof(format string, args ...interface{}) {
	Log.Infof(format, args...)
}

// Warn logs a message at warn level
func Warn(args ...interface{}) {
	Log.Warn(args...)
}

// Warnf logs a formatted message at warn level
func Warnf(format string, args ...interface{}) {
	Log.Warnf(format, args...)
}

// Error logs a message at error level
func Error(args ...interface{}) {
	Log.Error(args...)
}

// Errorf logs a formatted message at error level
func Errorf(format string, args ...interface{}) {
	Log.Errorf(format, args...)
}

// Fatal logs a message at fatal level and exits
func Fatal(args ...interface{}) {
	Log.Fatal(args...)
}

// Fatalf logs a formatted message at fatal level and exits
func Fatalf(format string, args ...interface{}) {
	Log.Fatalf(format, args...)
}
