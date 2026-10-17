// Serves dist/ locally, close enough to the production server (server/Caddyfile)
// to check pages, links and the 404 page:
//
//   bun run preview              http://localhost:4321
//   bun run preview --port 8080
//
// Not used in production; the Docker image serves dist/ with Caddy.

import { existsSync, statSync } from "node:fs";
import { join, normalize, resolve } from "node:path";

const DIST = resolve(import.meta.dir, "..", "dist");
const portArg = process.argv.indexOf("--port");
const port = portArg > 0 ? Number(process.argv[portArg + 1]) : 4321;

if (!existsSync(join(DIST, "index.html"))) {
  console.error("preview: dist/ is empty. Run `bun run build` first.");
  process.exit(1);
}

function fileFor(pathname: string): string | undefined {
  const path = normalize(join(DIST, decodeURIComponent(pathname)));
  if (!path.startsWith(DIST)) return undefined;
  if (existsSync(path) && statSync(path).isFile()) return path;
  const index = join(path, "index.html");
  if (existsSync(index)) return index;
  return undefined;
}

const server = Bun.serve({
  port,
  fetch(req) {
    const url = new URL(req.url);
    if (url.pathname === "/healthz") return new Response("ok\n");
    if (url.pathname === "/install" || url.pathname === "/install.sh") {
      return new Response(Bun.file(join(DIST, "install.sh")), { headers: { "content-type": "text/plain; charset=utf-8" } });
    }
    const file = fileFor(url.pathname);
    if (file?.endsWith("index.html") && !url.pathname.endsWith("/") && !url.pathname.endsWith(".html")) {
      return Response.redirect(`${url.pathname}/${url.search}`, 308);
    }
    if (file) return new Response(Bun.file(file));
    return new Response(Bun.file(join(DIST, "404.html")), { status: 404, headers: { "content-type": "text/html; charset=utf-8" } });
  },
});

console.log(`Serving dist/ at http://localhost:${server.port}`);
