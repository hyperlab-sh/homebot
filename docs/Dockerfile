# syntax=docker/dockerfile:1

# homebot.sh: the homepage and the docs as one static site.
#
#   docker build -t homebot-site docs/
#   docker run --rm -p 8080:80 homebot-site     # http://localhost:8080
#
# Coolify: Dockerfile build pack, base directory /docs, port 80, health check
# path /healthz. TLS is Traefik's job; this image serves plain HTTP on :80.

FROM oven/bun:1.4.2 AS build
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile
COPY . .
# Images from landing/img/raw/, the docmd site (base /docs/), then dist/.
RUN bun run build

FROM caddy:2.11.7-alpine
COPY server/Caddyfile /etc/caddy/Caddyfile
COPY --from=build /app/dist /srv
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1/healthz || exit 1
