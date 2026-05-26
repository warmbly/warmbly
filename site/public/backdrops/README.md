# Backdrops

The home page expects one painted backdrop image:

```
site/public/backdrops/hero.webp        ← drop your AI-generated image here
```

Recommended size: **2400 × 1400** (16:9-ish). Export to `.webp` at quality 80
(~100–180 KB). PNG and JPG also work but webp gives the smallest payload.

Until a file exists at that path, `<PaintedSky>` falls back to a CSS-only
painted gradient so the design still works.

---

## Prompts to generate the image

Pick one and run it through Midjourney, Krea, Leonardo, DALL·E, or any
image generator that handles painterly landscapes. All prompts are tuned
to match the "Cursor.com" hero aesthetic (soft, painted, atmospheric).

### Prompt 1 — Misty mountain dawn (closest to Cursor)

> Soft pastel watercolor landscape, misty layered mountains in haze,
> peach and lavender sunrise sky, dreamy painterly atmosphere, subtle
> brush strokes, ultra-clean composition, cinematic, no people, no
> text, no logos, --ar 16:9 --style raw --v 6

### Prompt 2 — Clouds at high altitude

> Oil-paint cloudscape at high altitude, soft cumulus clouds in warm
> golden light, pastel pink and powder blue sky, painterly brush
> strokes, peaceful, vast, no horizon, no objects, no text, abstract
> dreamy mood, --ar 16:9 --style raw --v 6

### Prompt 3 — Lavender alpine sunset

> Wide alpine valley at golden hour, soft lavender mist over rolling
> hills, distant snow-capped peaks fading into haze, painterly oil
> brush style, warm peach sky, subtle, calm, no people, no text,
> --ar 16:9 --style raw --v 6

### Prompt 4 — Cloud bank, calm and minimal

> Single soft cloud bank suspended over a faded mountain ridge, muted
> peach and lilac palette, watercolor wash, vast empty space above
> for typography, painterly, no objects, no text, soft horizon,
> --ar 16:9 --style raw --v 6

---

## Settings cheat-sheet by tool

**Midjourney** — append `--ar 16:9 --style raw --v 6` to any prompt above.
Use `--stylize 200` if it feels too literal.

**Krea.ai** — pick "Flux Schnell" or "Flux Dev". Aspect ratio 16:9. Lower
the prompt strength slightly to keep it soft.

**Leonardo.ai** — model "Phoenix" or "Dreamshaper XL". 16:9. Style preset:
"Cinematic" or "Illustration".

**DALL·E 3 (ChatGPT)** — paste the prompt as-is; ask for "wide / landscape
aspect ratio".

**Ideogram** — turn off "Magic Prompt" so it doesn't add bullet points.

---

## After generating

1. Crop to 16:9 if it isn't already (Figma, Photoshop, GIMP, or
   `magick input.png -gravity center -extent 2400x1400 hero.webp`).
2. Save as `hero.webp` at quality ~80.
3. Drop it at `site/public/backdrops/hero.webp`.
4. Reload — `<img>` will render it automatically.

If you generate multiple variants and want different ones per section,
just edit the `src=` on each `<PaintedSky>` in `site/src/pages/index.astro`.
