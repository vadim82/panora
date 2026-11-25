package cli

import (
	"context"
	"os"
	"testing"
)

// The bundled provider executes the same binary. Test binaries dispatch that
// child invocation through the real CLI, without helper environment variables.
func TestMain(m *testing.M) {
	// Race-instrumented plugin children should exit promptly between operations.
	_ = os.Setenv("GORACE", os.Getenv("GORACE")+" atexit_sleep_ms=0")
	if len(os.Args) >= 2 && os.Args[1] == "provider" {
		if err := NewRootCommand().ExecuteContext(context.Background()); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
