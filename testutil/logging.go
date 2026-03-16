package testutil

import (
	"log/slog"
	"testing"
)

func GetTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.DiscardHandler)
}
