// Turns the raw art in landing/img/raw/ into every image the homepage uses, in
// landing/assets/img/ (gitignored, rebuilt by `bun run build`).
//
//   bun run build:images            only redo what changed
//   bun run build:images --force    redo everything
//   bun scripts/images.ts --out <dir>   write somewhere else (the test does)
//
// raw/ is read only: this script opens those files and never writes there.
// What it makes, each as AVIF, WebP and JPEG:
//
//   hero-landscape-{640,...,5120}      the hero for wide screens, 9:4, shown
//                                      whole from edge to edge
//   hero-portrait-{640,960,1280,1800}  the hero for phones held upright, 9:16
//   <id>-face-{120,240}                a square crop of the robot's head file
//   <id>-body-{360,540,720,1080}       the full-body card, 4:5 (HAL and R2-D2
//                                      are 9:16, so they keep their shape)
//   maker-{120,240}                    the maker's photo, square, for the
//                                      "Behind homebot" section
//   og.jpg                             the 1200 x 630 social card
//   icon-{48,96,144}                   the header's robot head, as WebP and
//                                      PNG (both keep the transparency)
//   favicon.ico, apple-touch-icon.png  the same head, from icon.webp
//
// Nothing is ever scaled up. A width above its source is skipped, not made
// smaller than its name. Every box is a fraction of its source ([x, y, w, h],
// 0 to 1), so a re-rendered source of another size crops the same region.
// The face boxes and OG_BOX were checked by eye: the test writes a contact
// sheet of every output (see images.test.ts) to look at after a change.

import { createHash } from "node:crypto";
import { existsSync, mkdirSync, readdirSync, readFileSync, unlinkSync, writeFileSync } from "node:fs";
import { join, resolve } from "node:path";
import sharp from "sharp";

const ROOT = resolve(import.meta.dir, "..");
const LANDING = join(ROOT, "landing");
export const RAW_DIR = join(LANDING, "img", "raw");
// Bump when the output sizes, formats or crops change in a way the signature misses.
export const RECIPE = "10";

export type Box = readonly [x: number, y: number, w: number, h: number];

/** The original portrait, 4:3: the social card is cut from it. */
export const PORTRAIT = "old-family-portrait.png";
/**
 * The hero, two pictures of the family by the house, each used whole (never
 * cropped), with sky left empty for the headline. The page picks one with
 * <picture>: the landscape one everywhere but on phones held upright.
 *
 *   landscape  9:4, shown up to the screen's width, so 5120 covers a 2560 px
 *              screen at 2x (and a landscape phone at 3x)
 *   portrait   9:16, shown about 430 to 530 CSS px wide on phones, so 1800
 *              (the source's own width) covers 3x
 *
 * ratio is [w, h]: the page's width and height attributes assume it, so a
 * source of another shape fails the build instead of being stretched.
 */
export const HEROES = {
  "hero-landscape": { file: "hero-landscape.png", widths: [640, 1024, 1448, 2048, 2880, 3840, 5120], ratio: [9, 4] },
  "hero-portrait": { file: "hero-portrait.png", widths: [640, 960, 1280, 1800], ratio: [9, 16] },
} as const;

/** Faces, in output pixels: 120 for 1x, 240 for 2x. The picker uses these. */
export const FACE_WIDTHS = [120, 240];
/** Cards: about 360 CSS px wide, so 360 to 1080 covers 1x to 3x. */
export const BODY_WIDTHS = [360, 540, 720, 1080];
export const BODY_RATIO = 4 / 5;

/**
 * One entry per robot. The files keep the names the art came with (the id is
 * johnny-5, the file johnny), and nothing in raw/ is renamed.
 *
 *   face  a crop of the head file, square (the heads are square, so w = h)
 *   card  a crop of the body file to 4:5, or "contain": use the whole file
 *         and let the page fit it (HAL and R2-D2 are 9:16 until 4:5 art exists)
 */
