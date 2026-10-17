# The image scripts/docker-test.sh runs homebot's test gate in. The script tags
# it by a hash of this file, so any edit here builds a new image on first use.
# Every tool is pinned to the version .github/workflows/ci.yml uses (or, for
# tools CI takes from apt or setup actions, a fixed release).
FROM golang:1.27.2-trixie

ARG AGE_VERSION=v1.3.2
ARG GITLEAKS_VERSION=8.30.1
ARG GOLANGCI_LINT_VERSION=2.14.0
ARG SHELLCHECK_VERSION=0.10.0

# trixie, not bookworm: homebot needs git 2.40+ (cat-file -Z); bookworm has 2.39.
# git, zsh and the usual tools the tests exec (cp, cat, ls, test, sh come with
# the base). xz-utils only unpacks the shellcheck archive below.
RUN apt-get update \
 && apt-get install -y --no-install-recommends git zsh ca-certificates curl xz-utils \
 && rm -rf /var/lib/apt/lists/*

# age 1.3's age-plugin-batchpass is not in apt, so age comes from source, as in CI.
RUN GOBIN=/usr/local/bin GOTOOLCHAIN=local GOFLAGS=-mod=mod \
    go install \
      filippo.io/age/cmd/age@${AGE_VERSION} \
      filippo.io/age/cmd/age-keygen@${AGE_VERSION} \
      filippo.io/age/cmd/age-plugin-batchpass@${AGE_VERSION} \
 && rm -rf /go/pkg /root/.cache

RUN set -eux; \
    case "$(dpkg --print-architecture)" in \
      amd64) gitleaks=x64;   lint=amd64; sc=x86_64 ;; \
      arm64) gitleaks=arm64; lint=arm64; sc=aarch64 ;; \
      *) echo "unsupported architecture" >&2; exit 1 ;; \
    esac; \
    tmp="$(mktemp -d)"; cd "$tmp"; \
    curl -sSfL "https://github.com/gitleaks/gitleaks/releases/download/v${GITLEAKS_VERSION}/gitleaks_${GITLEAKS_VERSION}_linux_${gitleaks}.tar.gz" \
      | tar -xz -C /usr/local/bin gitleaks; \
    curl -sSfL "https://github.com/golangci/golangci-lint/releases/download/v${GOLANGCI_LINT_VERSION}/golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${lint}.tar.gz" \
      | tar -xz --strip-components=1 -C /usr/local/bin "golangci-lint-${GOLANGCI_LINT_VERSION}-linux-${lint}/golangci-lint"; \
    curl -sSfL "https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK_VERSION}/shellcheck-v${SHELLCHECK_VERSION}.linux.${sc}.tar.xz" \
      | tar -xJ --strip-components=1 -C /usr/local/bin "shellcheck-v${SHELLCHECK_VERSION}/shellcheck"; \
    cd /; rm -rf "$tmp"; \
    git --version; zsh --version; age --version; age-keygen --version; age-plugin-batchpass -h >/dev/null 2>&1 || true; \
    gitleaks version; golangci-lint --version; shellcheck --version

# A non-root user: several tests skip as root. The cache directories exist and
# belong to it so the named volumes the script mounts on them start writable.
RUN useradd --create-home --uid 1000 --shell /bin/bash tester \
 && mkdir -p /home/tester/go/pkg/mod /home/tester/.cache /src \
 && chown -R tester:tester /home/tester /src

ENV HOME=/home/tester \
    GOPATH=/home/tester/go \
    GOMODCACHE=/home/tester/go/pkg/mod \
    GOCACHE=/home/tester/.cache/go-build \
    GOLANGCI_LINT_CACHE=/home/tester/.cache/golangci-lint \
    GOTOOLCHAIN=local \
    CI=true

USER tester
WORKDIR /src
