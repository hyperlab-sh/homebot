// Tests for scripts/images.ts. They run the real build into a temp folder, so
// they need landing/img/raw/ (checked in) and sharp, nothing else.
//
//   bun test scripts/images.test.ts
//   IMAGES_SHEET=/path/contact-sheet.png bun test scripts/images.test.ts
//       also writes a contact sheet of every output, to look at after a crop change
//
// The Docker build is the other gate: see docs/Dockerfile (`bun run build`).

import { afterAll, beforeAll, describe, expect, setDefaultTimeout, test } from "bun:test";
import { createHash } from "node:crypto";
import { existsSync, mkdirSync, mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, relative, resolve } from "node:path";
import sharp from "sharp";
import { BODY_RATIO, BODY_WIDTHS, FACE_WIDTHS, HEROES, ICON, ICON_RATIO, MAKER, OG_BOX, OG_SIZE, PERSONAS, PORTRAIT, RAW_DIR, RECIPE, TOUCH_BG, pixelBox } from "./images.ts";

// Each build takes a minute on a slow machine.
setDefaultTimeout(600_000);

const SCRIPT = resolve(import.meta.dir, "images.ts");
const FORMATS = ["avif", "webp", "jpg"];

/** Every file under dir -> sha256 of its bytes. */
function hashTree(dir: string): Record<string, string> {
  const out: Record<string, string> = {};
  const walk = (d: string) => {
    for (const e of readdirSync(d, { withFileTypes: true })) {
      const p = join(d, e.name);
      if (e.isDirectory()) walk(p);
      else out[relative(dir, p)] = createHash("sha256").update(readFileSync(p)).digest("hex");
    }
  };
  walk(dir);
  return out;
}

function build(out: string, ...flags: string[]) {
  const p = Bun.spawnSync(["bun", SCRIPT, "--out", out, ...flags], { stdout: "pipe", stderr: "pipe" });
  return { code: p.exitCode, stdout: p.stdout.toString(), stderr: p.stderr.toString() };
}

const tmp = mkdtempSync(join(tmpdir(), "homebot-images-"));
const OUT = join(tmp, "img");
const rawBefore = hashTree(RAW_DIR);
let first: ReturnType<typeof build>;
const ids = Object.keys(PERSONAS);

beforeAll(() => {
  first = build(OUT, "--force");
}, 600_000);

afterAll(() => rmSync(tmp, { recursive: true, force: true }));

/** The icon's head without its empty margin, as the build trims it. */
async function trimmedHead() {
  const { info } = await sharp(join(RAW_DIR, ICON.file)).trim().png().toBuffer({ resolveWithObject: true });
  return { width: info.width, height: info.height };
}

/** The pixel at (x, y) of an image file, as [r, g, b, a]. */
async function pixel(file: string, x: number, y: number): Promise<number[]> {
  const { data, info } = await sharp(file).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
  const i = (y * info.width + x) * 4;
  return [...data.subarray(i, i + 4)];
}

/** The PNG images inside a .ico file, from its directory. */
function icoImages(file: string): Buffer[] {
  const b = readFileSync(file);
  expect([b.readUInt16LE(0), b.readUInt16LE(2)]).toEqual([0, 1]);
  return Array.from({ length: b.readUInt16LE(4) }, (_, i) => {
    const e = 6 + i * 16;
    return b.subarray(b.readUInt32LE(e + 12), b.readUInt32LE(e + 12) + b.readUInt32LE(e + 8));
  });
}

/** The source width an output must not exceed, from its file name. */
async function sourceWidth(base: string): Promise<number> {
  const width = async (file: string) => (await sharp(join(RAW_DIR, file)).metadata()).width!;
  if (base.startsWith("icon-")) return (await trimmedHead()).width;
  for (const [name, h] of Object.entries(HEROES)) if (base.startsWith(`${name}-`)) return width(h.file);
  if (base === "og") return width(PORTRAIT);
  if (base.startsWith("maker-")) return width(MAKER.file);
  for (const [id, p] of Object.entries(PERSONAS)) {
    if (base.startsWith(`${id}-face`)) return Math.round(p.face[2] * (await width(p.head)));
    if (base.startsWith(`${id}-body-`)) return width(p.body);
  }
  throw new Error(`no source for ${base}`);
}