export const PERSONAS: Record<string, { head: string; body: string; face: Box; card: Box | "contain" }> = {
  "hal-9000": { head: "hal-9000-head.png", body: "hal-9000.jpg", face: [0, 0, 1, 1], card: "contain" },
  "johnny-5": { head: "johnny-head.png", body: "johnny.png", face: [0.08, 0, 0.84, 0.84], card: [0, 0, 1, 1] },
  rosie: { head: "rosie-head.png", body: "rosie.png", face: [0.08, 0, 0.84, 0.84], card: [0, 0, 1, 1] },
  "miss-minute": { head: "miss-minute-head.png", body: "miss-minute.png", face: [0.15, 0.05, 0.85, 0.85], card: [0, 0, 1, 1] },
  marvin: { head: "marvin-head.png", body: "marvin.png", face: [0, 0.05, 0.9, 0.9], card: [0, 0, 1, 1] },
  "r2-d2": { head: "r2-d2-head.png", body: "r2-d2.jpg", face: [0.05, 0.05, 0.9, 0.9], card: "contain" },
  neatpik: { head: "neatpik-head.png", body: "neatpik.png", face: [0.12, 0.15, 0.75, 0.75], card: [0, 0, 1, 1] },
  "wall-e": { head: "wall-e-head.png", body: "wall-e.png", face: [0.15, 0.15, 0.8, 0.8], card: [0, 0, 1, 1] },
};

/** The maker's photo (square), shown at 120 CSS px. */
export const MAKER = { file: "saulo-vallory.jpg", widths: [120, 240] } as const;

/**
 * The site's icon: a robot head on a transparent background. Its empty margin
 * is trimmed, so the head (about 3:2) is what gets scaled. The header shows it
 * about 40 CSS px wide, so 48 to 144 covers 1x to 3x; the page's width and
 * height attributes assume ICON_RATIO, so a head of another shape fails the
 * build. The favicon and the Apple touch icon are made from the same head.
 */
export const ICON = { file: "icon.webp", widths: [48, 96, 144] } as const;
export const ICON_RATIO = 3 / 2;
/** The Apple touch icon's background: iOS shows transparency as black. */
export const TOUCH_BG = "#ccdfec";

/** The social card, a band of the portrait. Its ratio must be 1200:630. */
export const OG_SIZE = [1200, 630] as const;
export const OG_BOX: Box = [0, 0.21, 1, 0.7];

/** A fractional box on a W x H source, in whole pixels. Throws if it leaves the source. */
export function pixelBox(name: string, [x, y, w, h]: Box, W: number, H: number) {
  if ([x, y, w, h].some((v) => !Number.isFinite(v)) || x < 0 || y < 0 || w <= 0 || h <= 0 || x + w > 1 + 1e-9 || y + h > 1 + 1e-9) {
    throw new Error(`the ${name} box [${x}, ${y}, ${w}, ${h}] does not fit inside the source (fractions 0 to 1). Fix it in scripts/images.ts.`);
  }
  const left = Math.round(x * W);
  const top = Math.round(y * H);
  return {
    left,
    top,
    width: Math.min(Math.round(w * W), W - left),
    height: Math.min(Math.round(h * H), H - top),
  };
}

const args = process.argv.slice(2);
const force = args.includes("--force");
const outFlag = args.indexOf("--out");
const OUT = outFlag >= 0 ? resolve(args[outFlag + 1] ?? "") : join(LANDING, "assets", "img");
const MANIFEST = join(OUT, "manifest.json");

function fail(message: string): never {
  console.error(`images: ${message}`);
  process.exit(1);
}

// ------------------------------------------------------------- outputs

type Manifest = Record<string, string>;
let manifest: Manifest = {};

/** Writes one image as AVIF, WebP and JPEG. */
async function writeFormats(base: string, img: Buffer): Promise<void> {
  await Promise.all([
    sharp(img).avif({ quality: 52, effort: 4 }).toFile(join(OUT, `${base}.avif`)),
    sharp(img).webp({ quality: 78, effort: 5 }).toFile(join(OUT, `${base}.webp`)),
    sharp(img).jpeg({ quality: 80, mozjpeg: true, progressive: true }).toFile(join(OUT, `${base}.jpg`)),
  ]);
}

function formats(base: string): string[] {
  return ["avif", "webp", "jpg"].map((ext) => `${base}.${ext}`);
}

const made = new Set<string>();
async function job(key: string, sig: string, outputs: string[], run: () => Promise<void>): Promise<void> {
  for (const f of outputs) made.add(f);
  if (manifest[key] === sig && outputs.every((f) => existsSync(join(OUT, f)))) {
    console.log(`  same    ${key}`);
    return;
  }
  await run();
  manifest[key] = sig;
  console.log(`  made    ${key}`);
}

/** A source, read once: decoded to sRGB with its EXIF turn applied, plus a hash of its bytes. */
async function load(file: string) {
  const path = join(RAW_DIR, file);
  if (!existsSync(path)) fail(`landing/img/raw/${file} is missing.`);
  const bytes = readFileSync(path);
  const buf = await sharp(bytes).rotate().toColourspace("srgb").png().toBuffer();
  const { width = 0, height = 0 } = await sharp(buf).metadata();
  return { buf, width, height, sig: createHash("sha256").update(bytes).digest("hex").slice(0, 12) };
}

