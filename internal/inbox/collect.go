package inbox

import (
	"slices"
	"strings"
	"time"

	"github.com/hyperlab-sh/homebot/internal/cleanup"
	"github.com/hyperlab-sh/homebot/internal/config"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/guard"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/proc"
	"github.com/hyperlab-sh/homebot/internal/profile"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/triage"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

// checks are the inbox's checks in src/inbox.ts's order.
var checks = []check{
	{run: identityItems},
	{run: signingKeyItems},
	{run: gitconfigItems},
	{run: blockedItems},
	{run: tidyErrorItems},
	{run: repoItems},
	{id: "secrets", retry: args("lock"), run: secretItems},
	{id: "links", retry: args("link", "--dry-run"), run: linkItems},
	{id: "triage", retry: args("triage"), run: triageItems},
	{id: "git", retry: func(e *env) *Fix { return shell("git -C " + e.repo + " status") }, run: gitItems},
	{id: "profile", retry: args("unpack", "--dry-run"), run: profileItems},
	{run: exportItems},
	{id: "name", retry: args("name"), run: nameItems},
	{run: personaBrokenItems},
	{run: helperItems},
	{run: binaryPathItems},
	{run: updateItems},
}

func personaBrokenItems(e *env) ([]Item, error) {
	p := configProblem(e.c)
	if p == nil {
		return nil, nil
	}
	id := msg.StripUnsafe(p.ID)
	it := e.item("persona_broken", Todo, msg.InboxPersonaBrokenTitle(id), &Fix{Args: []string{"persona", "validate", p.ID}})
	it.Detail = e.plain(msg.InboxPersonaBrokenDetail(id))
	return []Item{it}, nil
}

func args(a ...string) func(*env) *Fix {
	return func(*env) *Fix { return &Fix{Args: a} }
}

func shell(line string) *Fix { return &Fix{Shell: line} }

// older says at, in JavaScript milliseconds, is set and more than d ago.
func (e *env) older(at int64, d time.Duration) bool {
	return at != 0 && e.ms()-at > d.Milliseconds()
}

func (e *env) plain(t msg.Text) string { return voice.Plain(e.c, t) }

func (e *env) item(id string, level Level, title msg.Text, fix *Fix) Item {
	return Item{ID: id, Level: level, Title: e.plain(title), Fix: fix}
}

func (e *env) ago(at int64) msg.Text { return ui.Ago(time.UnixMilli(at), e.now) }

// firstThree is up to three of names joined with ", ", and ", ..." when
// more were left out and more says so.
func firstThree(names []string, more bool) string {
	text := strings.Join(names[:min(3, len(names))], ", ")
	if more && len(names) > 3 {
		text += ", ..."
	}
	return text
}

// identityItems comes before "A commit was refused": a missing identity is
// the usual reason on a new machine.
func identityItems(e *env) ([]Item, error) {
	who := gitx.Identity(e.c, e.c.Root)
	if who.Name != "" && who.Email != "" {
		return nil, nil
	}
	it := e.item("no-identity", Urgent, msg.InboxNoIdentityTitle(), shell(e.plain(msg.GitIdentityFix())))
	it.Detail = e.plain(msg.InboxNoIdentityDetail())
	return []Item{it}, nil
}

func signingKeyItems(e *env) ([]Item, error) {
	key := gitx.MissingSigningKey(e.c, e.c.Root)
	if key == "" {
		return nil, nil
	}
	it := e.item("no-signing-key", Urgent, msg.InboxNoSigningKeyTitle(ui.Tildify(key, e.c.Home)), shell(e.plain(msg.GitSigningOff())))
	it.Detail = e.plain(msg.InboxNoSigningKeyDetail())
	return []Item{it}, nil
}

func gitconfigItems(e *env) ([]Item, error) {
	if gitx.ShadowingGitconfig(e.c) == "" {
		return nil, nil
	}
	it := e.item("gitconfig-shadow", Todo, msg.InboxGitconfigShadowTitle(), shell(e.plain(msg.GitGitconfigAside())))
	it.Detail = e.plain(msg.InboxGitconfigShadowDetail())
	return []Item{it}, nil
}

func blockedItems(e *env) ([]Item, error) {
	if e.st.Blocked == nil {
		return nil, nil
	}
	it := e.item("blocked", Urgent, msg.InboxBlockedTitle(), &Fix{Args: []string{"save"}})
	it.Detail = e.st.Blocked.Reason
	return []Item{it}, nil
}

func tidyErrorItems(e *env) ([]Item, error) {
	var items []Item
	for _, t := range e.st.TidyErrors {
		if t.Step == "autosave" && e.st.Blocked != nil {
			continue
		}
		it := e.item("tidy:"+t.Step, Urgent, msg.InboxTidyFailedTitle(t.Step, e.ago(t.At)), &Fix{Args: []string{"tidy"}})
		it.Detail = t.Message
		items = append(items, it)
	}
	return items, nil
}

// repoItems checks the repo itself, gitleaks and the commit guard. The guard
// counts as installed only when guard.Installed says its shim is all there
// (plan 12.3), where the TypeScript looked at core.hooksPath alone.
func repoItems(e *env) ([]Item, error) {
	if !e.repoOK {
		return []Item{e.item("no-repo", Urgent, msg.InboxNoRepoTitle(e.repo), shell("git -C "+e.repo+" init"))}, nil
	}
	var items []Item
	if !proc.HasCommand(e.c, "gitleaks") {
		items = append(items, e.item("no-gitleaks", Urgent, msg.InboxNoGitleaksTitle(), shell("brew install gitleaks")))
	}
	if !guard.Installed(e.c) {
		items = append(items, e.item("no-guard", Urgent, msg.InboxNoGuardTitle(), &Fix{Args: []string{"setup"}}))
	}
	return items, nil
}

// secretItems: Go seals with its own age library, so the TypeScript's
// no-age item is gone.
func secretItems(e *env) ([]Item, error) {
	if !secrets.KeysExist(e.c) {
		return []Item{e.item("no-keys", Todo, msg.InboxNoKeysTitle(), &Fix{Args: []string{"keys", "init"}})}, nil
	}
	counts, err := secrets.Status(e.c)
	if err != nil {
		return nil, err
	}
	unlock := func() *Fix { return &Fix{Args: []string{"unlock"}} }
	var items []Item
	if n := counts.Of(secrets.Conflict); n > 0 {
		items = append(items, e.item("secret-conflict", Urgent, msg.InboxSecretConflictTitle(n), unlock()))
	}
	if n := counts.Of(secrets.Locked); n > 0 {
		items = append(items, e.item("secret-locked", Todo, msg.InboxSecretLockedTitle(n), unlock()))
	}
	if n := counts.Of(secrets.Upstream) + counts.Of(secrets.Unverified); n > 0 {
		items = append(items, e.item("secret-upstream", Todo, msg.InboxSecretUpstreamTitle(n), unlock()))
	}
	if n := counts.Of(secrets.New) + counts.Of(secrets.Modified); n > 0 {
		items = append(items, e.item("secret-unsealed", Todo, msg.InboxSecretUnsealedTitle(n), &Fix{Args: []string{"lock"}}))
	}
	return items, nil
}

func linkItems(e *env) ([]Item, error) {
	plan, err := linker.PlanLinks(e.c)
	if err != nil {
		return nil, err
	}
	var items []Item
	if invalid := plan.Only(linker.Invalid); len(invalid) > 0 {
		var reasons []string
		for _, op := range invalid[:min(2, len(invalid))] {
			reasons = append(reasons, e.plain(op.ReasonText))
		}
		it := e.item("link-invalid", Urgent, msg.InboxLinkInvalidTitle(len(invalid)), &Fix{Args: []string{"link", "--dry-run"}})
		it.Detail = e.plain(msg.InboxLinkInvalidDetail(strings.Join(reasons, "; ")))
		items = append(items, it)
	}
	if conflicts := plan.Only(linker.Conflict); len(conflicts) > 0 {
		var targets []string
		for _, op := range conflicts {
			targets = append(targets, ui.Tildify(op.Target, e.c.Home))
		}
		it := e.item("link-conflict", Todo, msg.InboxLinkConflictTitle(len(conflicts)), &Fix{Args: []string{"link"}})
		it.Detail = e.plain(msg.InboxLinkConflictDetail(firstThree(targets, false)))
		items = append(items, it)
	}
	if n := len(plan.Pending()); n > 0 {
		items = append(items, e.item("link-pending", Todo, msg.InboxLinkPendingTitle(n), &Fix{Args: []string{"link"}}))
	}
	backups, err := cleanup.FindBackupsIn(e.c, plan)
	if err != nil {
		return items, err
	}
	if len(backups) > 0 {
		var paths []string
		for _, b := range backups {
			paths = append(paths, ui.Tildify(b.Path, e.c.Home))
		}
		it := e.item("backups", Todo, msg.InboxBackupsTitle(len(backups)), &Fix{Args: []string{"cleanup"}})
		it.Detail = e.plain(msg.InboxBackupsDetail(firstThree(paths, true)))
		items = append(items, it)
	}
	return items, nil
}

func triageItems(e *env) ([]Item, error) {
	cands, err := triage.Candidates(e.c)
	if err != nil || len(cands) == 0 {
		return nil, err
	}
	var keys []string
	for _, x := range cands {
		keys = append(keys, x.Key)
	}
	it := e.item("triage", Todo, msg.InboxTriageTitle(len(cands)), &Fix{Args: []string{"triage"}})
	it.Detail = e.plain(msg.InboxTriageDetail(firstThree(keys, true)))
	return []Item{it}, nil
}

func gitItems(e *env) ([]Item, error) {
	if !e.repoOK {
		return nil, nil
	}
	c := e.c
	st, err := gitx.Status(c, c.Root)
	if err != nil {
		return nil, err
	}
	save := func() *Fix { return &Fix{Args: []string{"save"}} }
	var items []Item
	hasRemote := strings.TrimSpace(gitx.Run(c, c.Root, "remote").Stdout) != ""
	switch {
	case !hasRemote:
		items = append(items, e.item("no-remote", Todo, msg.InboxNoRemoteTitle(), &Fix{Args: []string{"remote"}}))
	case st.Upstream == "" && gitx.HasCommits(c, c.Root):
		it := e.item("never-pushed", Todo, msg.InboxNeverPushedTitle(), &Fix{Args: []string{"remote"}})
		it.Detail = strings.TrimSpace(gitx.Run(c, c.Root, "config", "--get", "remote.origin.url").Stdout)
		items = append(items, it)
	case e.older(e.st.LastSync, 3*24*time.Hour):
		items = append(items, e.item("unreachable", Todo, msg.InboxUnreachableTitle(e.ago(e.st.LastSync)), save()))
	}
	switch {
	case st.Ahead > 0 && st.Behind > 0:
		items = append(items, e.item("diverged", Urgent, msg.InboxDivergedTitle(), shell("git -C "+e.repo+" pull --rebase")))
	case st.Ahead > 0:
		items = append(items, e.item("unpushed", Todo, msg.InboxUnpushedTitle(st.Ahead), save()))
	case st.Behind > 0:
		items = append(items, e.item("behind", Todo, msg.InboxBehindTitle(st.Behind), save()))
	}
	cfg := c.Config
	if cfg == nil {
		cfg = config.Default()
	}
	autosaving := cfg.Autosave.Commit != config.CadenceOff
	stale := e.st.LastAutosave == 0 || e.older(e.st.LastAutosave, 2*24*time.Hour)
	if len(st.Changed) > 0 && (!autosaving || stale) {
		items = append(items, e.item("uncommitted", Todo, msg.InboxUncommittedTitle(len(st.Changed)), save()))
	}
	return items, nil
}

func profileItems(e *env) ([]Item, error) {
	c := e.c
	name, ok := profile.DefaultProfile(c)
	if !ok || name == "" {
		it := e.item("no-profile", Todo, msg.InboxNoProfileTitle(c.Host), shell("$EDITOR "+e.repo+"/homebot.yml"))
		it.Detail = e.plain(msg.InboxNoProfileDetail())
		return []Item{it}, nil
	}
	if !slices.Contains(profile.ListProfiles(c), name) {
		it := e.item("missing-profile", Todo, msg.InboxMissingProfileTitle(name), shell("$EDITOR "+e.repo+"/manifests/profiles/"+name+".yml"))
		it.Detail = e.plain(msg.InboxMissingProfileDetail())
		return []Item{it}, nil
	}
	steps, err := profile.ResolveFor(c, name)
	if err != nil {
		return nil, err
	}
	hash := profile.StepsHash(steps)
	unpack := func() *Fix { return &Fix{Args: []string{"unpack"}} }
	var it Item
	switch last := e.st.Unpack; {
	case last == nil || last.Profile != name:
		other := ""
		if last != nil {
			other = e.plain(msg.InboxNeverUnpackedLast(last.Profile))
		}
		it = e.item("never-unpacked", Todo, msg.InboxNeverUnpackedTitle(name, other), unpack())
	case last.Hash != hash:
		it = e.item("profile-changed", Todo, msg.InboxProfileChangedTitle(name), unpack())
	default:
		return nil, nil
	}
	it.Detail = e.plain(msg.InboxUnpackPreview())
	return []Item{it}, nil
}

func exportItems(e *env) ([]Item, error) {
	var items []Item
	for _, spec := range exports.ExportsFor(e.c) {
		r, ok := e.st.Exports.Get(spec.Name)
		if !ok || r.OK {
			continue
		}
		it := e.item("export:"+spec.Name, Todo, msg.InboxExportFailingTitle(spec.Name), &Fix{Args: []string{"export", spec.Name}})
		it.Detail = r.Error
		items = append(items, it)
	}
	return items, nil
}

// helperItems: the TypeScript's no-agent and agent-stale are no-helper and
// helper-stale. helper-no-linger is new (plan 12.2): on a headless Linux
// machine without lingering, systemd stops the helper's timer at logout.
func helperItems(e *env) ([]Item, error) {
	setup := func() *Fix { return &Fix{Args: []string{"setup"}} }
	if !helper.Installed(e.c) {
		it := e.item("no-helper", Todo, msg.InboxNoHelperTitle(), setup())
		it.Detail = e.plain(msg.InboxNoHelperDetail())
		return []Item{it}, nil
	}
	var items []Item
	if e.older(e.st.LastTidy, 6*time.Hour) {
		items = append(items, e.item("helper-stale", Todo, msg.InboxHelperStaleTitle(e.ago(e.st.LastTidy)), setup()))
	}
	if helper.NeedsLinger(e.c) {
		it := e.item("helper-no-linger", Todo, msg.InboxHelperNoLingerTitle(), shell("loginctl enable-linger $USER"))
		it.Detail = e.plain(msg.InboxHelperNoLingerDetail())
		items = append(items, it)
	}
	return items, nil
}
