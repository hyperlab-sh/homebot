// Puts the homepage and the docmd build together into dist/, the folder the
// web server serves:
//
//   dist/            the homepage (landing/, minus the raw photos)
//   dist/docs/       the docmd site (site/, built with base /docs/)
//
// It also stamps every local /assets/ URL in the homepage's HTML and CSS with a
// short content hash (?v=1a2b3c4d), so the server can cache those files for a
// year and a changed file still reaches everyone at once.
//
// Run `bun run build`, which builds the images and the docs first.

import { createHash } from "node:crypto";
import { copyFileSync, cpSync, existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";
import { markSite } from "./command-tokens";

const ROOT = resolve(import.meta.dir, "..");
const LANDING = join(ROOT, "landing");
const SITE = join(ROOT, "site");
const DIST = join(ROOT, "dist");

function fail(message: string): never {
  console.error(`assemble: ${message}`);
  process.exit(1);
}

if (!existsSync(join(SITE, "index.html"))) {
  fail(
    [
      "site/index.html is missing, so the docs did not build. Run `bun run build:docs` first.",
      "If docmd says it generated 0 pages: docmd 0.9.7 applies every .gitignore up the",
      "directory tree to the full path of pages/. A parent folder named like an ignored",
      "pattern (site/, dist/, .worktrees/, ...) hides every page. Build from another path.",
    ].join("\n"),
  );
}
if (!existsSync(join(LANDING, "assets", "img", "manifest.json"))) {
  fail("landing/assets/img/ is empty. Run `bun run build:images` first.");
}

// Start from an empty dist/ (this script owns it; nothing else writes there).
rmSync(DIST, { recursive: true, force: true });
mkdirSync(DIST, { recursive: true });

const skip = new Set([join(LANDING, "img"), join(LANDING, "IMAGE-PROMPTS.md"), join(LANDING, "assets", "img", "manifest.json")]);
cpSync(LANDING, DIST, {
  recursive: true,
  filter: (src) => !skip.has(src) && !src.split(sep).pop()!.startsWith("."),
});
cpSync(SITE, join(DIST, "docs"), { recursive: true });

// The landing page owns the picker (landing/assets/js/choice.js); the docs
// navbar has its own companion, docs-choice.js, which reads the same storage.
// Both read the generated reserved names (go run ./tools/gennames), which
// replace the landing's hand-kept copy here.
const scripts = join(ROOT, "assets", "js");
mkdirSync(join(DIST, "assets", "js"), { recursive: true });
mkdirSync(join(DIST, "docs", "assets", "js"), { recursive: true });
copyFileSync(join(scripts, "reserved-names.json"), join(DIST, "assets", "js", "reserved-names.json"));
for (const file of ["docs-choice.js", "reserved-names.json"]) {
  copyFileSync(join(scripts, file), join(DIST, "docs", "assets", "js", file));
}
writeFileSync(join(DIST, "command-tokens.txt"), markSite(join(DIST, "docs")));

// The homepage links the favicon under /assets/, so it gets a content hash like
// every other asset: /favicon.ico is cached for a week by the browser, and
// a new icon would wait that long. Browsers still ask for /favicon.ico at the
// root no matter what the page says, so it is there as well. The docs link
// theirs from docs/assets/ (docmd.config.json, which adds its own ?v=), where
// docmd would otherwise leave its default icon.
const favicon = join(DIST, "assets", "img", "favicon.ico");
copyFileSync(favicon, join(DIST, "favicon.ico"));
copyFileSync(favicon, join(DIST, "docs", "assets", "favicon.ico"));

// The font switch's script (font.js) serves the homepage and the docs alike.
// docmd.config.json loads it from docs/assets/js/, with docmd's own ?v=.
mkdirSync(join(DIST, "docs", "assets", "js"), { recursive: true });
copyFileSync(join(DIST, "assets", "js", "font.js"), join(DIST, "docs", "assets", "js", "font.js"));

// font.js runs at the end of the body, so a docs page would first paint in
// the usual font and then switch. The homepage sets the attribute from a line
// in its <head>; docmd has no setting for one, so it goes in here, at the top
// of every docs page's <head>. docmd's in-page navigation keeps <html>, and
// with it the attribute.
const FONT_LINE = `<script>try{if(localStorage.getItem("homebot.font")==="dyslexic")document.documentElement.dataset.font="dyslexic"}catch(e){}</script>`;
function docsPages(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? docsPages(join(dir, e.name)) : e.name.endsWith(".html") ? [join(dir, e.name)] : [],
  );
}
for (const page of docsPages(join(DIST, "docs"))) {
  const html = readFileSync(page, "utf8");
  const head = html.match(/<head(?:\s[^>]*)?>/);
  if (!head) fail(`${relative(DIST, page)} has no <head>`);
  writeFileSync(page, html.replace(head[0], head[0] + FONT_LINE));
}

// ------------------------------------------------------------- stamping

const hashes = new Map<string, string>();
function hashOf(urlPath: string, from: string): string {
  const file = join(DIST, urlPath);
  if (!existsSync(file)) fail(`${from} points at ${urlPath}, which is not in dist/`);
  let h = hashes.get(file);
  if (!h) {
    h = createHash("sha256").update(readFileSync(file)).digest("hex").slice(0, 8);
    hashes.set(file, h);
  }
  return h;
}

// /assets/... or https://homebot.sh/assets/..., not already stamped.
const ASSET = /(https:\/\/homebot\.sh)?(\/assets\/[A-Za-z0-9._/-]+\.(?:css|js|woff2|avif|webp|jpe?g|png|svg|ico))(?![?\w])/g;

function stamp(file: string) {
  const from = relative(DIST, file);
  const text = readFileSync(file, "utf8");
  let count = 0;
  const out = text.replace(ASSET, (_m, origin: string | undefined, path: string) => {
    count++;
    return `${origin ?? ""}${path}?v=${hashOf(path, from)}`;
  });
  writeFileSync(file, out);
  console.log(`  stamped ${String(count).padStart(3)} in ${from}`);
}

// CSS first: the HTML then hashes the stamped stylesheet.
for (const css of readdirSync(join(DIST, "assets", "css"))) stamp(join(DIST, "assets", "css", css));
stamp(join(DIST, "docs", "assets", "css", "homebot.css"));
for (const html of readdirSync(DIST).filter((f) => f.endsWith(".html"))) stamp(join(DIST, html));

// ------------------------------------------------------------- summary

function size(dir: string): number {
  return readdirSync(dir, { withFileTypes: true }).reduce((sum, e) => {
    const p = join(dir, e.name);
    return sum + (e.isDirectory() ? size(p) : statSync(p).size);
  }, 0);
}
console.log(`  dist/ ready: ${(size(DIST) / 1024 / 1024).toFixed(1)} MB (docs ${(size(join(DIST, "docs")) / 1024 / 1024).toFixed(1)} MB)`);
