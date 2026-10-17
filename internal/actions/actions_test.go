package actions_test

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/actions"
	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/fail"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func TestHomebotAction(t *testing.T) {
	t.Run("runs a custom action with its arguments and passes its exit code on", func(t *testing.T) {
		r := newLinkedRig(t)
		r.box.Files(map[string]string{"home/.home/manifests/actions/greet.sh": "#!/usr/bin/env bash\necho \"hi $1 from $PWD\"\nexit 3\n"})
		res := r.run("greet", []string{"you"}, nil)
		if !strings.Contains(res.out, "hi you from "+r.path("manifests")) {
			t.Errorf("output %q", res.out)
		}
		if res.code != 3 {
			t.Errorf("exit %d, want 3", res.code)
		}
	})

	t.Run("an unknown action says where to look", func(t *testing.T) {
		r := newRig(t)
		res := r.run("nope", nil, nil)
		if res.code != 1 || !strings.Contains(res.out, `No action named "nope". See: homebot actions`) {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
		var user *fail.UserError
		if err := actions.Run(r.box.Ctx, "nope", nil); !errors.As(err, &user) {
			t.Errorf("err = %T, want a user error", err)
		}
	})

	t.Run("a custom script overrides a built-in, never an internal action", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{
			"home/.home/manifests/actions/brew.sh": "#!/bin/bash\necho custom brew\n",
			"home/.home/manifests/actions/run.sh":  "#!/bin/bash\necho never\n",
		})
		if res := r.run("brew", nil, nil); res.out != "custom brew\n" {
			t.Errorf("brew: %q", res.out)
		}
		if res := r.run("run", []string{"echo internal"}, nil); res.out != "internal\n" {
			t.Errorf("run: %q", res.out)
		}
	})

	// $(pwd) names manifests as HOMEBOT_MANIFESTS does, with the repo behind
	// a link too.
	t.Run("scripts see the HOMEBOT variables and manifests as the working folder", func(t *testing.T) {
		r := newLinkedRig(t)
		r.box.Files(map[string]string{"home/.home/manifests/actions/show.sh": "#!/bin/bash\necho \"$HOMEBOT_ROOT|$HOMEBOT_OS|$HOMEBOT_HOST|$HOMEBOT_MANIFESTS|$HOMEBOT_FURNITURE|$HOMEBOT_AUTOLINK|$HOMEBOT_PRIVATE|$(pwd)\"\n"})
		want := strings.Join([]string{r.box.Root, "linux", "testhost", r.path("manifests"), r.path("furniture"), r.path("autolink"), r.path("private"), r.path("manifests")}, "|") + "\n"
		if res := r.run("show", nil, nil); res.out != want {
			t.Errorf("got %q, want %q", res.out, want)
		}
	})

	t.Run("arguments reach the script untouched", func(t *testing.T) {
		r := newRig(t)
		r.box.Files(map[string]string{"home/.home/manifests/actions/args.sh": "#!/bin/bash\nfor a in \"$@\"; do echo \"[$a]\"; done\n"})
		res := r.run("args", []string{"one", "two words", "--help", "-h", ""}, nil)
		if res.out != "[one]\n[two words]\n[--help]\n[-h]\n[]\n" {
			t.Errorf("got %q", res.out)
		}
	})

	t.Run("a built-in script gets its arguments after the name", func(t *testing.T) {
		r := newRig(t)
		res := r.run("apt-repos", []string{"--export"}, nil)
		if res.code != 2 {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
	})
}

func TestRunScriptSaysWhenThereIsNoScript(t *testing.T) {
	r := newRig(t)
	code, found, err := actions.RunScript(r.box.Ctx, "nope", nil)
	if err != nil || found || code != 0 {
		t.Errorf("got %d, %v, %v", code, found, err)
	}
}