async function main() {
  mkdirSync(OUT, { recursive: true });
  if (existsSync(MANIFEST) && !force) manifest = JSON.parse(readFileSync(MANIFEST, "utf8"));

  // The heroes: whole, only scaled down.
  for (const [name, h] of Object.entries(HEROES)) {
    const hero = await load(h.file);
    const [rw, rh] = h.ratio;
    if (Math.abs(hero.width / hero.height - rw / rh) > 0.005) {
      fail(`landing/img/raw/${h.file} is ${hero.width} x ${hero.height}, not ${rw}:${rh}. The page assumes ${rw}:${rh}: fix the picture, or the ratio in scripts/images.ts and the hero's width and height in landing/index.html.`);
    }
    const widths = h.widths.filter((w) => w <= hero.width);
    for (const w of h.widths) if (w > hero.width) console.warn(`  skip    ${name}-${w}: the source is ${hero.width} px wide`);
    await job(name, `${hero.sig}#${RECIPE}`, widths.flatMap((w) => formats(`${name}-${w}`)), async () => {
      for (const w of widths) {
        await writeFormats(`${name}-${w}`, await sharp(hero.buf).resize(w, Math.round((w * rh) / rw)).png().toBuffer());
      }
    });
  }

  const portrait = await load(PORTRAIT);

  let og;
  try {
    og = pixelBox("og", OG_BOX, portrait.width, portrait.height);
  } catch (e) {
    fail((e as Error).message);
  }
  await job("og", `${portrait.sig}:${OG_BOX.join(",")}#${RECIPE}`, ["og.jpg"], async () => {
    await sharp(portrait.buf)
      .extract(og)
      .resize(OG_SIZE[0], OG_SIZE[1], { fit: "cover" })
      .jpeg({ quality: 82, mozjpeg: true, progressive: true })
      .toFile(join(OUT, "og.jpg"));
  });

  for (const [id, p] of Object.entries(PERSONAS)) {
    // Face: a square crop of the head file, at each size the crop can fill.
    const head = await load(p.head);
    let box;
    try {
      box = pixelBox(`${id} face`, p.face, head.width, head.height);
    } catch (e) {
      fail((e as Error).message);
    }
    if (Math.abs(box.width - box.height) > 1) fail(`the ${id} face box is ${box.width} x ${box.height}, not square.`);
    const side = Math.min(box.width, box.height);
    const faceWidths = FACE_WIDTHS.filter((w) => w <= side);
    await job(`${id}-face`, `${head.sig}:${p.face.join(",")}#${RECIPE}`, faceWidths.flatMap((w) => formats(`${id}-face-${w}`)), async () => {
      const face = await sharp(head.buf).extract({ left: box.left, top: box.top, width: side, height: side }).png().toBuffer();
      for (const w of faceWidths) {
        await writeFormats(`${id}-face-${w}`, await sharp(face).resize(w, w).png().toBuffer());
      }
    });

    // Card: the body file at 4:5 (cropped by its box), or whole when it is "contain".
    const body = await load(p.body);
    let cardBox = { left: 0, top: 0, width: body.width, height: body.height };
    if (p.card !== "contain") {
      try {
        cardBox = pixelBox(`${id} card`, p.card, body.width, body.height);
      } catch (e) {
        fail((e as Error).message);
      }
      if (Math.abs(cardBox.width / cardBox.height - BODY_RATIO) > 0.005) {
        fail(`the ${id} card box is ${cardBox.width} x ${cardBox.height}, not 4:5. Fix it in scripts/images.ts, or use "contain".`);
      }
    }
    const bodyWidths = BODY_WIDTHS.filter((w) => w <= cardBox.width);
    await job(`${id}-body`, `${body.sig}:${p.card === "contain" ? "contain" : p.card.join(",")}#${RECIPE}`, bodyWidths.flatMap((w) => formats(`${id}-body-${w}`)), async () => {
      const card = await sharp(body.buf).extract(cardBox).png().toBuffer();
      for (const w of bodyWidths) {
        await writeFormats(`${id}-body-${w}`, await sharp(card).resize({ width: w }).png().toBuffer());
      }
    });
  }

  // The maker's photo: the centre square, only scaled down.
  const maker = await load(MAKER.file);
  const makerSide = Math.min(maker.width, maker.height);
  const makerWidths = MAKER.widths.filter((w) => w <= makerSide);
  for (const w of MAKER.widths) if (w > makerSide) console.warn(`  skip    maker-${w}: the photo is ${makerSide} px wide`);
  await job("maker", `${maker.sig}#${RECIPE}`, makerWidths.flatMap((w) => formats(`maker-${w}`)), async () => {
    const square = await sharp(maker.buf).resize(makerSide, makerSide, { fit: "cover", position: "centre" }).png().toBuffer();
    for (const w of makerWidths) await writeFormats(`maker-${w}`, await sharp(square).resize(w, w).png().toBuffer());
  });

  // The icon: the head without its empty margin, only scaled down.
  const icon = await load(ICON.file);
  const head = await sharp(icon.buf).trim().png().toBuffer({ resolveWithObject: true });
  const { width: hw, height: hh } = head.info;
  if (Math.abs(hw / hh - ICON_RATIO) > 0.05) {
    fail(`the head in landing/img/raw/${ICON.file} is ${hw} x ${hh} once trimmed, not about 3:2. The header assumes 3:2: fix the picture, or ICON_RATIO in scripts/images.ts and the icon's width and height in landing/index.html and landing/404.html.`);
  }
  const iconWidths = ICON.widths.filter((w) => w <= hw);
  for (const w of ICON.widths) if (w > hw) console.warn(`  skip    icon-${w}: the head is ${hw} px wide`);
  const iconOutputs = [...iconWidths.flatMap((w) => [`icon-${w}.webp`, `icon-${w}.png`]), "favicon.ico", "apple-touch-icon.png"];
  await job("icon", `${icon.sig}#${RECIPE}`, iconOutputs, async () => {
    for (const w of iconWidths) {
      const img = await sharp(head.data).resize({ width: w }).png().toBuffer();
      await Promise.all([
        sharp(img).webp({ quality: 85, alphaQuality: 90, effort: 5 }).toFile(join(OUT, `icon-${w}.webp`)),
        sharp(img).png({ compressionLevel: 9, palette: false }).toFile(join(OUT, `icon-${w}.png`)),
      ]);
    }
    // A square tab icon, the head centred on transparency.
    const square = (s: number) => sharp(head.data).resize(s, s, { fit: "contain", background: { r: 0, g: 0, b: 0, alpha: 0 } }).png().toBuffer();
    writeFileSync(join(OUT, "favicon.ico"), ico(await Promise.all([16, 32, 48].map(square)), [16, 32, 48]));
    await sharp({ create: { width: 180, height: 180, channels: 4, background: TOUCH_BG } })
      .composite([{ input: await square(150), top: 15, left: 15 }])
      .png()
      .toFile(join(OUT, "apple-touch-icon.png"));
  });

  // OUT belongs to this script: drop files and entries from older recipes.
  const keys = new Set([...Object.keys(HEROES), "og", "icon", "maker", ...Object.keys(PERSONAS).flatMap((id) => [`${id}-face`, `${id}-body`])]);
  for (const key of Object.keys(manifest)) if (!keys.has(key)) delete manifest[key];
  for (const f of readdirSync(OUT)) {
    if (f !== "manifest.json" && !made.has(f)) {
      unlinkSync(join(OUT, f));
      console.log(`  gone    ${f}`);
    }
  }
  writeFileSync(MANIFEST, `${JSON.stringify(manifest, null, 2)}\n`);
}

/** An .ico holding PNG images (supported everywhere that matters since Windows Vista). */
function ico(pngs: Buffer[], sizes: number[]): Buffer {
  const header = Buffer.alloc(6);
  header.writeUInt16LE(0, 0);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(pngs.length, 4);
  const entries: Buffer[] = [];
  let offset = 6 + 16 * pngs.length;
  pngs.forEach((png, i) => {
    const e = Buffer.alloc(16);
    e.writeUInt8(sizes[i] >= 256 ? 0 : sizes[i], 0);
    e.writeUInt8(sizes[i] >= 256 ? 0 : sizes[i], 1);
    e.writeUInt8(0, 2);
    e.writeUInt8(0, 3);
    e.writeUInt16LE(1, 4);
    e.writeUInt16LE(32, 6);
    e.writeUInt32LE(png.length, 8);
    e.writeUInt32LE(offset, 12);
    offset += png.length;
    entries.push(e);
  });
  return Buffer.concat([header, ...entries, ...pngs]);
}

if (import.meta.main) await main();
