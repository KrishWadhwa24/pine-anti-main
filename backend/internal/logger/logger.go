package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

var Log zerolog.Logger

func Init(level string) {
	lvl, err := zerolog.ParseLevel(level)
	if err != nil {
		lvl = zerolog.InfoLevel
	}

	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	multi := zerolog.MultiLevelWriter(consoleWriter)

	Log = zerolog.New(multi).
		With().
		Timestamp().
		Caller().
		Str("service", "tradenexus").
		Logger().
		Level(lvl)
}

func WithComponent(component string) zerolog.Logger {
	return Log.With().Str("component", component).Logger()
}

func Writer() io.Writer {
	return Log
}
