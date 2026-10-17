package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot"
	"github.com/hyperlab-sh/homebot/internal/app"
)

var (
	ErrTimeout         = errors.New("upgrade timed out")
	ErrNotYetAvailable = errors.New("release is not yet available in Homebrew")
	ErrBrokenInstall   = errors.New("Homebrew install is broken: brew reinstall --cask hyperlab-sh/tap/homebot")
	ErrInstall         = errors.New("installer failed")
	ErrPlatform        = errors.New("unsupported platform")
	ErrDownload        = errors.New("download failed")
	ErrCosign          = errors.New("cosign unavailable")
	ErrVerify          = errors.New("release verification failed")
	ErrSmoke           = errors.New("new binary failed smoke test")
	ErrRefused         = errors.New("installer refused")
	ErrNotNewer        = errors.New("target release is not newer than the running version")
)

// InstallScript is the root installer embedded into this binary. Tests may
// replace it with a stub to cover exit codes and timeouts.
var InstallScript = homebot.InstallScript

var termGrace = 30 * time.Second

var allowedEnv = []string{
	"PATH", "HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	"HTTPS_PROXY", "HTTP_PROXY", "NO_PROXY", "https_proxy", "http_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR",
}

func childEnv(c *app.Context, brew, plain bool) []string {
	var env []string
	for _, key := range allowedEnv {
		if value, ok := c.Env.Lookup(key); ok {
			env = append(env, key+"="+value)
		}
	}
	if brew {
		env = append(env, "HOMEBREW_NO_INSTALL_CLEANUP=1", "HOMEBREW_NO_ENV_HINTS=1", "HOMEBREW_NO_ANALYTICS=1")
	}
	if plain {
		env = append(env, "HOMEBOT_PLAIN=1")
	}
	return env
}

func runChild(ctx context.Context, argv []string, stdin io.Reader, env []string) (int, string, bool, error) {
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return 0, "", false, errors.New("upgrade command must be absolute")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = env
	cmd.Stdin = stdin
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	cmd.WaitDelay = termGrace
	if err := cmd.Start(); err != nil {
		return 0, "", false, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return exitCode(err), output.String(), false, err
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		timer := time.NewTimer(termGrace)
		defer timer.Stop()
		select {
		case <-done:
		case <-timer.C:
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		// A child may outlive a shell that exited on TERM. Kill the group too.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		return 0, output.String(), true, ctx.Err()
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 1
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

func scriptError(code int) error {
	switch code {
	case 2:
		return ErrPlatform
	case 3:
		return ErrDownload
	case 4:
		return ErrCosign
	case 5:
		return ErrVerify
	case 6:
		return ErrSmoke
	case 7:
		return ErrRefused
	default:
		return ErrInstall
	}
}

// RunUnattended executes only the cask and signed-script methods. It never
// invokes sudo, and its child sees only the explicit environment allowlist.
func RunUnattended(ctx context.Context, c *app.Context, u Upgrade) error {
	brew := u.Method == Cask
	if u.Method == Deb || u.Method == RPM {
		return ErrNeedsRoot
	}
	if !brew && u.Method != Script {
		return ErrManual
	}
	// The cosign identity pins the workflow and tag, so an older genuine
	// release still verifies. Refuse anything that is not a strict upgrade
	// before spawning a child, and fail closed on unparseable versions.
	newer, err := Newer(u.Current, u.Version)
	if err != nil {
		return err
	}
	if !newer {
		return fmt.Errorf("%w: running %s, target %s", ErrNotNewer, u.Current, u.Version)
	}
	argv := append([]string(nil), u.Argv...)
	var stdin io.Reader
	if u.Method == Script {
		if len(InstallScript) == 0 {
			return fmt.Errorf("%w: embedded install.sh unavailable", ErrInstall)
		}
		stdin = bytes.NewReader(InstallScript)
		if u.BaseURL != "" && testing.Testing() {
			argv = append(argv, "--base-url", u.BaseURL)
		}
	}
	code, output, timedOut, err := runChild(ctx, argv, stdin, childEnv(c, brew, false))
	if timedOut {
		if brew {
			checkCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _, _, checkErr := runChild(checkCtx, []string{u.Binary, "version"}, nil, childEnv(c, false, true))
			cancel()
			if checkErr != nil {
				return errors.Join(ErrTimeout, ErrBrokenInstall)
			}
		}
		return ErrTimeout
	}
	if err != nil {
		kind := ErrInstall
		if u.Method == Script {
			kind = scriptError(code)
		}
		return fmt.Errorf("%w: %s", kind, firstLine(output))
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	var foundOutput string
	_, foundOutput, timedOut, err = runChild(checkCtx, []string{u.Binary, "version"}, nil, childEnv(c, false, true))
	cancel()
	if timedOut {
		if brew {
			return errors.Join(ErrTimeout, ErrBrokenInstall)
		}
		return ErrTimeout
	}
	if err != nil {
		kind := ErrInstall
		if brew {
			kind = ErrBrokenInstall
		}
		return errors.Join(kind, err)
	}
	found := strings.TrimSpace(foundOutput)
	if _, err := parseVersion(found); err != nil {
		return fmt.Errorf("%w: invalid post-check version %q", ErrInstall, found)
	}
	if u.Found != nil {
		*u.Found = found
	}
	if brew {
		newer, err := Newer(found, u.Version)
		if err != nil {
			return err
		}
		if newer {
			return fmt.Errorf("%w: found %s", ErrNotYetAvailable, found)
		}
		return nil
	}
	if found != u.Version {
		return fmt.Errorf("%w: found %s, want %s", ErrInstall, found, u.Version)
	}
	return nil
}

// CosignMin reads the minimum from the exact installer bytes this binary runs.
func CosignMin() string {
	for _, line := range strings.Split(string(InstallScript), "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), "COSIGN_MIN="); ok {
			return strings.Trim(value, "'\"")
		}
	}
	return ""
}
