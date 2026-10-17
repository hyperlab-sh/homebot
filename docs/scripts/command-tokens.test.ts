import { expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { markCommands } from "./command-tokens";

const ROOT = join(import.meta.dir, "..");

test("marks complete command tokens after shell separators, including split highlighting", () => {
  const source = `<pre><code><span>home</span><span>bot name</span>\n$ homebot --plain name\necho ok && homebot tidy\necho ok || homebot link\necho ok | homebot next\necho ok; homebot save</code></pre>`;
  const result = markCommands(source);
  expect(result.contexts).toHaveLength(6);
  expect(result.html.match(/class="hb-cmd"/g)).toHaveLength(6);
  expect(result.html).toContain('<span class="hb-cmd">homebot</span> name');
});

test("leaves paths, files, labels, substitutions and kept cron examples alone", () => {
  const source = `<code>homebot.yml /homebot hyperlab-sh/homebot homebot@ homebot: homebot · $(homebot shell zsh)</code><div data-hb-keep><pre><code>0 * * * * homebot tidy</code></pre></div>`;
  expect(markCommands(source).contexts).toEqual([]);
});

test("leaves setup commands alone: setup is what creates the name link", () => {
  const source = `<pre><code>homebot setup --name zork\nhomebot setup --persona rosie\nhomebot setup\necho ok && homebot setup --remove\nhomebot name rosie</code></pre>`;
  const result = markCommands(source);
  expect(result.contexts).toEqual(["homebot name rosie"]);
  expect(result.html.match(/class="hb-cmd"/g)).toHaveLength(1);
  expect(result.html).toContain("homebot setup --name zork");
});

test("an inline kept mention covers only itself", () => {
  const source = `<p>The <code data-hb-keep>homebot</code> binary</p><p><code>homebot tidy</code></p><div data-hb-keep><pre><code>homebot link</code></pre></div><p><code>homebot save</code></p>`;
  expect(markCommands(source).contexts).toEqual(["homebot tidy", "homebot save"]);
});

test("leaves message catalog rows alone but marks commands in other table rows", () => {
  const source = `<table><tr><td><code>context.error.platform</code></td><td><code>homebot supports {oses}.</code></td></tr><tr><td><code>homebot name [word]</code></td><td>Show the name</td></tr><tr><td><code>decisions.yml</code></td><td><code>homebot triage</code></td></tr></table>`;
  expect(markCommands(source).contexts).toEqual(["homebot name [word]", "homebot triage"]);
});

test("built command contexts match the reviewed golden", () => {
  expect(readFileSync(join(ROOT, "dist", "command-tokens.txt"), "utf8"))
    .toBe(readFileSync(join(import.meta.dir, "command-tokens.golden"), "utf8"));
});

test("the hand-marked landing has no new unmarked command token", () => {
  const landing = readFileSync(join(ROOT, "landing", "index.html"), "utf8")
    .replace(/<code\b[^>]*class="hb-cmd"[^>]*>[\s\S]*?<\/code>/g, "")
    .replace(/<span\b[^>]*class="[^"]*hb-cmd[^"]*"[^>]*>homebot<\/span>/g, "COMMAND");
  const contexts = markCommands(landing).contexts.filter((context) => context !== "homebot");
  expect(contexts).toEqual([]);
});
