package ui_test

import (
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func TestStyledTextPrintsPlainInTheSandbox(t *testing.T) {
	sb := testutil.Sandbox(t, testutil.Options{})
	ctx := sb.Ctx
	p := ui.Styles(ctx)
	// The inbox render builds the "{bot} · {host} · " prefix; this test does it by hand.
	header := "homebot · box · " + voice.Render(ctx, msg.InboxHeaderCount(3))
	command := ui.Command(ctx, voice.Render(ctx, msg.FormatCommand("homebot save")))
	alarm := p.Alarm.Render("Refused: " + voice.Render(ctx, msg.PackErrorMissing("~/x")))
	if !strings.Contains(header+command+alarm, "\x1b[") {
		t.Fatal("nothing was styled; the test proves nothing")
	}

	ui.Println(ctx, header)
	ui.Println(ctx, command, "and", alarm)
	if _, err := ui.Out(ctx).Write([]byte(p.OK.Render("done") + "\n")); err != nil {
		t.Fatal(err)
	}
	ui.Eprintln(ctx, alarm)
	if _, err := ui.Err(ctx).Write([]byte(p.Muted.Render("muted") + "\n")); err != nil {
		t.Fatal(err)
	}

	wantOut := "homebot · box · 3 things need you\n  homebot save and Refused: ~/x does not exist.\ndone\n"
	if got := sb.Stdout.String(); got != wantOut {
		t.Errorf("stdout = %q, want %q", got, wantOut)
	}
	if got, want := sb.Stderr.String(), "Refused: ~/x does not exist.\nmuted\n"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestTheWritersAreBuiltOncePerContext(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	if ui.Out(ctx) != ui.Out(ctx) || ui.Err(ctx) != ui.Err(ctx) || ui.Out(ctx) == ui.Err(ctx) {
		t.Error("Out and Err should each be one writer per Context")
	}
}

func TestAttachedHandsAChildTheContextsOwnStreams(t *testing.T) {
	ctx := testutil.Sandbox(t, testutil.Options{}).Ctx
	stdin, stdout, stderr := ui.Attached(ctx)
	if stdin != ctx.Stdin || stdout != ctx.Stdout || stderr != ctx.Stderr {
		t.Error("Attached should return the Context's streams themselves")
	}
	if stdout == ui.Out(ctx) || stderr == ui.Err(ctx) {
		t.Error("Attached should not return the colour writers")
	}
}
