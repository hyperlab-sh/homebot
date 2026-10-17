// Run against `bun run preview` after `bun run build`:
// DOCS_URL=http://127.0.0.1:4321 bun run test:browser
import { chromium } from "playwright";

const base = process.env.DOCS_URL || "http://127.0.0.1:4321";
const browser = await chromium.launch({ headless: true });
const assert = (ok: boolean, message: string) => { if (!ok) throw new Error(message); };

try {
  const context = await browser.newContext();
  await context.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async (value: string) => { (window as any).__copied = value; } },
    });
  });
  const page = await context.newPage();
  for (const path of ["/docs/", "/docs/naming/", "/docs/reference/commands/"]) {
    await page.goto(`${base}${path}`);
    await page.locator(".hb-choice").waitFor();
    assert(await page.locator(".hb-choice").count() === 1, `one navbar choice on ${path}`);
    assert(await page.locator(".hb-also").count() === 0, `no alternate-command note before a name on ${path}`);
  }

  await page.goto(`${base}/docs/naming/`);
  await page.locator(".hb-choice input").fill("zork");
  assert(await page.locator(".main-content .hb-cmd").first().innerText() === "zork", "command swaps to the chosen name");
  assert(await page.locator(".main-content .hb-also").count() === 1, "one homebot alternate-command note");
  const mainText = await page.locator(".main-content").innerText();
  assert(mainText.includes("homebot setup --name zork"), "first-setup command keeps homebot");
  assert(mainText.includes("homebot name zork"), "name bootstrap command keeps homebot");
  assert(mainText.includes("zork name --off"), "a command that can run under the name swaps");
  const swapped = page.locator(".main-content .code-wrapper:has(.hb-cmd) .copy-code-button").first();
  await swapped.click();
  assert(((await page.evaluate(() => (window as any).__copied)) as string).includes("zork name"), "copied command uses the name");

  for (const path of ["/docs/reference/commands/", "/docs/naming/", "/docs/reference/commands/"]) {
    const link = path.endsWith("commands/") ? "/reference/commands/" : "/naming/";
    await page.locator(`.main-content a[href$="${link}"]`).first().click();
    await page.waitForURL(`**${path}`);
    await page.locator(".hb-choice").waitFor();
    assert(await page.locator(".hb-choice").count() === 1, "navbar injection remains singular after SPA navigation");
    assert(await page.locator(".main-content .hb-cmd").first().innerText() === "zork", "command swap survives navigation");
  }
  await page.goto(`${base}/docs/`);
  await page.locator(".hb-choice").waitFor();
  const start = await page.locator(".main-content").innerText();
  for (const kept of ["homebot --help", "homebot keys init", "homebot unpack --dry-run", "homebot link --backup"]) {
    assert(start.includes(kept), `pre-setup command keeps homebot: ${kept}`);
  }
  assert(start.includes("zork next"), "a post-setup command on the getting-started page swaps");
  await page.goto(`${base}/docs/personas/`);
  await page.locator(".hb-choice").waitFor();
  assert((await page.locator(".main-content").innerText()).includes("homebot setup --persona <id>"), "persona setup command keeps homebot");
  await page.locator(".hb-choice input").fill("");
  assert(await page.locator(".hb-also").count() === 0, "alternate-command note disappears with the name");
  await context.close();

  const noJS = await browser.newContext({ javaScriptEnabled: false });
  const plain = await noJS.newPage();
  await plain.goto(`${base}/docs/naming/`);
  assert(await plain.locator(".hb-choice").count() === 0, "JavaScript-off page has no injected controls");
  const plainCommand = await plain.locator(".main-content .hb-cmd").first().textContent();
  assert(plainCommand === "homebot", `JavaScript-off commands stay homebot (saw ${JSON.stringify(plainCommand)})`);
  await noJS.close();
  console.log("N10 docs browser checks passed");
} finally {
  await browser.close();
}
