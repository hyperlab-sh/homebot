//go:build !linux && !darwin

package secrets

import "errors"

// echoOff is not supported here: homebot runs on Linux and macOS.
func echoOff(int) (func() error, error) {
	return nil, errors.ErrUnsupported
}