describe("the build", () => {
  test("succeeds", () => {
    expect(first.stderr).toBe("");
    expect(first.code).toBe(0);
  });

  test("makes exactly the planned files", () => {
    // heroes 7 + 4, faces 8 x 2, maker 2, bodies 6 x 4 + HAL 4 + R2-D2 3,
    // and the icon's 3 widths as WebP and PNG.
    const want = new Set<string>(["manifest.json", "og.jpg", "favicon.ico", "apple-touch-icon.png"]);
    for (const w of [48, 96, 144]) want.add(`icon-${w}.webp`).add(`icon-${w}.png`);
    const add = (base: string) => FORMATS.forEach((f) => want.add(`${base}.${f}`));
    for (const w of [640, 1024, 1448, 2048, 2880, 3840, 5120]) add(`hero-landscape-${w}`);
    for (const w of [640, 960, 1280, 1800]) add(`hero-portrait-${w}`);
    for (const w of [120, 240]) add(`maker-${w}`);
    for (const id of ids) {
      for (const w of [120, 240]) add(`${id}-face-${w}`);
      for (const w of id === "r2-d2" ? [360, 540, 720] : [360, 540, 720, 1080]) add(`${id}-body-${w}`);
    }
    expect(new Set(readdirSync(OUT))).toEqual(want);
    expect(want.size).toBe((11 + 8 * 2 + 2 + 6 * 4 + 4 + 3) * 3 + 4 + 6);
  });

  test("the widths in the script are the ones the plan names", () => {
    expect(HEROES["hero-landscape"].widths).toEqual([640, 1024, 1448, 2048, 2880, 3840, 5120]);
    expect(HEROES["hero-portrait"].widths).toEqual([640, 960, 1280, 1800]);
    expect(FACE_WIDTHS).toEqual([120, 240]);
    expect(MAKER.widths).toEqual([120, 240]);
    expect(ICON.widths).toEqual([48, 96, 144]);
    expect(RECIPE).toBe("10");
  });

  test("the plan's eight robots, keeping their real names", () => {
    expect([...ids].sort()).toEqual(["hal-9000", "johnny-5", "marvin", "miss-minute", "neatpik", "r2-d2", "rosie", "wall-e"]);
  });

  test("every source the map names exists in raw/", () => {
    for (const p of Object.values(PERSONAS)) {
      expect(existsSync(join(RAW_DIR, p.head))).toBe(true);
      expect(existsSync(join(RAW_DIR, p.body))).toBe(true);
    }
    expect(existsSync(join(RAW_DIR, PORTRAIT))).toBe(true);
    for (const h of Object.values(HEROES)) expect(existsSync(join(RAW_DIR, h.file))).toBe(true);
    expect(existsSync(join(RAW_DIR, MAKER.file))).toBe(true);
  });
});

