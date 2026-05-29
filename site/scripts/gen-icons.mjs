/**
 * Generate every favicon / app-icon variant from the source social avatar
 * (public/brand/social-avatar.jpg).
 *
 * Outputs land in public/:
 *   favicon-16x16.png, favicon-32x32.png, favicon-48x48.png,
 *   favicon-96x96.png           (rounded, used in browser tabs)
 *   apple-touch-icon.png        (180 square, iOS rounds it)
 *   web-app-manifest-192x192.png
 *   web-app-manifest-512x512.png
 *
 * The 1200x630 OG / Twitter card (public/og-image.png) is generated
 * separately from scripts/og-image.html — see scripts/render-og.py.
 *
 * Run with: node scripts/gen-icons.mjs
 */
import sharp from '../node_modules/.pnpm/sharp@0.34.5/node_modules/sharp/lib/index.js';
import { readFileSync, writeFileSync } from 'node:fs';
import { resolve, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const ROOT = resolve(__dirname, '..');
const SRC = resolve(ROOT, 'public/brand/social-avatar.jpg');
const PUB = resolve(ROOT, 'public');

const source = readFileSync(SRC);

/**
 * Build an SVG mask with rounded corners at the given radius ratio (0..0.5
 * of the side). 0.5 = perfect circle.
 */
function roundedMask(size, radiusRatio = 0.22) {
  const r = Math.round(size * radiusRatio);
  return Buffer.from(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}"><rect x="0" y="0" width="${size}" height="${size}" rx="${r}" ry="${r}" fill="#fff"/></svg>`
  );
}

async function square(size, outPath) {
  await sharp(source)
    .resize(size, size, { fit: 'cover' })
    .png({ quality: 92 })
    .toFile(outPath);
  console.log('wrote', outPath);
}

async function rounded(size, outPath, radiusRatio = 0.22) {
  const base = await sharp(source)
    .resize(size, size, { fit: 'cover' })
    .png()
    .toBuffer();
  await sharp(base)
    .composite([{ input: roundedMask(size, radiusRatio), blend: 'dest-in' }])
    .png({ quality: 92 })
    .toFile(outPath);
  console.log('wrote', outPath, `(rounded r=${radiusRatio})`);
}

await Promise.all([
  // Tab favicons: rounded so the icon reads as a soft chip in the tab strip.
  rounded(16, resolve(PUB, 'favicon-16x16.png'), 0.22),
  rounded(32, resolve(PUB, 'favicon-32x32.png'), 0.22),
  rounded(48, resolve(PUB, 'favicon-48x48.png'), 0.22),
  rounded(96, resolve(PUB, 'favicon-96x96.png'), 0.22),

  // Apple touch icon: square. iOS will apply its own corner radius.
  square(180, resolve(PUB, 'apple-touch-icon.png')),

  // PWA manifest icons: square. Browsers and OSes mask them per platform
  // for adaptive (maskable) usage.
  square(192, resolve(PUB, 'web-app-manifest-192x192.png')),
  square(512, resolve(PUB, 'web-app-manifest-512x512.png')),
]);

console.log('done.');