func TestList(t *testing.T) {
	r := newRig(t)
	r.box.Files(map[string]string{
		"home/.home/manifests/actions/greet.sh":     "#!/bin/bash\n# Say hello.\necho hello\n",
		"home/.home/manifests/actions/nosummary.sh": "#!/bin/bash\necho no comment above me\n",
		"home/.home/manifests/actions/brew.sh":      "#!/bin/bash\n# My own brew.\n",
		"home/.home/manifests/actions/link.sh":      "#!/bin/bash\n# Cannot override an internal action.\n",
		"home/.home/manifests/actions/notes.txt":    "not an action",
	})
	list, err := actions.List(r.box.Ctx)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	kinds := map[string]actions.Kind{}
	summaries := map[string]string{}
	for _, a := range list {
		names = append(names, a.Name)
		kinds[a.Name] = a.Kind
		summaries[a.Name] = a.Summary
	}
	wantNames := []string{"apt", "apt-repos", "brew", "flatpak", "fonts", "greet", "link", "nosummary", "run", "unlock"}
	if !slices.Equal(names, wantNames) {
		t.Fatalf("names %v, want %v", names, wantNames)
	}
	for name, want := range map[string]actions.Kind{"apt": actions.Builtin, "brew": actions.Custom, "link": actions.Internal, "run": actions.Internal, "unlock": actions.Internal, "greet": actions.Custom} {
		if kinds[name] != want {
			t.Errorf("%s is %s, want %s", name, kinds[name], want)
		}
	}
	if summaries["greet"] != "Say hello." || summaries["nosummary"] != "" || summaries["brew"] != "My own brew." {
		t.Errorf("summaries %q", summaries)
	}
	if summaries["apt"] != "Install apt packages listed one per line (# comments allowed).   - apt: linux/apt.txt" {
		t.Errorf("apt summary %q", summaries["apt"])
	}
}

func TestListWithoutCustomActionsFolder(t *testing.T) {
	r := newRig(t)
	list, err := actions.List(r.box.Ctx)
	if err != nil || len(list) != 8 {
		t.Errorf("got %d actions, %v", len(list), err)
	}
}

func TestEveryBuiltinScriptHasASummary(t *testing.T) {
	r := newRig(t)
	list, _ := actions.List(r.box.Ctx)
	for _, a := range list {
		if a.Kind == actions.Builtin && a.Summary == "" {
			t.Errorf("%s has no summary", a.Name)
		}
	}
}

