package voice

import (
	"fmt"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// SpeakAs makes every Context speak as p until t ends.
//
// It swaps a process-wide variable, so no test that runs beside others may
// use it: a parallel test would have its words changed by, or change the
// words of, a test running at the same time. The guard is t.Setenv, which Go
// refuses in a parallel test and which makes a later t.Parallel panic, so a
// test that calls SpeakAs and runs in parallel (in either order) panics
// instead of racing.
func SpeakAs(t testing.TB, p Persona) {
	t.Helper()
	guardNotParallel(t)
	old := testSpeaker
	testSpeaker = func(*app.Context) Persona { return p }
	t.Cleanup(func() { testSpeaker = old })
}

func guardNotParallel(t testing.TB) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			panic(fmt.Sprintf("voice.SpeakAs sets process-wide state and cannot be used in a parallel test: %v", r))
		}
	}()
	t.Setenv("HOMEBOT_TEST_SPEAKAS", "1")
}
