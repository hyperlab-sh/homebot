# The image scripts/release-test.sh runs the release checks in (goreleaser
# snapshots, package and install-script tests). The script tags it by a hash of
# this file, so any edit here builds a new image on first use.
#
# Not the official goreleaser image: its v2.18.3 tag is built on
# golang:1.27.1-alpine, and this repository builds with Go 1.27.2 on glibc.
#
# The build context is empty (the script feeds this file on stdin), so every
# tool comes from a pinned download. Each download is checked against a sha256
# taken from the project's own published checksum file (shellcheck publishes
# none; its sha256 was computed from the release asset). goreleaser is checked
# the strong way: its checksums.txt against the signature bundle it ships,
# with the cosign binary pinned above, and the archive against checksums.txt.
FROM golang:1.27.2-trixie@sha256:47667934874913a449b96faa7ef4777fa285d9ec29126ee80ed61c45b7cccf0e

ARG GORELEASER_VERSION=2.18.3
ARG SYFT_VERSION=1.54.1
ARG COSIGN_VERSION=3.1.3
ARG ACTIONLINT_VERSION=1.7.12
ARG SHELLCHECK_VERSION=0.10.0

# trixie, not bookworm: homebot needs git 2.40+ (cat-file -Z); bookworm has 2.39.
# mandoc, dash, busybox, zsh, fish, bash-completion: the shells and man tooling
# the completion, man page and install-script tests run. python3: http.server
# for the fake release. rpm: rpm -qp. file, jq: the archive checks.
# docker-cli: the client the sibling-container helpers (lib.sh) use through the
# Docker socket release-test.sh mounts; there is no daemon in the image.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      git ca-certificates curl xz-utils \
      mandoc dash busybox zsh fish bash-completion python3 file jq rpm ruby \
      docker-cli \
 && rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    case "$(dpkg --print-architecture)" in \
      amd64) cosign=amd64; cosign_sha=4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71; \
             syft=amd64; syft_sha=c069905b391cc4c20a5ba65ad5c10be2a7ba074f8ea6ad203e24d14e303dad47; \
             gr=x86_64; \
             al=amd64; al_sha=8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8; \
             sc=x86_64; sc_sha=6c881ab0698e4e6ea235245f22832860544f17ba386442fe7e9d629f8cbedf87 ;; \
      arm64) cosign=arm64; cosign_sha=c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a; \
             syft=arm64; syft_sha=dfdf0537610113edbefe1f1fc6548bc957b2d77439636ec824fcf0e10d46d054; \
             gr=arm64; \
             al=arm64; al_sha=325e971b6ba9bfa504672e29be93c24981eeb1c07576d730e9f7c8805afff0c6; \
             sc=aarch64; sc_sha=324a7e89de8fa2aed0d0c28f3dab59cf84c6d74264022c00c22af665ed1a09bb ;; \
      *) echo "unsupported architecture" >&2; exit 1 ;; \
    esac; \
    tmp="$(mktemp -d)"; cd "$tmp"; \
    # cosign first: it verifies goreleaser below.
    curl -sSfL -o cosign "https://github.com/sigstore/cosign/releases/download/v${COSIGN_VERSION}/cosign-linux-${cosign}"; \
    echo "${cosign_sha}  cosign" | sha256sum -c -; \
    install -m 0755 cosign /usr/local/bin/cosign; \
    # goreleaser: checksums.txt against its cosign bundle, then the archive against checksums.txt.
    base="https://github.com/goreleaser/goreleaser/releases/download/v${GORELEASER_VERSION}"; \
    curl -sSfL -O "${base}/checksums.txt" -O "${base}/checksums.txt.sigstore.json" -O "${base}/goreleaser_Linux_${gr}.tar.gz"; \
    cosign verify-blob \
      --certificate-identity "https://github.com/goreleaser/goreleaser/.github/workflows/release.yml@refs/tags/v${GORELEASER_VERSION}" \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com \
      --bundle checksums.txt.sigstore.json checksums.txt; \
    grep " goreleaser_Linux_${gr}.tar.gz\$" checksums.txt | sha256sum -c -; \
    tar -xzf "goreleaser_Linux_${gr}.tar.gz" -C /usr/local/bin goreleaser; \
    curl -sSfL -o syft.tgz "https://github.com/anchore/syft/releases/download/v${SYFT_VERSION}/syft_${SYFT_VERSION}_linux_${syft}.tar.gz"; \
    echo "${syft_sha}  syft.tgz" | sha256sum -c -; \
    tar -xzf syft.tgz -C /usr/local/bin syft; \
    curl -sSfL -o actionlint.tgz "https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_linux_${al}.tar.gz"; \
    echo "${al_sha}  actionlint.tgz" | sha256sum -c -; \
    tar -xzf actionlint.tgz -C /usr/local/bin actionlint; \
    curl -sSfL -o shellcheck.txz "https://github.com/koalaman/shellcheck/releases/download/v${SHELLCHECK_VERSION}/shellcheck-v${SHELLCHECK_VERSION}.linux.${sc}.tar.xz"; \
    echo "${sc_sha}  shellcheck.txz" | sha256sum -c -; \
    tar -xJf shellcheck.txz --strip-components=1 -C /usr/local/bin "shellcheck-v${SHELLCHECK_VERSION}/shellcheck"; \
    cd /; rm -rf "$tmp" /root/.sigstore; \
    goreleaser --version; syft version; cosign version; actionlint -version; shellcheck --version; \
    git --version; zsh --version; fish --version; command -v mandoc dash zsh fish rpm file jq python3 busybox; docker --version

# A non-root user, like the docker-test image. /release is the mount point of
# the per-run volume: a new named volume copies the owner of the directory it is
# mounted on, so the volume starts writable for tester (and for the root
# siblings). The cache directories are the mount points of the two cache volumes.
RUN useradd --create-home --uid 1000 --shell /bin/bash tester \
 && mkdir -p /home/tester/go/pkg/mod /home/tester/.cache /src /release \
 && chown -R tester:tester /home/tester /src /release

# GOPRIVATE: a stray `go get`, `go run` or `go install` of this module goes
# direct and fails instead of reaching proxy.golang.org, which would record the
# version permanently (nothing public before the gated release steps).
ENV HOME=/home/tester \
    GOPATH=/home/tester/go \
    GOMODCACHE=/home/tester/go/pkg/mod \
    GOCACHE=/home/tester/.cache/go-build \
    GOTOOLCHAIN=local \
    GOPRIVATE=github.com/hyperlab-sh/* \
    RT_VOL=/release

USER tester
WORKDIR /src
