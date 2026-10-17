// Package layout names the home repo's folders and files, relative to the
// repo root and slash-separated. It imports nothing, so every package can use
// the same names: app.Layout joins them to the root, config checks export
// outputs against them, the linker names its trees with them.
package layout

const (
	// ConfigFile is the file that marks a directory as a home repo.
	ConfigFile = "homebot.yml"
	// Autolink holds the linked layers, one folder each (common, linux,
	// macos).
	Autolink = "autolink"
	// Private holds the secrets: plaintext ignored by git, sealed .age
	// files committed.
	Private = "private"
	// Manifests holds profiles, custom actions and the exports' snapshots.
	Manifests = "manifests"
	// Profiles is where unpack reads the profiles.
	Profiles = Manifests + "/profiles"
	// CustomActions is where the person's own actions live.
	CustomActions = Manifests + "/actions"
	// Furniture holds files copied, not linked, into $HOME.
	Furniture = "furniture"
	// Decisions records triage's answers.
	Decisions = "decisions.yml"
	// Identity is the private key, sealed with a passphrase.
	Identity = Private + "/identity.age"
	// Recipients lists the public keys secrets are sealed to.
	Recipients = Private + "/recipients.txt"
	// Vendored is the folder homes vendored the TypeScript build into,
	// where its hooks lived.
	Vendored = "homebot"
	// Git is git's own folder.
	Git = ".git"
	// Gitignore and Gitattributes are the repo's own git files.
	Gitignore     = ".gitignore"
	Gitattributes = ".gitattributes"
)

// Owned are the repo paths that homebot or git own whole: nothing else may
// replace them or write inside them. Manifests is not among them, since the
// exports' snapshots go in it; only the folder itself is owned (see
// OwnedFolder).
var Owned = []string{
	Autolink, Profiles, CustomActions, Furniture, Vendored, Private, Git,
	ConfigFile, Decisions, Gitignore, Gitattributes,
}

// OwnedFolder is the folder homebot owns but whose contents are partly the
// person's: replacing it is refused, writing inside it is not.
const OwnedFolder = Manifests
