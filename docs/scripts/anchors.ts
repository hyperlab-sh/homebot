// Checks every #fragment link in the markdown against the ids in the built docs.
//
// `docmd validate` only checks that the page a link points at exists, not that
// the #fragment is a real id on it. docmd also prefixes each heading id with
// the ids of the headings above it (the h2 "Shell integration" under the h1
// "Inbox & helper" is #inbox-helper-shell-integration), so a link to the
// plain heading slug works only because docmd's client script falls back to
// an id that ends with it. With JavaScript off the link lands at the top of
// the page. This check wants the real id.
//
// It runs from `bun test` after `bun run build:docs`, so site/ is current.

import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

export interface BadLink {
  file: string;
  line: number;
  target: string;
  page: string;
}

export interface AnchorReport {
  checked: number;
  bad: BadLink[];
}

const DOCS_URL = /^https:\/\/homebot\.sh\/docs\/?/;

function markdownFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    if (statSync(path).isDirectory()) return name === "node_modules" ? [] : markdownFiles(path);
    return path.endsWith(".md") ? [path] : [];
  });
}

/**
 * Checks fragment links in every .md file under pagesDir, plus the extra files
 * (the README), against the ids in siteDir. Links to other sites are skipped.
 * In an extra file only links into the docs (by absolute /path or by
 * https://homebot.sh/docs/ URL) count: its bare #fragments are GitHub's.
 */
export function checkAnchors(pagesDir: string, siteDir: string, extraFiles: string[] = []): AnchorReport {
  const idCache = new Map<string, Set<string>>();
  const idsOf = (page: string): Set<string> => {
    let ids = idCache.get(page);
    if (!ids) {
      const file = join(siteDir, page, "index.html");
      const html = existsSync(file) ? readFileSync(file, "utf8") : "";
      ids = new Set([...html.matchAll(/\sid="([^"]+)"/g)].map((m) => m[1]));
      idCache.set(page, ids);
    }
    return ids;
  };

  const files = [...markdownFiles(pagesDir).map((file) => ({ file, isPage: true })), ...extraFiles.map((file) => ({ file, isPage: false }))];
  const report: AnchorReport = { checked: 0, bad: [] };

  for (const { file, isPage } of files) {
    const rel = isPage ? file.slice(pagesDir.length + 1) : file;
    let fenced = false;
    readFileSync(file, "utf8").split("\n").forEach((text, index) => {
      if (/^\s*(```|~~~)/.test(text)) {
        fenced = !fenced;
        return;
      }
      if (fenced) return;
      for (const m of text.matchAll(/\]\(([^)\s]*#[^)\s]*)\)|href="([^"]*#[^"]*)"/g)) {
        const target = m[1] ?? m[2];
        const hash = target.indexOf("#");
        const path = target.slice(0, hash);
        const fragment = target.slice(hash + 1);
        let page: string;
        if (path === "") {
          if (!isPage) continue;
          page = rel.replace(/\.md$/, "").replace(/(^|\/)index$/, "");
        } else if (path.startsWith("/")) {
          page = path.replace(/^\/|\/$/g, "");
        } else if (DOCS_URL.test(path)) {
          page = path.replace(DOCS_URL, "").replace(/\/$/, "");
        } else if (/^[a-z][a-z0-9+.-]*:/i.test(path) || !isPage) {
          continue;
        } else {
          page = resolve("/", dirname(rel), path).slice(1).replace(/\.md$/, "").replace(/\/$/, "");
        }
        report.checked++;
        if (fragment && !idsOf(page).has(fragment)) {
          report.bad.push({ file: rel, line: index + 1, target, page });
        }
      }
    });
  }
  return report;
}

if (import.meta.main) {
  const root = resolve(import.meta.dir, "..");
  const { checked, bad } = checkAnchors(join(root, "pages"), join(root, "site"), [join(root, "..", "README.md")]);
  console.log(`checked ${checked} fragment links; ${bad.length} wrong`);
  for (const b of bad) console.log(`${b.file}:${b.line}  ${b.target}  (no such id on /${b.page}/)`);
  process.exit(bad.length ? 1 : 0);
}
