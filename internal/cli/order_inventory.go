package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperlab-sh/homebot/internal/app"
	"github.com/hyperlab-sh/homebot/internal/exports"
	"github.com/hyperlab-sh/homebot/internal/gitx"
	"github.com/hyperlab-sh/homebot/internal/helper"
	"github.com/hyperlab-sh/homebot/internal/linker"
	"github.com/hyperlab-sh/homebot/internal/msg"
	"github.com/hyperlab-sh/homebot/internal/profile"
	"github.com/hyperlab-sh/homebot/internal/secrets"
	"github.com/hyperlab-sh/homebot/internal/state"
	"github.com/hyperlab-sh/homebot/internal/triage"
	"github.com/hyperlab-sh/homebot/internal/ui"
	"github.com/hyperlab-sh/homebot/internal/voice"
)

func init() { register(KeepingOrder, "inventory", newInventoryCmd) }

func newInventoryCmd(c *app.Context) *cobra.Command {
	return &cobra.Command{
		Use:  "inventory",
		Args: cobra.ArbitraryArgs,
		RunE: func(*cobra.Command, []string) error { return runInventory(c) },
	}
}

// runInventory prints one row per part of this machine's setup: profile,
// links, secrets, triage, repo, exports and the helper. It changes nothing,
// so it does not rebuild the inbox.
func runInventory(c *app.Context) error {
	if err := c.LocateRoot(); err != nil {
		return err
	}
	st := ui.Styles(c)
	s, err := state.Load(c)
	if err != nil {
		return err
	}
	ago := func(ms int64) msg.Text { return ui.Ago(time.UnixMilli(ms), c.Now()) }
	row := func(label msg.Text, value string) {
		padded := fmt.Sprintf("%-9s", voice.Plain(c, label))
		ui.Println(c, voice.Render(c, msg.InventoryRow(st.Accent.Render(padded), value)))
	}
	ui.Println(c, st.Accent.Render(voice.Render(c, msg.InventoryTitle(c.Host, string(c.OS), ui.Tildify(c.Root, c.Home)))))

	profileRow, err := inventoryProfile(c, s, ago)
	if err != nil {
		return err
	}
	row(msg.InventoryLabelProfile(), profileRow)

	plan, err := linker.PlanLinks(c)
	if err != nil {
		return err
	}
	sum := plan.Summary()
	links := voice.Render(c, msg.InventoryLinksValue(sum.OK, sum.Create+sum.Replace+sum.Adopt+sum.Unfold+sum.Prune, sum.Conflict, sum.Locked))
	if sum.Invalid > 0 {
		links += st.Error.Render(voice.Render(c, msg.InventoryLinksInvalid(sum.Invalid)))
	}
	row(msg.InventoryLabelLinks(), links)

	secretsRow, err := inventorySecrets(c)
	if err != nil {
		return err
	}
	row(msg.InventoryLabelSecrets(), secretsRow)

	items, err := triage.Candidates(c)
	if err != nil {
		return err
	}
	ignores, err := triage.LoadIgnores(c)
	if err != nil {
		return err
	}
	row(msg.InventoryLabelTriage(), voice.Render(c, msg.InventoryTriageValue(len(items), len(ignores))))

	repoRow, err := inventoryRepo(c)
	if err != nil {
		return err
	}
	row(msg.InventoryLabelRepo(), repoRow)

	var ex []string
	for _, spec := range exports.ExportsFor(c) {
		r, ok := s.Exports.Get(spec.Name)
		switch {
		case !ok:
			ex = append(ex, voice.Render(c, msg.InventoryExportsNever(spec.Name)))
		case r.OK:
			ex = append(ex, voice.Render(c, msg.InventoryExportsOk(spec.Name, ago(r.At))))
		default:
			line := voice.Render(c, msg.InventoryExportsFailing(spec.Name))
			if i := strings.LastIndexByte(line, ' '); i >= 0 {
				line = line[:i+1] + st.Error.Render(line[i+1:])
			}
			ex = append(ex, line)
		}
	}
	if len(ex) == 0 {
		ex = []string{voice.Render(c, msg.InventoryExportsNone())}
	}
	row(msg.InventoryLabelExports(), strings.Join(ex, ", "))

	if helper.Installed(c) {
		ran := msg.InventoryHelperNeverRan()
		if s.LastTidy != 0 {
			ran = ago(s.LastTidy)
		}
		row(msg.InventoryLabelHelper(), voice.Render(c, msg.InventoryHelperInstalled(ran)))
	} else {
		row(msg.InventoryLabelHelper(), st.Todo.Render(voice.Render(c, msg.InventoryHelperMissing())))
	}
	return nil
}

// inventoryProfile is the Profile row's value: the profile this machine
// unpacks and when it last did, or that none is chosen.
func inventoryProfile(c *app.Context, s *state.State, ago func(int64) msg.Text) (string, error) {
	st := ui.Styles(c)
	name, ok := profile.DefaultProfile(c)
	if !ok {
		return st.Todo.Render(voice.Render(c, msg.InventoryProfileNone())), nil
	}
	status := voice.Render(c, msg.InventoryProfileNever())
	if s.Unpack != nil {
		steps, err := profile.ResolveFor(c, name)
		if err != nil {
			return "", err
		}
		status = voice.Render(c, msg.InventoryProfileUnpacked(ago(s.Unpack.At)))
		if s.Unpack.Hash != profile.StepsHash(steps) {
			status += st.Todo.Render(voice.Render(c, msg.InventoryProfileChanged()))
		}
	}
	return voice.Render(c, msg.InventoryProfileValue(name, status)), nil
}

// inventorySecrets is the Secrets row's value: how many secrets are in each
// state, or that there are no keys or no secrets yet.
func inventorySecrets(c *app.Context) (string, error) {
	if !secrets.KeysExist(c) {
		return ui.Styles(c).Todo.Render(voice.Render(c, msg.InventorySecretsNoKeys())), nil
	}
	counts, err := secrets.Status(c)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, n := range counts {
		parts = append(parts, voice.Render(c, msg.InventorySecretsCount(n.N, string(n.State))))
	}
	if len(parts) == 0 {
		return voice.Render(c, msg.InventorySecretsNone()), nil
	}
	return strings.Join(parts, ", "), nil
}

// inventoryRepo is the Repo row's value: branch, upstream (or why there is
// none), and how far it is from the remote.
func inventoryRepo(c *app.Context) (string, error) {
	st := ui.Styles(c)
	if !gitx.IsRepo(c, c.Root) {
		return st.Error.Render(voice.Render(c, msg.InventoryRepoNotRepo())), nil
	}
	rs, err := gitx.Status(c, c.Root)
	if err != nil {
		return "", err
	}
	branch := rs.Branch
	if branch == "" {
		branch = voice.Plain(c, msg.InventoryRepoDetached())
	}
	var upstream string
	switch {
	case rs.Upstream != "":
		upstream = voice.Render(c, msg.InventoryRepoUpstream(rs.Upstream))
	case strings.TrimSpace(gitx.Run(c, c.Root, "remote").Stdout) != "":
		upstream = st.Todo.Render(voice.Render(c, msg.InventoryRepoNeverPushed()))
	default:
		upstream = st.Todo.Render(voice.Render(c, msg.InventoryRepoNoRemote()))
	}
	return voice.Render(c, msg.InventoryRepoValue(branch, upstream, rs.Ahead, rs.Behind, len(rs.Changed))), nil
}
