package testutil

import (
	"os"
	"testing"

	"github.com/rs/zerolog"
)

func GetTestLogger(t *testing.T) zerolog.Logger {
	t.Helper()
	return zerolog.New(os.Stderr).With().Logger().Level(zerolog.Disabled)
}
