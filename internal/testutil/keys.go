package testutil

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

const (
	// TestPassphrase seals the private key TestKeys makes.
	TestPassphrase = "correct horse battery staple"
	// TestWorkFactor is the scrypt work factor TestKeys seals with, low so
	// tests stay fast (age's default is 18).
	TestWorkFactor = 10
)

// TestKeys gives the home repo at root a key pair, made in-process, as
// homebot keys init would: private/recipients.txt holds the public key and
// private/identity.age the private key, sealed with TestPassphrase at
// TestWorkFactor. It returns the private key file's text.
func TestKeys(t testing.TB, root string) string {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("testutil: %v", err)
	}
	identity := "# public key: " + id.Recipient().String() + "\n" + id.String() + "\n"
	r, err := age.NewScryptRecipient(TestPassphrase)
	if err != nil {
		t.Fatalf("testutil: %v", err)
	}
	r.SetWorkFactor(TestWorkFactor)
	var sealed bytes.Buffer
	w, err := age.Encrypt(&sealed, r)
	if err == nil {
		_, err = w.Write([]byte(identity))
	}
	if err == nil {
		err = w.Close()
	}
	if err != nil {
		t.Fatalf("testutil: %v", err)
	}
	private := filepath.Join(root, "private")
	if err := os.MkdirAll(private, 0o755); err != nil {
		t.Fatalf("testutil: %v", err)
	}
	for name, data := range map[string][]byte{
		"recipients.txt": []byte(id.Recipient().String() + "\n"),
		"identity.age":   sealed.Bytes(),
	} {
		if err := os.WriteFile(filepath.Join(private, name), data, 0o644); err != nil {
			t.Fatalf("testutil: %v", err)
		}
	}
	return identity
}
