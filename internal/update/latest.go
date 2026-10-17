package update

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/hyperlab-sh/homebot/internal/app"
)

const releaseBase = "https://github.com/hyperlab-sh/homebot"
const testReleasesURL = "HOMEBOT_TEST_RELEASES_URL"

var ErrNoRelease = errors.New("no release found")

// UserAgentVersion is filled from the caller's build stamp when the CLI
// integrates this package. Go-installed binaries fall back to build info.
var UserAgentVersion string

type Release struct {
	Version string // without the leading v
	Tag     string // v-prefixed tag as published
	URL     string
}

func releasesBase(c *app.Context) string {
	if testing.Testing() {
		if raw := c.Env.Get(testReleasesURL); raw != "" {
			return strings.TrimRight(raw, "/")
		}
	}
	return releaseBase
}

func userAgent() string {
	v := UserAgentVersion
	if v == "" {
		v = "dev"
	}
	if UserAgentVersion == "" {
		if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
			v = strings.TrimPrefix(info.Main.Version, "v")
		}
	}
	return "homebot/" + v
}

// Latest reads the release redirect once; it never follows an untrusted URL.
func Latest(c *app.Context) (Release, error) {
	base := releasesBase(c)
	client := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: http.ProxyFromEnvironment}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(c.Context(), http.MethodGet, base+"/releases/latest", nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("User-Agent", userAgent())
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return Release{}, ErrNoRelease
	}
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return Release{}, fmt.Errorf("latest release: unexpected HTTP %d", resp.StatusCode)
	}
	location := resp.Header.Get("Location")
	u, err := url.Parse(location)
	if err != nil || u.RawQuery != "" || u.Fragment != "" {
		return Release{}, fmt.Errorf("latest release: invalid redirect %q", location)
	}
	want, err := url.Parse(base + "/releases/tag/")
	if err != nil {
		return Release{}, err
	}
	if u.Host != "" && (u.Scheme != want.Scheme || !strings.EqualFold(u.Host, want.Host)) {
		return Release{}, fmt.Errorf("latest release: foreign redirect %q", location)
	}
	path := u.Path
	if !strings.HasPrefix(path, want.Path) {
		return Release{}, fmt.Errorf("latest release: invalid tag redirect %q", location)
	}
	tag := strings.TrimPrefix(path, want.Path)
	if !strings.HasPrefix(tag, "v") || strings.Contains(tag, "/") {
		return Release{}, fmt.Errorf("latest release: invalid tag %q", tag)
	}
	if _, err := parseVersion(tag); err != nil {
		return Release{}, err
	}
	return Release{Version: strings.TrimPrefix(tag, "v"), Tag: tag, URL: base + "/releases/tag/" + tag}, nil
}
