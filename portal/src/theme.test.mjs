import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { test } from "node:test";

import {
  contrast, DARK_TEXT, DEFAULT_ACCENT, DEFAULT_LOOK, deriveTones, lookOf, mix, parseBrand, parseHex, parseLook, parseTheme,
  SURFACES, textOn, toneVars,
} from "./theme.ts";

const HEX = /^#[0-9a-f]{6}$/;
const css = readFileSync(new URL("./styles.css", import.meta.url), "utf8");

// Every sixth step of each channel, plus the colours a brand is likely to pick and the two extremes.
function sweep() {
  const out = ["#0284c7", "#facc15", "#ef4444", "#22c55e", "#f97316", "#7c3aed", "#111827", "#fefce8", "#ffffff", "#000000"];
  const steps = ["00", "33", "66", "99", "cc", "ff"];
  for (const r of steps) for (const g of steps) for (const b of steps) out.push("#" + r + g + b);
  return out;
}

test("an absent or malformed brand parses to the defaults", () => {
  for (const raw of [undefined, null, "brand", 7, [], { theme: "dark" }, { theme: [] }]) {
    const b = parseBrand(raw);
    assert.equal(b.name, "");
    assert.equal(b.accent, DEFAULT_ACCENT);
    assert.equal(b.logoURL, undefined);
    assert.equal(b.poweredBy, undefined);
    assert.deepEqual(b.theme, {
      font: "default", corners: "default", appearance: "light",
      supportEmail: undefined, supportURL: undefined, termsURL: undefined, privacyURL: undefined,
    });
    assert.deepEqual(lookOf(b), DEFAULT_LOOK);
  }
});

test("a full brand keeps every valid field", () => {
  const b = parseBrand({
    name: "  Northwind Agency  ",
    logo_url: "https://cdn.example.com/logo.png",
    icon_url: "https://cdn.example.com/icon.png",
    accent_color: "#7C3AED",
    website_url: "https://northwind.example",
    powered_by: { name: "Warmbly", url: "https://warmbly.com" },
    theme: {
      font: "serif", corners: "round", appearance: "dark",
      support_email: "help@northwind.example",
      support_url: "https://northwind.example/help",
      terms_url: "https://northwind.example/terms",
      privacy_url: "https://northwind.example/privacy",
      sign_in_headline: "ignored here",
    },
  });
  assert.equal(b.name, "Northwind Agency");
  assert.equal(b.logoURL, "https://cdn.example.com/logo.png");
  assert.equal(b.iconURL, "https://cdn.example.com/icon.png");
  assert.equal(b.accent, "#7c3aed");
  assert.equal(b.websiteURL, "https://northwind.example/");
  assert.deepEqual(b.poweredBy, { name: "Warmbly", url: "https://warmbly.com/" });
  assert.deepEqual(b.theme, {
    font: "serif", corners: "round", appearance: "dark",
    supportEmail: "help@northwind.example",
    supportURL: "https://northwind.example/help",
    termsURL: "https://northwind.example/terms",
    privacyURL: "https://northwind.example/privacy",
  });
  assert.deepEqual(lookOf(b), { accent: "#7c3aed", font: "serif", corners: "round", appearance: "dark" });
});

test("an unknown or empty enum value becomes the default", () => {
  assert.equal(parseTheme({ font: "" }).font, "default");
  assert.equal(parseTheme({ font: "comic" }).font, "default");
  assert.equal(parseTheme({ font: "Serif" }).font, "default");
  assert.equal(parseTheme({ font: ["serif"] }).font, "default");
  assert.equal(parseTheme({ corners: "" }).corners, "default");
  assert.equal(parseTheme({ corners: "pill" }).corners, "default");
  assert.equal(parseTheme({ corners: "sharp; } body { display: none" }).corners, "default");
  assert.equal(parseTheme({ appearance: "" }).appearance, "light");
  assert.equal(parseTheme({ appearance: "auto" }).appearance, "light");
  assert.equal(parseTheme({ appearance: 1 }).appearance, "light");
  for (const font of ["system", "geometric", "serif"]) assert.equal(parseTheme({ font }).font, font);
  for (const corners of ["sharp", "round"]) assert.equal(parseTheme({ corners }).corners, corners);
  for (const appearance of ["light", "dark"]) assert.equal(parseTheme({ appearance }).appearance, appearance);
});