describe("output sizes", () => {
  test("the heroes: whole, at their ratio, at each width, all three formats", async () => {
    for (const [name, h] of Object.entries(HEROES)) {
      const [rw, rh] = h.ratio;
      for (const w of h.widths) {
        for (const f of FORMATS) {
          const m = await sharp(join(OUT, `${name}-${w}.${f}`)).metadata();
          expect([m.width, m.height]).toEqual([w, Math.round((w * rh) / rw)]);
        }
      }
    }
    // The ratios the page's width and height attributes assume.
    expect(HEROES["hero-landscape"].ratio).toEqual([9, 4]);
    expect(HEROES["hero-portrait"].ratio).toEqual([9, 16]);
  });

  test("the heroes are not cropped: the smallest output is the whole source scaled down", async () => {
    for (const [name, h] of Object.entries(HEROES)) {
      const w = h.widths[0];
      const out = await sharp(join(OUT, `${name}-${w}.webp`)).removeAlpha().raw().toBuffer({ resolveWithObject: true });
      const ref = await sharp(join(RAW_DIR, h.file)).resize(out.info.width, out.info.height).removeAlpha().raw().toBuffer();
      // Mean absolute difference per channel, out of 255. Measured on these heroes:
      // the whole picture 4 to 6.7 (lossy compression), a 1% shift 19 to 24, a 2%
      // crop 26 to 33.
      let sum = 0;
      for (let i = 0; i < ref.length; i++) sum += Math.abs(ref[i] - out.data[i]);
      expect(sum / ref.length).toBeLessThan(12);
    }
  });

  test("faces: square at 120 and 240, with no old unsized name left", async () => {
    for (const id of ids) {
      for (const f of FORMATS) {
        for (const w of FACE_WIDTHS) {
          const m = await sharp(join(OUT, `${id}-face-${w}.${f}`)).metadata();
          expect([m.width, m.height]).toEqual([w, w]);
        }
        expect(existsSync(join(OUT, `${id}-face.${f}`))).toBe(false);
      }
    }
  });

  test("the maker's photo: square at 120 and 240", async () => {
    for (const f of FORMATS) {
      for (const w of MAKER.widths) {
        const m = await sharp(join(OUT, `maker-${w}.${f}`)).metadata();
        expect([m.width, m.height]).toEqual([w, w]);
      }
    }
  });

  test("bodies: 4:5 for the boxed ones, the source shape for contain", async () => {
    for (const [id, p] of Object.entries(PERSONAS)) {
      const src = await sharp(join(RAW_DIR, p.body)).metadata();
      for (const w of BODY_WIDTHS) {
        if (w > src.width!) {
          expect(existsSync(join(OUT, `${id}-body-${w}.jpg`))).toBe(false);
          continue;
        }
        for (const f of FORMATS) {
          const m = await sharp(join(OUT, `${id}-body-${w}.${f}`)).metadata();
          expect(m.width).toBe(w);
          const ratio = m.width! / m.height!;
          if (p.card === "contain") expect(Math.abs(ratio - src.width! / src.height!)).toBeLessThan(0.005);
          else expect(Math.abs(ratio - BODY_RATIO)).toBeLessThan(0.005);
        }
      }
    }
    // The two 9:16 ones are the ones marked contain, and nothing else is.
    const contain = Object.entries(PERSONAS).filter(([, p]) => p.card === "contain").map(([id]) => id);
    expect(contain.sort()).toEqual(["hal-9000", "r2-d2"]);
  });

  test("og.jpg is 1200 x 630 and OG_BOX has that shape on the portrait", async () => {
    const m = await sharp(join(OUT, "og.jpg")).metadata();
    expect([m.width, m.height]).toEqual([1200, 630]);
    const src = await sharp(join(RAW_DIR, PORTRAIT)).metadata();
    const box = pixelBox("og", OG_BOX, src.width!, src.height!);
    expect(Math.abs(box.width / box.height - OG_SIZE[0] / OG_SIZE[1])).toBeLessThan(0.005);
  });

  test("the head in icon.webp is about 3:2 once trimmed, and big enough for every width", async () => {
    const head = await trimmedHead();
    expect(Math.abs(head.width / head.height - ICON_RATIO)).toBeLessThan(0.05);
    expect(head.width).toBeGreaterThanOrEqual(Math.max(...ICON.widths));
  });

  test("the header icons are the trimmed head at 48 x 32 (the page's width and height), 96 and 144, transparent around it", async () => {
    const head = await trimmedHead();
    for (const w of ICON.widths) {
      for (const ext of ["webp", "png"]) {
        const file = join(OUT, `icon-${w}.${ext}`);
        const m = await sharp(file).metadata();
        expect(m.width).toBe(w);
        expect(Math.abs(m.height! - (w * head.height) / head.width)).toBeLessThanOrEqual(1);
        expect(m.hasAlpha).toBe(true);
        expect((await pixel(file, 0, 0))[3]).toBe(0); // the head is rounded, so its corners are empty
        expect((await pixel(file, w / 2, Math.floor(m.height! / 2)))[3]).toBeGreaterThan(240); // the source head is 253, not 255
      }
    }
    expect((await sharp(join(OUT, "icon-48.png")).metadata()).height).toBe(32);
  });

  test("favicon.ico holds the head at 16, 32 and 48 px, centred on transparency", async () => {
    const pngs = icoImages(join(OUT, "favicon.ico"));
    expect(pngs.length).toBe(3);
    for (const [i, s] of [16, 32, 48].entries()) {
      const m = await sharp(pngs[i]).metadata();
      expect([m.format, m.width, m.height]).toEqual(["png", s, s]);
      const { data } = await sharp(pngs[i]).ensureAlpha().raw().toBuffer({ resolveWithObject: true });
      const alpha = (x: number, y: number) => data[(y * s + x) * 4 + 3];
      expect(alpha(0, 0)).toBe(0); // the head is wider than tall: empty bands above and below
      expect(alpha(0, s - 1)).toBe(0);
      expect(alpha(s / 2, s / 2)).toBeGreaterThan(240);
    }
  });

  test("apple-touch-icon.png is 180 x 180, opaque, the head on TOUCH_BG", async () => {
    const file = join(OUT, "apple-touch-icon.png");
    const m = await sharp(file).metadata();
    expect([m.width, m.height]).toEqual([180, 180]);
    const bg = [1, 3, 5].map((i) => parseInt(TOUCH_BG.slice(i, i + 2), 16));
    expect(await pixel(file, 0, 0)).toEqual([...bg, 255]);
    expect(await pixel(file, 179, 179)).toEqual([...bg, 255]);
    expect(await pixel(file, 90, 90)).not.toEqual([...bg, 255]);
  });
});

