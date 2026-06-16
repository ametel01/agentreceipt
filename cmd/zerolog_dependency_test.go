package cmd

import (
	"io"
	"testing"

	"github.com/rs/zerolog"
)

func TestZerologDependencyAvailable(t *testing.T) {
	t.Parallel()

	logger := zerolog.New(io.Discard)
	logger.Info().Str("provider", "codex").Msg("dependency compile check")
}
