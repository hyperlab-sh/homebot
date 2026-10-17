package save_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/save"
	"github.com/hyperlab-sh/homebot/internal/testutil"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "guard", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func refusal(t *testing.T, output string) (string, []string) {
	t.Helper()
	sb := testutil.Sandbox(t, testutil.Options{})
	return save.Refusal(sb.Ctx, output)
}

const (
	plaintext = "Commit refused: private/a is plaintext. Only *.age files may be committed from private/. Unstage it:\n  git rm --cached private/a"
	bigFile   = "  - big.bin is 60 MB (limit 50 MB). Unstage it and add it to .gitignore."
	gitleaks  = "Commit refused: gitleaks found something that looks like a secret. Move it to private/ (homebot pack --private <path>) or unstage it."
)

func TestRefusalTheGuardsFirstProblemAndItsFixAreTheReasonTheOthersAreDetails(t *testing.T) {
	reason, details := refusal(t, fixture(t, "refusal-two.txt"))
	eq(t, reason, plaintext, "reason")
	eq(t, details, []string{bigFile}, "details")
}

func TestRefusalGitleaksFindingsStayUnderItsProblem(t *testing.T) {
	reason, details := refusal(t, fixture(t, "refusal-gitleaks.txt"))
	eq(t, reason, gitleaks+"\n  Finding:     token = REDACTED\n  File:        autolink/common/zshrc", "reason")
	eq(t, len(details), 0, "details")
}

// The TypeScript test's own fixture indented the findings by six spaces.
func TestRefusalGitleaksFindingsWithTheTypeScriptIndent(t *testing.T) {
	guard := "homebot refused this commit:\n  - gitleaks found something that looks like a secret. Move it to private/ (homebot pack --private <path>) or unstage it.\n      Finding: token = REDACTED\n      File: config.toml\n"
	reason, details := refusal(t, guard)
	eq(t, reason, gitleaks+"\n    Finding: token = REDACTED\n    File: config.toml", "reason")
	eq(t, len(details), 0, "details")
}

func TestRefusalWithoutTheGuardWhatGitSaid(t *testing.T) {
	reason, details := refusal(t, fixture(t, "refusal-nobullet.txt"))
	eq(t, reason, "Commit refused: gpg failed to sign the data", "reason")
	eq(t, details, []string{"  failed to write commit object"}, "details")

	reason, details = refusal(t, "")
	eq(t, reason, "Commit refused: git gave no reason", "reason")
	eq(t, len(details), 0, "details")
}

func TestRefusalEdgeCases(t *testing.T) {
	cases := []struct {
		name, output, reason string
		details              []string
	}{
		{
			"colour codes are stripped",
			"\x1b[1m\x1b[31mhomebot refused this commit:\x1b[0m\n  - \x1b[31mbig.bin\x1b[0m is 60 MB (limit 50 MB).\n",
			"Commit refused: big.bin is 60 MB (limit 50 MB).", nil,
		},
		{
			"the headline may carry the speaker prefix of an error line",
			"homebot: homebot refused this commit:\n  - a is bad.\n",
			"Commit refused: a is bad.", nil,
		},
		{
			"the headline may be a persona's name",
			"Tester refused this commit:\n  - a is bad.\n",
			"Commit refused: a is bad.", nil,
		},
		{
			"the headline without its colon",
			"homebot refused this commit\n  - a is bad.\n",
			"Commit refused: a is bad.", nil,
		},
		{
			"blank lines are dropped, also between a problem and its fix",
			"\n\nhomebot refused this commit:\n\n  - a is bad. Fix it:\n\n    do-this\n\n  - b is bad.\n\n",
			"Commit refused: a is bad. Fix it:\n  do-this", []string{"  - b is bad."},
		},
		{
			"lines before the first bullet are dropped",
			"hint: something git said\nhomebot refused this commit:\n  - a is bad.\n",
			"Commit refused: a is bad.", nil,
		},
		{
			"a problem's continuation line that is not under a bullet's indent loses only two characters",
			"  - a is bad.\nx\n  - b.\n",
			"Commit refused: a is bad.\n", []string{"  - b."},
		},
		{
			"three problems: the rest are all details, in order",
			"  - a.\n  - b.\n    b-fix\n  - c.\n",
			"Commit refused: a.", []string{"  - b.", "    b-fix", "  - c."},
		},
		{
			"no bullet: only the last three lines git said count",
			"error: one\nfatal: two\nthree\nfour\n",
			"Commit refused: two", []string{"  three", "  four"},
		},
		{
			"no bullet and no final newline",
			"hook failed",
			"Commit refused: hook failed", nil,
		},
		{
			"a bullet that ends like the headline is a problem, not the headline",
			"  - a refused this commit\n",
			"Commit refused: a refused this commit", nil,
		},
		{
			"a bullet needs its two-space indent",
			"- not a bullet\n",
			"Commit refused: - not a bullet", nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reason, details := refusal(t, c.output)
			eq(t, reason, c.reason, "reason")
			eq(t, len(details), len(c.details), "details")
			for i := range c.details {
				eq(t, details[i], c.details[i], "detail")
			}
		})
	}
}