describe("never upscales", () => {
  test("no output is wider than its source", async () => {
    let checked = 0;
    for (const f of readdirSync(OUT)) {
      if (!/\.(avif|webp|jpg|png)$/.test(f) || f === "apple-touch-icon.png") continue;
      const base = f.replace(/\.(avif|webp|jpg|png)$/, "");
      const m = await sharp(join(OUT, f)).metadata();
      const src = await sourceWidth(base);
      expect(m.width!).toBeLessThanOrEqual(src);
      checked++;
    }
    expect(checked).toBe(11 * 3 + 1 + 8 * 2 * 3 + 2 * 3 + (6 * 4 + 4 + 3) * 3 + 3 * 2);
  });

  test("the source widths leave every hero width and the 1080 cards reachable, and R2-D2's 1080 skipped", async () => {
    for (const h of Object.values(HEROES)) {
      expect((await sharp(join(RAW_DIR, h.file)).metadata()).width!).toBeGreaterThanOrEqual(Math.max(...h.widths));
    }
    expect((await sharp(join(RAW_DIR, PERSONAS["r2-d2"].body)).metadata()).width!).toBeLessThan(1080);
  });
});

describe("raw/ is read only", () => {
  test("byte-identical after the build, with no file added or removed", () => {
    expect(hashTree(RAW_DIR)).toEqual(rawBefore);
  });

  test("and after a second, incremental build and a forced one", () => {
    expect(build(OUT).code).toBe(0);
    expect(build(OUT, "--force").code).toBe(0);
    expect(hashTree(RAW_DIR)).toEqual(rawBefore);
  }, 600_000);
});

describe("incremental builds", () => {
  test("a second run redoes nothing and changes nothing", () => {
    const before = hashTree(OUT);
    const r = build(OUT);
    expect(r.code).toBe(0);
    expect(r.stdout).not.toContain("made");
    expect(r.stdout.match(/same/g)?.length).toBe(5 + ids.length * 2);
    expect(hashTree(OUT)).toEqual(before);
  });

  test("stale files and manifest entries from an older recipe are deleted", () => {
    writeFileSync(join(OUT, "johnny-5-face-96.jpg"), "stale");
    writeFileSync(join(OUT, "family-1448-old.webp"), "stale");
    writeFileSync(join(OUT, "hero-3840.avif"), "stale"); // the single 3:2 hero, before recipe 10
    const manifest = JSON.parse(readFileSync(join(OUT, "manifest.json"), "utf8"));
    manifest["zork-face"] = "old";
    manifest.hero = "old";
    manifest.icons = "old"; // the icons from favicon.svg, before icon.webp
    writeFileSync(join(OUT, "manifest.json"), JSON.stringify(manifest));
    const r = build(OUT);
    expect(r.stdout).toContain("gone    johnny-5-face-96.jpg");
    expect(existsSync(join(OUT, "family-1448-old.webp"))).toBe(false);
    expect(existsSync(join(OUT, "hero-3840.avif"))).toBe(false);
    const after = JSON.parse(readFileSync(join(OUT, "manifest.json"), "utf8"));
    expect(after["zork-face"]).toBeUndefined();
    expect(after.hero).toBeUndefined();
    expect(after.icons).toBeUndefined();
  });

  test("a deleted output is made again", () => {
    rmSync(join(OUT, "neatpik-body-540.webp"));
    const r = build(OUT);
    expect(r.stdout).toContain("made    neatpik-body");
    expect(existsSync(join(OUT, "neatpik-body-540.webp"))).toBe(true);
  });

  test("every manifest signature carries the recipe, so a bump redoes everything", () => {
    const manifest = JSON.parse(readFileSync(join(OUT, "manifest.json"), "utf8")) as Record<string, string>;
    expect(Object.keys(manifest).length).toBe(5 + ids.length * 2);
    for (const sig of Object.values(manifest)) expect(sig.endsWith(`#${RECIPE}`)).toBe(true);
  });
});

