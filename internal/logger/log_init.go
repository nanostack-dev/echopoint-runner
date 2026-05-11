package logger

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// Format LogFormat represents the log output format.
type Format string

const (
	// JSON outputs logs in JSON format (production-ready, default).
	JSON Format = "json"
	// HUMAN outputs logs in human-readable format (development).
	HUMAN Format = "human"
)

// InitLogger initializes the global zerolog logger with the specified level and format.
// By default, uses JSON format. Set format to "human" for human-readable output.
// Use this function to configure logging during initialization.
func InitLogger(level zerolog.Level, format Format) {
	zerolog.SetGlobalLevel(level)

	switch format {
	case HUMAN:
		//nolint:reassign // reassigning log.Logger is intentional here
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr}).
			Level(level).
			With().
			Caller().
			Logger()
	case JSON:
		fallthrough
	default:
		//nolint:reassign // reassigning log.Logger is intentional here
		log.Logger = log.Output(os.Stderr).
			Level(level).
			With().
			Caller().
			Logger()
	}
}

// SetDebugLogging enables debug level logging in human-readable format.
// Useful for troubleshooting flow execution.
func SetDebugLogging() {
	InitLogger(zerolog.DebugLevel, HUMAN)
}

// SetInfoLogging sets logging to info level in human-readable format.
func SetInfoLogging() {
	InitLogger(zerolog.InfoLevel, HUMAN)
}

// SetDebugLoggingJSON enables debug level logging in JSON format.
func SetDebugLoggingJSON() {
	InitLogger(zerolog.DebugLevel, JSON)
}

// SetInfoLoggingJSON sets logging to info level in JSON format (production-ready).
func SetInfoLoggingJSON() {
	InitLogger(zerolog.InfoLevel, JSON)
}
