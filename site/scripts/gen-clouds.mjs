// Generate painterly cloud PNGs using Perlin noise + Sharp.
//
//   node scripts/gen-clouds.mjs
//
// Outputs site/public/backdrops/cloud-{1..5}.webp. Each cloud is a
// fractal-noise white mass clipped by an elliptical falloff, with a
// soft blue-gray underbelly composited below. Pure pixels — same
// painterly look as raster clouds on Apple / Cursor / Linear, with
// zero browser-quirk surface area.

import sharp from 'sharp';
import { mkdir } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { dirname, resolve } from 'node:path';

const __dirname = dirname(fileURLToPath(import.meta.url));
const outDir = resolve(__dirname, '../public/backdrops');
await mkdir(outDir, { recursive: true });

// --- Tiny Perlin noise (Ken Perlin, classic 2D impl, public domain) ---
function makePerm(seed) {
  // Mulberry32 PRNG for deterministic permutation table
  let s = seed >>> 0;
  const rand = () => {
    s = (s + 0x6D2B79F5) | 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
  const p = new Uint8Array(512);
  const base = new Uint8Array(256);
  for (let i = 0; i < 256; i++) base[i] = i;
  for (let i = 255; i > 0; i--) {
    const j = Math.floor(rand() * (i + 1));
    [base[i], base[j]] = [base[j], base[i]];
  }
  for (let i = 0; i < 512; i++) p[i] = base[i & 255];
  return p;
}
function fade(t) { return t * t * t * (t * (t * 6 - 15) + 10); }
function lerp(a, b, t) { return a + (b - a) * t; }
function grad(hash, x, y) {
  const h = hash & 7;
  const u = h < 4 ? x : y;
  const v = h < 4 ? y : x;
  return ((h & 1) ? -u : u) + ((h & 2) ? -v : v);
}
function perlin2(perm, x, y) {
  const X = Math.floor(x) & 255;
  const Y = Math.floor(y) & 255;
  x -= Math.floor(x);
  y -= Math.floor(y);
  const u = fade(x);
  const v = fade(y);
  const A = perm[X] + Y, B = perm[X + 1] + Y;
  return lerp(
    lerp(grad(perm[A], x, y), grad(perm[B], x - 1, y), u),
    lerp(grad(perm[A + 1], x, y - 1), grad(perm[B + 1], x - 1, y - 1), u),
    v,
  );
}
function fbm(perm, x, y, octaves) {
  let v = 0, amp = 1, freq = 1, maxAmp = 0;
  for (let i = 0; i < octaves; i++) {
    v += perlin2(perm, x * freq, y * freq) * amp;
    maxAmp += amp;
    amp *= 0.55;
    freq *= 2.0;
  }
  return v / maxAmp;
}

// --- Cloud generator ----------------------------------------------------
async function genCloud({ name, w, h, seed, freqX, freqY, octaves, threshold, ellipseRx, ellipseRy }) {
  const perm = makePerm(seed);
  const body   = Buffer.alloc(w * h * 4);
  const shadow = Buffer.alloc(w * h * 4);

  const cx = w / 2, cy = h * 0.50;
  const rx = w * ellipseRx, ry = h * ellipseRy;

  // smoothstep maps low..high noise → 0..1 with eased edges
  const smoothstep = (low, high, x) => {
    const t = Math.max(0, Math.min(1, (x - low) / (high - low)));
    return t * t * (3 - 2 * t);
  };

  for (let y = 0; y < h; y++) {
    for (let x = 0; x < w; x++) {
      // Fractal Brownian motion noise — main density field.
      const n = (fbm(perm, x * freqX, y * freqY, octaves) + 1) * 0.5;

      // Elliptical falloff so the noise is shaped like a cloud silhouette.
      const dx = (x - cx) / rx;
      const dy = (y - cy) / ry;
      const dist = Math.sqrt(dx * dx + dy * dy);
      // Smooth elliptical mask (1 in the middle, 0 at the edge, eased)
      const mask = smoothstep(1.0, 0.45, dist);

      // Smooth-threshold the noise → puffy edges, no hard cutoff.
      // Lower threshold = more cloud area; higher = wispier.
      const cloud = smoothstep(threshold - 0.10, threshold + 0.18, n);

      // Final cloud density combines noise and the mask.
      let density = cloud * mask;
      // Boost middle-density pixels so the cloud reads as opaque puffs.
      density = Math.min(1, Math.pow(density, 0.7) * 1.15);

      // Sun-catch: top half gets a warm white tint, bottom is cooler.
      const topness = Math.max(0, (cy * 1.15 - y) / (cy * 1.15));   // 1 at top → 0 at bottom
      const warmR = 1.0;
      const warmG = 0.985 - (1 - topness) * 0.06;
      const warmB = 0.945 - (1 - topness) * 0.18;

      const bi = (y * w + x) * 4;
      body[bi]     = Math.round(255 * warmR);
      body[bi + 1] = Math.round(255 * warmG);
      body[bi + 2] = Math.round(255 * warmB);
      body[bi + 3] = Math.round(255 * density);

      // Underbelly shadow — same density field, blue-gray tint, only
      // the lower 50% of the cloud, faded by distance from middle.
      const bottomBand = smoothstep(0.42, 0.95, (y - cy * 0.65) / (h - cy * 0.65));
      const shadowDensity = density * bottomBand;
      shadow[bi]     = 70;
      shadow[bi + 1] = 92;
      shadow[bi + 2] = 132;
      shadow[bi + 3] = Math.round(255 * shadowDensity * 0.55);
    }
  }

  // Sharp pipeline: raw → blur (soft painted edges) → composite shadow under body → webp.
  const bodyImg = sharp(body, { raw: { width: w, height: h, channels: 4 } }).blur(1.6);
  const shadowImg = sharp(shadow, { raw: { width: w, height: h, channels: 4 } }).blur(6);

  const shadowBuf = await shadowImg.png().toBuffer();
  const composed = await bodyImg.png().toBuffer();

  await sharp({
    create: { width: w, height: h, channels: 4, background: { r: 0, g: 0, b: 0, alpha: 0 } },
  })
    .composite([
      { input: shadowBuf, top: 0, left: 0, blend: 'over' },
      { input: composed,  top: 0, left: 0, blend: 'over' },
    ])
    .webp({ quality: 86, effort: 5 })
    .toFile(resolve(outDir, `cloud-${name}.webp`));

  console.log(`✓ cloud-${name}.webp  (${w}x${h})`);
}

const cfgs = [
  { name: '1', w: 1040, h: 520, seed: 3,  freqX: 0.0048, freqY: 0.0118, octaves: 5, threshold: 0.50, ellipseRx: 0.48, ellipseRy: 0.42 },
  { name: '2', w: 1200, h: 560, seed: 11, freqX: 0.0040, freqY: 0.0108, octaves: 5, threshold: 0.50, ellipseRx: 0.48, ellipseRy: 0.42 },
  { name: '3', w:  840, h: 440, seed: 23, freqX: 0.0056, freqY: 0.0130, octaves: 4, threshold: 0.49, ellipseRx: 0.48, ellipseRy: 0.42 },
  { name: '4', w:  720, h: 380, seed: 41, freqX: 0.0062, freqY: 0.0140, octaves: 4, threshold: 0.48, ellipseRx: 0.48, ellipseRy: 0.42 },
  { name: '5', w: 1440, h: 640, seed: 7,  freqX: 0.0034, freqY: 0.0094, octaves: 5, threshold: 0.51, ellipseRx: 0.48, ellipseRy: 0.42 },
];

for (const c of cfgs) await genCloud(c);
console.log('All clouds generated →', outDir);
