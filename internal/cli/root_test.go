package cli

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestRootGivesCommandsTheCancellableContext(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	c := sb.Ctx
	root := NewRoot(c, Build{Version: "test"})
	var r proc.Result
	root.AddCommand(&cobra.Command{
		Use: "slow",
		RunE: func(*cobra.Command, []string) error {
			r = proc.Run(c, []string{"sleep", "30"}, proc.Options{})
			return nil
		},
	})
	root.SetArgs([]string{"slow"})

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	done := make(chan error, 1)
	go func() { done <- root.ExecuteContext(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the command kept running after its context was cancelled")
	}
	if c.Context() != ctx {
		t.Error("the root did not hand the command's context to app.Context")
	}
	if r.Code != 1 || r.TimedOut {
		t.Errorf("Run = %+v, want code 1 from the cancel", r)
	}
}
