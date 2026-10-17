import { afterEach, beforeEach, expect, test } from "bun:test";
import { existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { checkAnchors } from "./anchors";

const ROOT = join(import.meta.dir, "..");

test("every #fragment link in the docs and README is a real id in the built site", () => {
  // The Docker build context is docs/ alone, so the README is not there; skip it in that build.
  const readme = join(ROOT, "..", "README.md");
  const { checked, bad } = checkAnchors(join(ROOT, "pages"), join(ROOT, "site"), existsSync(readme) ? [readme] : []);
  expect(checked).toBeGreaterThan(5);
  expect(bad.map((b) => `${b.file}:${b.line} ${b.target}`)).toEqual([]);
});

let tmp: string;
let pages: string;
let site: string;

function page(name: string, markdown: string, ids: string[]) {
  mkdirSync(join(pages, name, ".."), { recursive: true });
  writeFileSync(join(pages, `${name}.md`), markdown);
  mkdirSync(join(site, name), { recursive: true });
  writeFileSync(join(site, name, "index.html"), ids.map((id) => `<h2 id="${id}">x</h2>`).join("\n"));
}

beforeEach(() => {
  tmp = mkdtempSync(join(tmpdir(), "anchors-"));
  pages = join(tmp, "pages");
  site = join(tmp, "site");
});
afterEach(() => rmSync(tmp, { recursive: true, force: true }));

test("accepts the prefixed id and rejects the bare heading slug", () => {
  page("reference/inbox", "# Inbox\n", ["inbox-helper", "inbox-helper-shell-integration"]);
  page("a", "[ok](/reference/inbox/#inbox-helper-shell-integration) [bare](/reference/inbox/#shell-integration)\n", []);
  const { checked, bad } = checkAnchors(pages, site);
  expect(checked).toBe(2);
  expect(bad).toEqual([{ file: "a.md", line: 1, target: "/reference/inbox/#shell-integration", page: "reference/inbox" }]);
});

test("same-page fragments resolve against the page's own ids, index pages included", () => {
  page("reference/inbox", "[ok](#inbox-updates) [bad](#updates)\n", ["inbox-updates"]);
  expect(checkAnchors(pages, site).bad.map((b) => b.target)).toEqual(["#updates"]);
});

test("relative .md links resolve from the linking file's folder", () => {
  page("reference/inbox", "# x\n", ["inbox-state"]);
  page("reference/config", "[ok](inbox.md#inbox-state) [bad](./inbox#nope) [up](../reference/inbox.md#inbox-state)\n", []);
  const { checked, bad } = checkAnchors(pages, site);
  expect(checked).toBe(3);
  expect(bad.map((b) => b.target)).toEqual(["./inbox#nope"]);
});

test("a link to a page that was not built fails, even with a fragment that looks fine", () => {
  page("a", "[gone](/missing/#x)\n", []);
  expect(checkAnchors(pages, site).bad.map((b) => b.page)).toEqual(["missing"]);
});

test("skips fenced code, external links and links without a fragment", () => {
  page("a", "```md\n[x](/a/#nope)\n```\n[ext](https://example.com/p#frag) [mail](mailto:a@b.c#x) [plain](/a/)\n", []);
  expect(checkAnchors(pages, site)).toEqual({ checked: 0, bad: [] });
});

test("README: docs URLs are checked, its own GitHub #fragments are not", () => {
  page("install", "# x\n", ["install-and-update"]);
  const readme = join(tmp, "README.md");
  writeFileSync(readme, "[toc](#features) [ok](https://homebot.sh/docs/install/#install-and-update) [bad](https://homebot.sh/docs/install/#nope)\n");
  const { checked, bad } = checkAnchors(pages, site, [readme]);
  expect(checked).toBe(2);
  expect(bad.map((b) => b.target)).toEqual(["https://homebot.sh/docs/install/#nope"]);
});