test("a theme link that is not https is dropped", () => {
  const bad = [
    "http://agency.example/help", "javascript:alert(1)", "data:text/html,<p>x</p>", "//agency.example/help", "/help",
    "agency.example/help", "mailto:help@agency.example", "https://user:pass@agency.example/", "", "   ", 42, null,
    "https://" + "a".repeat(2100) + ".example",
  ];
  for (const url of bad) {
    const t = parseTheme({ support_url: url, terms_url: url, privacy_url: url });
    assert.equal(t.supportURL, undefined, String(url));
    assert.equal(t.termsURL, undefined, String(url));
    assert.equal(t.privacyURL, undefined, String(url));
  }
  assert.equal(parseTheme({ terms_url: "https://agency.example/terms?x=1#top" }).termsURL, "https://agency.example/terms?x=1#top");
});

test("the logo, icon and website take http or https and nothing else", () => {
  const ok = parseBrand({ logo_url: "http://localhost:8080/logo.png", icon_url: "https://a.example/i.png", website_url: "http://a.example" });
  assert.equal(ok.logoURL, "http://localhost:8080/logo.png");
  assert.equal(ok.iconURL, "https://a.example/i.png");
  assert.equal(ok.websiteURL, "http://a.example/");
  for (const url of ["javascript:alert(1)", "data:image/png;base64,AAAA", "blob:https://a.example/x", "file:///etc/passwd", "logo.png", 5]) {
    const b = parseBrand({ logo_url: url, icon_url: url, website_url: url, powered_by: { name: "X", url } });
    assert.equal(b.logoURL, undefined, String(url));
    assert.equal(b.iconURL, undefined, String(url));
    assert.equal(b.websiteURL, undefined, String(url));
    assert.deepEqual(b.poweredBy, { name: "X", url: undefined });
  }
  assert.equal(parseBrand({ powered_by: { url: "https://warmbly.com" } }).poweredBy, undefined);
});

test("the support address must be a plain email address", () => {
  assert.equal(parseTheme({ support_email: " help+portal@agency.example " }).supportEmail, "help+portal@agency.example");
  const bad = [
    "help@agency", "help", "@agency.example", "help@agency.example?subject=x", "help@agency.example,evil@x.example",
    "help@agency.example\nBcc: evil@x.example", "help%0A@agency.example", "a b@agency.example", "<help@agency.example>", 9,
  ];
  for (const email of bad) assert.equal(parseTheme({ support_email: email }).supportEmail, undefined, String(email));
});

test("the accent must be #rrggbb", () => {
  assert.equal(parseHex("#0284C7"), "#0284c7");
  const bad = [
    "#abc", "#0284c7ff", "0284c7", "#0284c", "#12345g", "red", "rgb(0,0,0)", " #0284c7", "#0284c7 ",
    "#fff;background:url(https://x.example)", "var(--x)", "", null, 0x0284c7,
  ];
  for (const colour of bad) {
    assert.equal(parseHex(colour), undefined, String(colour));
    assert.equal(parseBrand({ accent_color: colour }).accent, DEFAULT_ACCENT, String(colour));
  }
});

test("a remembered look is read back with the same rules", () => {
  const look = { accent: "#22c55e", font: "geometric", corners: "sharp", appearance: "dark" };
  assert.deepEqual(parseLook(JSON.parse(JSON.stringify(look))), look);
  assert.deepEqual(parseLook({ accent: "url(x)", font: "x", corners: 3, appearance: "dim" }), DEFAULT_LOOK);
  assert.deepEqual(parseLook(null), DEFAULT_LOOK);
});

test("mix blends and clamps", () => {
  assert.equal(mix("#000000", "#ffffff", 0), "#000000");
  assert.equal(mix("#000000", "#ffffff", 1), "#ffffff");
  assert.equal(mix("#000000", "#ffffff", 0.5), "#808080");
  assert.equal(mix("#102030", "#ffffff", 7), "#ffffff");
  assert.equal(mix("#102030", "#ffffff", -1), "#102030");
});

test("contrast is the WCAG ratio", () => {
  assert.equal(Math.round(contrast("#000000", "#ffffff")), 21);
  assert.equal(contrast("#777777", "#777777"), 1);
  assert.equal(contrast("#123456", "#fedcba"), contrast("#fedcba", "#123456"));
  assert.ok(Math.abs(contrast("#767676", "#ffffff") - 4.54) < 0.01);
});

