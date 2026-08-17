// Package logging provides structured JSON logging (zerolog) so platform
// logs can be shipped into the same Wazuh/ELK stack the SOC module uses.
package logging

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

func New(level, env, service string) zerolog.Logger {
	lvl, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil {
		lvl = zerolog.InfoLevel
	}
	zerolog.TimeFieldFormat = time.RFC3339Nano

	var l zerolog.Logger
	if env == "development" {
		l = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05"})
	} else {
		l = zerolog.New(os.Stdout)
	}
	return l.Level(lvl).With().Timestamp().Str("service", service).Logger()
}
