# docs/

Everything served at https://homebot.sh:

| Path | What | Source |
|---|---|---|
| `/` | the homepage | `landing/` (hand-written HTML and CSS) |
| `/docs/` | the docs | `pages/` (Markdown, built with [docmd](https://docmd.io)) |
| `/install`, `/install.sh` | the install script (the front door), served as plain text | `landing/install.sh` |
| `/schema/homebot.json`, `/schema/persona.json` | JSON Schemas for `homebot.yml` and persona files, cached for an hour, readable from any origin | `landing/schema/`, written by `go run ./tools/gendocs docs` |

The docs sidebar is `navigation` in `docmd.config.json`; the docs theme is
docmd's default, recoloured by `assets/css/homebot.css`.

## Build and preview

```sh
cd docs
bun install
bun run dev        # live preview of the docs alone (docmd)
bun run build      # images, docs and homepage, put together in dist/
bun run preview    # serve dist/ at http://localhost:4321, like production
```

`bun run build` runs three steps, each also available on its own:

- `build:images`: `scripts/images.ts` makes every image the homepage uses from
  the originals in `landing/img/raw/`, into `landing/assets/img/` (gitignored):
  the two heroes from `hero-landscape.webp` (9:4) and `hero-portrait.webp`
  (9:16, for phones held upright), both whole (`HEROES`), the social card from
  `family-portrait.png` (its box is `OG_BOX`), each robot's face and card from
  its head and body files (`PERSONAS`), the maker's photo, and the site's icon
  from `icon.webp` (a robot head on transparency): the header's logo, the
  favicon and the Apple touch icon. Nothing is upscaled. It fails if a source
  is missing, or if a hero or the icon's head is not the shape the page
  expects.
- `build:docs`: docmd builds `pages/` into `site/` with base `/docs/`.
- `assemble`: `scripts/assemble.ts` copies `landing/` to `dist/` and `site/` to
  `dist/docs/`, copies the favicon to `/favicon.ico` (browsers ask there) and
  `/docs/assets/favicon.ico` (the docs link it), and stamps the homepage's
  `/assets/` URLs with a content hash (`?v=...`) so they can be cached for a
  year. The homepage links its favicon under `/assets/img/` for that stamp:
  `/favicon.ico` is cached for a week by the browser.

docmd 0.9.7 applies every `.gitignore` up the directory tree to the full path of
`pages/`, so a checkout under a folder named like an ignored pattern (`site/`,
`dist/`, `.worktrees/`) builds 0 pages. Build from a plain path, or in Docker.

## Docker

```sh
cd docs
docker build -t homebot-site .
docker run --rm -p 8080:80 homebot-site    # http://localhost:8080
```

The image builds with `oven/bun` and serves `dist/` with Caddy
(`server/Caddyfile`) on port 80: `www.homebot.sh` redirects to `homebot.sh`,
hashed assets are cached for a year and HTML for five minutes, responses are
compressed with zstd or gzip, unknown paths get `404.html` with a 404 status,
`/install` is the front-door install script as `text/plain` (the release
installer lives at the GitHub release URL, not here), `/schema/*` is cached for
an hour with `Access-Control-Allow-Origin: *`, and `/healthz` answers `ok`.

## Coolify

- Build pack: Dockerfile; base directory `/docs`; Dockerfile `Dockerfile`.
- Port: 80. Health check path: `/healthz`.
- Domains: `https://homebot.sh` and `https://www.homebot.sh`. Coolify's Traefik
  terminates TLS; Caddy only speaks plain HTTP on port 80.