describe("pixelBox", () => {
  test("turns fractions into whole pixels of the source", () => {
    expect(pixelBox("t", [0.1, 0.2, 0.5, 0.25], 1000, 800)).toEqual({ left: 100, top: 160, width: 500, height: 200 });
    expect(pixelBox("t", [0, 0, 1, 1], 1122, 1402)).toEqual({ left: 0, top: 0, width: 1122, height: 1402 });
  });

  test("the same fractions crop the same region of a source of another size", () => {
    const a = pixelBox("t", [0.25, 0.25, 0.5, 0.5], 1000, 1000);
    const b = pixelBox("t", [0.25, 0.25, 0.5, 0.5], 2000, 2000);
    expect(b).toEqual({ left: a.left * 2, top: a.top * 2, width: a.width * 2, height: a.height * 2 });
  });

  test("never reaches past the edge after rounding", () => {
    const b = pixelBox("t", [0.333, 0.333, 0.667, 0.667], 1001, 1001);
    expect(b.left + b.width).toBeLessThanOrEqual(1001);
    expect(b.top + b.height).toBeLessThanOrEqual(1001);
  });

  test.each([
    [[-0.1, 0, 0.5, 0.5]],
    [[0, -0.1, 0.5, 0.5]],
    [[0.6, 0, 0.5, 0.5]],
    [[0, 0.6, 0.5, 0.5]],
    [[0, 0, 0, 0.5]],
    [[0, 0, 0.5, 0]],
    [[0, 0, 1.2, 1]],
    [[0, 0, Number.NaN, 1]],
    [[100, 100, 200, 200]], // pixels where fractions belong
  ] as const)("rejects the box %p", (box) => {
    expect(() => pixelBox("t", box as unknown as [number, number, number, number], 1000, 1000)).toThrow(/does not fit/);
  });

  test("every box in the map fits its source", async () => {
    for (const [id, p] of Object.entries(PERSONAS)) {
      const head = await sharp(join(RAW_DIR, p.head)).metadata();
      const f = pixelBox(id, p.face, head.width!, head.height!);
      expect(Math.abs(f.width - f.height)).toBeLessThanOrEqual(1);
      if (p.card !== "contain") {
        const body = await sharp(join(RAW_DIR, p.body)).metadata();
        pixelBox(id, p.card, body.width!, body.height!);
      }
    }
  });
});

describe("contact sheet", () => {
  // Not an assertion: set IMAGES_SHEET to get a picture of every output to look at.
  test.skipIf(!process.env.IMAGES_SHEET)("writes one", async () => {
    const CELL = 220;
    const LABEL = 34;
    const COLS = 8;
    const bases = [...new Set(readdirSync(OUT).filter((f) => f.endsWith(".jpg")).map((f) => f.slice(0, -4)))].sort();
    const rows = Math.ceil(bases.length / COLS);
    const cells: sharp.OverlayOptions[] = [];
    for (const [i, base] of bases.entries()) {
      const left = (i % COLS) * CELL;
      const top = Math.floor(i / COLS) * (CELL + LABEL);
      const m = await sharp(join(OUT, `${base}.jpg`)).metadata();
      const img = await sharp(join(OUT, `${base}.jpg`)).resize(CELL - 6, CELL - 6, { fit: "contain", background: "#2b2b2b" }).png().toBuffer();
      cells.push({ input: img, left: left + 3, top: top + 3 });
      const label = `<svg width="${CELL}" height="${LABEL}" xmlns="http://www.w3.org/2000/svg"><text x="4" y="14" fill="#eee" font-family="monospace" font-size="12">${base}</text><text x="4" y="29" fill="#9ab" font-family="monospace" font-size="12">${m.width} x ${m.height}</text></svg>`;
      cells.push({ input: Buffer.from(label), left, top: top + CELL });
    }
    const file = process.env.IMAGES_SHEET!;
    mkdirSync(resolve(file, ".."), { recursive: true });
    await sharp({ create: { width: COLS * CELL, height: rows * (CELL + LABEL), channels: 3, background: "#1a1a1a" } })
      .composite(cells)
      .png()
      .toFile(file);
    expect(existsSync(file)).toBe(true);
  }, 120_000);
});
