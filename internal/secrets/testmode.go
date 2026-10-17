package secrets

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/hyperlab-sh/homebot/internal/app"
)

// inTest reports whether this binary was built by "go test". It is a
// variable only so tests can play a release build.
var inTest = testing.Testing

// testPassphrase returns the passphrase HOMEBOT_TEST_PASSPHRASE gives, if
// any. It answers every passphrase prompt in place of the terminal. A
// release build ignores it: testing.Testing is false there.
func testPassphrase(env app.Env) (string, bool) {
	v, ok := env.Lookup("HOMEBOT_TEST_PASSPHRASE")
	if !ok || !inTest() {
		return "", false
	}
	return v, true
}

// testWorkFactor returns the scrypt work factor HOMEBOT_TEST_SCRYPT_WORK_FACTOR
// asks keys init to seal the private key with, if any, so tests stay fast.
// A release build ignores it.
func testWorkFactor(env app.Env) (int, bool, error) {
	v, ok := env.Lookup("HOMEBOT_TEST_SCRYPT_WORK_FACTOR")
	if !ok || !inTest() {
		return 0, false, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 30 {
		return 0, false, fmt.Errorf("HOMEBOT_TEST_SCRYPT_WORK_FACTOR is not a work factor from 1 to 30: %q", v)
	}
	return n, true, nil
}