func TestInternalSeam(t *testing.T) {
	t.Run("an internal action nobody registered says so", func(t *testing.T) {
		r := newRig(t)
		var user *fail.UserError
		actions.StubInternal(t, "zzunwired", msg.ActionUnlockSummary(), nil)
		err := actions.Run(r.box.Ctx, "zzunwired", nil)
		if !errors.As(err, &user) || !strings.Contains(err.Error(), "zzunwired: this action is not available in this build") {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("a registered action runs with the arguments and its error comes back as it is", func(t *testing.T) {
		r := newRig(t)
		var got []string
		boom := fail.User("boom")
		actions.StubInternal(t, "zzseam", msg.ActionLinkSummary(), func(_ *app.Context, args []string) error {
			got = args
			return boom
		})
		if err := actions.Run(r.box.Ctx, "zzseam", []string{"autolink", "private"}); !errors.Is(err, boom) {
			t.Errorf("err = %v, want the action's own", err)
		}
		if !slices.Equal(got, []string{"autolink", "private"}) {
			t.Errorf("args %v", got)
		}
	})

	t.Run("a new internal action is listed with its summary", func(t *testing.T) {
		r := newRig(t)
		actions.StubInternal(t, "zzfake", msg.ActionUnlockSummary(), func(*app.Context, []string) error { return nil })
		list, err := actions.List(r.box.Ctx)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(list, func(a actions.Info) bool { return a.Name == "zzfake" })
		if i < 0 || list[i].Kind != actions.Internal || !strings.HasPrefix(list[i].Summary, "Decrypt private/") {
			t.Errorf("list = %v", list)
		}
	})
}

func TestInternalRun(t *testing.T) {
	t.Run("runs each command in manifests", func(t *testing.T) {
		r := newLinkedRig(t)
		res := r.run("run", []string{"pwd", "echo $PWD", "echo $HOMEBOT_HOST"}, nil)
		if m := r.path("manifests"); res.code != 0 || res.out != m+"\n"+m+"\ntesthost\n" {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
	})

	t.Run("stops at the first failing command and names it", func(t *testing.T) {
		r := newRig(t)
		res := r.run("run", []string{"echo a", "exit 4", "echo never"}, nil)
		if res.code != 1 || !strings.Contains(res.out, `run: "exit 4" exited with 4`) || strings.Contains(res.out, "never") {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
	})

	t.Run("no commands is fine", func(t *testing.T) {
		r := newRig(t)
		if res := r.run("run", nil, nil); res.code != 0 || res.out != "" {
			t.Errorf("exit %d: %q", res.code, res.out)
		}
	})
}

// catalogInternalActions lists the actions the catalog has an
// action.<name>.summary for.
func catalogInternalActions(t testing.TB) []string {
	re := regexp.MustCompile(`^action\.([a-z][a-z0-9_]*)\.summary$`)
	var out []string
	for _, e := range testutil.Catalog(t) {
		if m := re.FindStringSubmatch(e.ID); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// unwired returns the names that are not internal actions with a body.
func unwired(names []string) []string {
	var missing []string
	for _, n := range names {
		if !actions.Wired(n) {
			missing = append(missing, n)
		}
	}
	return missing
}

// A missing registration cannot slip by: every internal action the catalog
// documents has a Go body. The TypeScript had three, which the catalog must
// still list.
func TestEveryCatalogInternalActionIsWired(t *testing.T) {
	cat := catalogInternalActions(t)
	if missing := unwired(cat); len(missing) > 0 {
		t.Errorf("catalog action summaries without a Go body: %v", missing)
	}
	for _, n := range []string{"link", "unlock", "run"} {
		if !slices.Contains(cat, n) {
			t.Errorf("%s is missing from the catalog list %v", n, cat)
		}
	}
}

func TestWiredCheckFindsAMissingAction(t *testing.T) {
	got := unwired(append(catalogInternalActions(t), "zzextra"))
	if !slices.Equal(got, []string{"zzextra"}) {
		t.Errorf("unwired = %v, want [zzextra]", got)
	}
}

func TestScriptExitIsSilent(t *testing.T) {
	r := newRig(t)
	r.box.Files(map[string]string{"home/.home/manifests/actions/bad.sh": "#!/bin/bash\nexit 3\n"})
	err := actions.Run(r.box.Ctx, "bad", nil)
	var exit *fail.Exit
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Errorf("err = %v, want fail.Silently(3)", err)
	}
}

func TestStubInternalPutsBackWhatWasThere(t *testing.T) {
	r := newRig(t)
	summaryOf := func(name string) (string, bool) {
		list, err := actions.List(r.box.Ctx)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(list, func(a actions.Info) bool { return a.Name == name })
		if i < 0 {
			return "", false
		}
		return list[i].Summary, true
	}
	original, ok := summaryOf("link")
	if !ok || !actions.Wired("link") {
		t.Fatal("premise: link is not a wired internal action")
	}

	t.Run("a stub over a real action", func(t *testing.T) {
		actions.StubInternal(t, "link", msg.ActionUnlockSummary(), nil)
		if actions.Wired("link") {
			t.Error("the stub (no body) did not replace link")
		}
		if got, _ := summaryOf("link"); got == original {
			t.Errorf("the stub's summary is not listed: %q", got)
		}
	})
	if !actions.Wired("link") {
		t.Error("link is not wired again after the stub's test ended")
	}
	if got, _ := summaryOf("link"); got != original {
		t.Errorf("link's summary is %q after the stub, want the original %q", got, original)
	}

	t.Run("a stub of a new name", func(t *testing.T) {
		actions.StubInternal(t, "zzgone", msg.ActionUnlockSummary(), func(*app.Context, []string) error { return nil })
		if !actions.Wired("zzgone") {
			t.Fatal("premise: the stub is not wired")
		}
	})
	if actions.Wired("zzgone") {
		t.Error("zzgone is still wired")
	}
	if _, listed := summaryOf("zzgone"); listed {
		t.Error("zzgone is still listed")
	}
}
