import { escapeHTML } from "bun";
import { readdirSync, readFileSync, writeFileSync } from "node:fs";
import { join, relative } from "node:path";

// A command begins a code line, or follows one of these shell separators.
// The suffix deliberately excludes filenames, paths, prompt text and URLs.
// `homebot setup` is what makes the name link, so it always runs as homebot.
const COMMAND = /(^|\$ |&& |\|\| |\| |; )(homebot)(?= [a-z-]|$)(?! setup\b)/gm;

function decodeHTML(value: string): string {
  return value.replace(/&(#(?:x[\da-f]+|\d+)|amp|lt|gt|quot|apos|nbsp|#39);/gi, (entity, name: string) => {
    if (name.startsWith("#")) {
      const hex = name[1]?.toLowerCase() === "x";
      return String.fromCodePoint(Number.parseInt(name.slice(hex ? 2 : 1), hex ? 16 : 10));
    }
    return ({ amp: "&", lt: "<", gt: ">", quot: '"', apos: "'", nbsp: " " } as Record<string, string>)[name.toLowerCase()] || entity;
  });
}

// The generated message catalog has one table row per message id (`context.error.platform`).
// Its cells quote what homebot prints, so a "homebot" there is output text, not a command.
function inCatalogRow(prefix: string): boolean {
  const row = prefix.lastIndexOf("<tr");
  if (row < 0 || row < prefix.lastIndexOf("</tr>")) return false;
  const id = /^<tr[^>]*>\s*<td[^>]*>\s*<code[^>]*>([a-z0-9_]+(?:\.[a-z0-9_]+)+)<\/code>/.exec(prefix.slice(row))?.[1];
  return id !== undefined && !/\.(?:ya?ml|json|toml|md|txt|sh)$/.test(id); // not a file name such as decisions.yml
}

export function markCommands(html: string): { html: string; contexts: string[] } {
  const contexts: string[] = [];
  let output = "";
  let cursor = 0;
  let depth = 0;
  let start = 0;
  let opening = "";
  for (const tag of html.matchAll(/<code\b[^>]*>|<\/code>/gi)) {
    if (tag[0].startsWith("</")) {
      depth--;
      if (depth !== 0) continue;
    } else {
      if (depth++ === 0) { start = tag.index; opening = tag[0]; }
      continue;
    }
    const whole = html.slice(start, tag.index + tag[0].length);
    const inner = html.slice(start + opening.length, tag.index);
    // HTMLRewriter joins text split by docmd's syntax-highlighting spans.
    let codeText = "";
    new HTMLRewriter().on("body", { text(chunk) { codeText += chunk.text; } })
      .transform(`<body>${inner}</body>`);
    codeText = decodeHTML(codeText);
    const prefix = html.slice(0, start);
    // A kept <div> or <pre> stays open until its closing tag; a kept <code> covers only itself.
    const keepOpen = prefix.search(/<(?:div|pre)\b[^>]*data-hb-keep[^>]*>(?:(?!<\/div>|<\/pre>)[\s\S])*$/) >= 0;
    // `<code data-hb-keep>` keeps one inline mention, such as prose about the literal binary name.
    if (keepOpen || inCatalogRow(prefix) || opening.includes("data-hb-keep") || /^\s*(?:[\d*,-]+\s+){5}/m.test(codeText)) {
      output += html.slice(cursor, start) + whole;
      cursor = tag.index + tag[0].length;
      continue;
    }
    let matches = 0;
    const marked = codeText.replace(COMMAND, (_token, before: string, _command: string, offset: number) => {
      matches++;
      const lineStart = codeText.lastIndexOf("\n", offset) + 1;
      const lineEnd = codeText.indexOf("\n", offset);
      contexts.push(codeText.slice(lineStart, lineEnd < 0 ? undefined : lineEnd).trim().slice(0, 180));
      return `${before}\u0000HB_CMD\u0000`;
    });
    output += html.slice(cursor, start) + (matches
      ? `${opening}${escapeHTML(marked).replaceAll("\u0000HB_CMD\u0000", '<span class="hb-cmd">homebot</span>')}${tag[0]}`
      : whole);
    cursor = tag.index + tag[0].length;
  }
  output += html.slice(cursor);
  return { html: output, contexts };
}

export function markSite(site: string): string {
  const paths: string[] = [];
  function visit(dir: string) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) visit(path);
      else if (entry.name.endsWith(".html")) paths.push(path);
    }
  }
  visit(site);
  paths.sort();
  const lines: string[] = [];
  for (const path of paths) {
    const result = markCommands(readFileSync(path, "utf8"));
    writeFileSync(path, result.html);
    for (const context of result.contexts) lines.push(`${relative(site, path)}: ${context}`);
  }
  return lines.join("\n") + (lines.length ? "\n" : "");
}
