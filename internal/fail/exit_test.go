package fail

import (
	"errors"
	"fmt"
	"testing"
)

func TestExit(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", Silently(3))
	var exit *Exit
	if !errors.As(err, &exit) {
		t.Fatal("errors.As does not find the *Exit")
	}
	if exit.Code != 3 {
		t.Errorf("Code = %d, want 3", exit.Code)
	}
	if got := Silently(1).Error(); got != "exit status 1" {
		t.Errorf("Error() = %q", got)
	}
}