test("text on the accent is white on a dark colour and dark on a light one", () => {
  for (const dark of ["#000000", "#111827", "#0284c7", "#7c3aed", "#1d4ed8", "#b91c1c", "#166534", "#3b82f6", "#ef4444"]) {
    assert.equal(textOn(dark), "#ffffff", dark);
  }
  for (const light of ["#ffffff", "#facc15", "#fefce8", "#a7f3d0", "#22c55e", "#f97316", "#e2e8f0", "#fbbf24"]) {
    assert.equal(textOn(light), DARK_TEXT, light);
  }
});

test("every derived tone is readable where it is used, for any accent in either appearance", () => {
  for (const accent of sweep()) {
    for (const appearance of ["light", "dark"]) {
      const s = SURFACES[appearance];
      const t = deriveTones(accent, appearance);
      const at = `${accent} ${appearance}`;
      for (const [name, value] of Object.entries(t)) assert.match(value, HEX, `${at} ${name}`);
      assert.equal(t.onAccent, textOn(accent), at);
      assert.ok(contrast(t.onAccent, t.accent) >= 4.5, `${at} button text ${contrast(t.onAccent, t.accent)}`);
      assert.ok(contrast(t.onAccent, t.accentHover) >= 4.5, `${at} hover text ${contrast(t.onAccent, t.accentHover)}`);
      for (const ground of [s.page, s.card, t.soft]) {
        assert.ok(contrast(t.link, ground) >= 4.5, `${at} link on ${ground} ${contrast(t.link, ground)}`);
      }
      for (const ground of [s.page, s.card]) {
        assert.ok(contrast(t.ring, ground) >= 3, `${at} ring on ${ground}`);
        assert.ok(contrast(t.chart, ground) >= 3, `${at} chart on ${ground}`);
      }
      assert.ok(contrast(t.accentEdge, s.card) >= 1.5, `${at} button edge ${contrast(t.accentEdge, s.card)}`);
      assert.ok(contrast(s.ink, t.soft) >= 7, `${at} ink on soft`);
    }
  }
});

test("a light accent keeps its own colour on the button and a readable one is not touched", () => {
  assert.equal(deriveTones("#facc15", "light").accent, "#facc15");
  assert.equal(deriveTones("#7c3aed", "light").accent, "#7c3aed");
  assert.equal(deriveTones("#7c3aed", "light").link, "#7c3aed");
  // The default sky is a little short of 4.5 with white, so its button is a shade deeper.
  assert.notEqual(deriveTones(DEFAULT_ACCENT, "light").accent, DEFAULT_ACCENT);
});

test("toneVars names both appearances and holds only colours", () => {
  const vars = toneVars("#7c3aed");
  const names = ["accent", "accent-hover", "on-accent", "accent-edge", "link", "soft", "halo", "ring", "chart"];
  assert.deepEqual(Object.keys(vars).sort(), names.flatMap((n) => [`--pt-d-${n}`, `--pt-l-${n}`]).sort());
  for (const value of Object.values(vars)) assert.match(value, HEX);
});

// The stylesheet repeats two things theme.ts decides; these fail when one side moves alone.
function block(selector) {
  const start = css.indexOf(selector + " {");
  assert.notEqual(start, -1, `styles.css has no ${selector} block`);
  return css.slice(start, css.indexOf("}", start));
}
function declared(source, name) {
  const m = new RegExp(`${name}:\\s*([^;]+);`).exec(source);
  assert.ok(m, `${name} is not declared`);
  return m[1].trim();
}

test("styles.css declares the grounds theme.ts computes against", () => {
  const light = block(":root");
  const dark = block(':root[data-appearance="dark"]');
  for (const [source, surface] of [[light, SURFACES.light], [dark, SURFACES.dark]]) {
    assert.equal(declared(source, "--pt-page"), surface.page);
    assert.equal(declared(source, "--pt-card"), surface.card);
    assert.equal(declared(source, "--pt-ink"), surface.ink);
  }
});

test("styles.css falls back to the default accent's tones", () => {
  const light = block(":root");
  const dark = block(':root[data-appearance="dark"]');
  for (const [name, value] of Object.entries(toneVars(DEFAULT_ACCENT))) {
    const source = name.startsWith("--pt-l-") ? light : dark;
    const tone = name.replace(/^--pt-[ld]-/, "--pt-");
    assert.equal(declared(source, tone), `var(${name}, ${value})`);
  }
});

test("the dark palette is a screen palette, so paper gets the light one", () => {
  const at = css.indexOf("@media screen {");
  assert.ok(at !== -1 && at < css.indexOf(':root[data-appearance="dark"] {'));
  assert.equal(css.split(':root[data-appearance="dark"] {').length, 2);
});
