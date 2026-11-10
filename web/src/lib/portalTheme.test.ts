import { afterEach, describe, expect, it } from "vitest";
import {
    CORNER_RADIUS,
    FONT_STACKS,
    applyBrandTheme,
    brandImageURL,
    httpsURL,
    parsePortalTheme,
    scopedVariables,
    signInBackground,
    supportEmail,
    supportHref,
    themeVariables,
} from "./portalTheme";

describe("parsePortalTheme", () => {
    it("keeps a well-formed theme", () => {
        const theme = {
            font: "serif",
            corners: "round",
            appearance: "dark",
            sign_in_layout: "split",
            sign_in_background: "image",
            sign_in_heading: "Welcome to Northwind",
            sign_in_message: "Your campaigns, replies and reports in one place.",
            support_email: "help@northwind.test",
            support_url: "https://northwind.test/help",
            terms_url: "https://northwind.test/terms",
            privacy_url: "https://northwind.test/privacy",
        };
        expect(parsePortalTheme(theme)).toEqual(theme);
    });

    it("drops an unknown choice, so it reads as the default", () => {
        const theme = parsePortalTheme({
            font: "comic",
            corners: "999px",
            appearance: "system",
            sign_in_layout: "url(https://evil.test)",
            sign_in_background: "red; } body { display: none",
        });
        expect(theme).toEqual({});
    });

    it("never passes a non-string through", () => {
        expect(parsePortalTheme({ font: ["serif"], corners: { toString: () => "round" }, sign_in_heading: 7, support_url: true })).toEqual({});
        expect(parsePortalTheme(null)).toEqual({});
        expect(parsePortalTheme("serif")).toEqual({});
    });

    it("holds the copy to plain text of the allowed length", () => {
        const theme = parsePortalTheme({ sign_in_heading: `  Hello\n\tthere‮  ${"x".repeat(100)}`, sign_in_message: "y".repeat(500) });
        expect(theme.sign_in_heading).toHaveLength(60);
        expect(theme.sign_in_heading?.startsWith("Hello there x")).toBe(true);
        expect(theme.sign_in_message).toHaveLength(200);
        expect(parsePortalTheme({ sign_in_heading: " \n " })).toEqual({});
    });

    it("keeps only https links and a plain email address", () => {
        const theme = parsePortalTheme({
            support_url: "javascript:alert(1)",
            terms_url: "http://northwind.test/terms",
            privacy_url: "https://user:pw@northwind.test/privacy",
            support_email: "help@northwind.test?subject=x",
        });
        expect(theme).toEqual({});
        expect(parsePortalTheme({ support_url: "https://localhost/help" })).toEqual({});
    });
});

describe("links and images", () => {
    it("accepts an https address with a real host", () => {
        expect(httpsURL(" https://northwind.test/help ")).toBe("https://northwind.test/help");
        expect(httpsURL("//northwind.test")).toBe("");
        expect(httpsURL(undefined)).toBe("");
    });
    it("accepts a plain mailbox@domain address", () => {
        expect(supportEmail(" help+portal@northwind.test ")).toBe("help+portal@northwind.test");
        for (const bad of ["help@", "Help <help@northwind.test>", "help@northwind", "a@b.test\nBcc: x@y.test", `${"a".repeat(65)}@northwind.test`]) {
            expect(supportEmail(bad)).toBe("");
        }
    });
    it("prefers the support page to the address", () => {
        expect(supportHref({ support_url: "https://northwind.test/help", support_email: "help@northwind.test" })).toBe("https://northwind.test/help");
        expect(supportHref({ support_email: "help@northwind.test" })).toBe("mailto:help@northwind.test");
        expect(supportHref({})).toBe("");
        expect(supportHref(undefined)).toBe("");
    });
    it("takes an image only from http(s) or this origin", () => {
        expect(brandImageURL("https://cdn.test/a.png")).toBe("https://cdn.test/a.png");
        expect(brandImageURL("/brand/a.png")).toBe("/brand/a.png");
        expect(brandImageURL("//cdn.test/a.png")).toBeUndefined();
        expect(brandImageURL("javascript:alert(1)")).toBeUndefined();
        expect(brandImageURL("data:text/html,x")).toBeUndefined();
        expect(brandImageURL(4)).toBeUndefined();
    });
    it("falls back to the accent when the image background has no image", () => {
        expect(signInBackground({ sign_in_background: "image" }, undefined)).toBe("accent");
        expect(signInBackground({ sign_in_background: "image" }, "https://cdn.test/a.png")).toBe("image");
        expect(signInBackground({ sign_in_background: "dark" }, undefined)).toBe("dark");
        expect(signInBackground({}, "https://cdn.test/a.png")).toBe("");
        expect(signInBackground(undefined, undefined)).toBe("");
    });
});

// Everything a theme writes into a custom property: computed colours, a fixed radius, a fixed font stack.
const SAFE_VALUE = /^(oklch\(\d+(\.\d+)?% \d+(\.\d+)? \d+(\.\d+)?( \/ 0\.\d+)?\)|\d+(\.\d+)?rem)$/;
const STACKS = new Set(Object.values(FONT_STACKS));

describe("themeVariables", () => {
    it("is empty for a brand that sets nothing", () => {
        expect(themeVariables(undefined, {})).toEqual({});
        expect(themeVariables("not a colour", {})).toEqual({});
    });

    it("sets the whole scale for light, dark and solid fills from an accent", () => {
        const vars = themeVariables("#dc2626", {});
        for (const step of [50, 100, 200, 300, 400, 500, 600, 700, 800, 900, 950]) {
            expect(vars[`--wb-sky-${step}`]).toMatch(/^oklch\(/);
            expect(vars[`--wb-sky-d-${step}`]).toMatch(/^oklch\(/);
        }
        expect(vars["--wb-sky-s-600"]).toMatch(/^oklch\(/);
        expect(vars["--wb-sky-s-400"]).toBeUndefined();
        expect(vars["--wb-sky-d-50"]).toMatch(/ \/ 0\.12\)$/);
    });

    it("maps corners and font to fixed values", () => {
        expect(themeVariables(undefined, { corners: "sharp" })).toEqual({ "--radius": CORNER_RADIUS.sharp });
        expect(themeVariables(undefined, { corners: "round" })["--radius"]).toBe("0.75rem");
        const serif = themeVariables(undefined, { font: "serif" });
        expect(serif["--wb-font"]).toBe(FONT_STACKS.serif);
        expect(serif["--wb-font-sans"]).toBe(FONT_STACKS.serif);
        expect(themeVariables(undefined, { font: "geometric" })["--wb-font"]).toContain("Manrope Variable");
    });

    it("writes nothing but computed colours and fixed constants", () => {
        const hostile = parsePortalTheme({ font: "serif; background: url(https://evil.test)", corners: "round", sign_in_heading: "} body {" });
        const vars = themeVariables("#7c3aed", hostile);
        for (const [name, value] of Object.entries(vars)) {
            expect(name).toMatch(/^--(wb-sky(-[ds])?-\d+|wb-font(-sans)?|radius)$/);
            expect(STACKS.has(value) || SAFE_VALUE.test(value), `${name}: ${value}`).toBe(true);
        }
    });
});

describe("scopedVariables", () => {
    it("states every value, so it also undoes a theme around it", () => {
        const vars = scopedVariables(undefined, {}, "light");
        expect(vars["--color-sky-600"]).toBe("oklch(58.8% 0.158 241.97)");
        expect(vars["--wb-sky-600"]).toBe(vars["--color-sky-600"]);
        expect(vars["--radius"]).toBe("0.5rem");
        expect(vars["--wb-font"]).toContain("-apple-system");
        expect(vars["--wb-font-sans"]).toContain("Inter Variable");
    });
    it("draws the sky utilities from the appearance asked for", () => {
        const light = scopedVariables("#16a34a", { corners: "sharp", font: "system" }, "light");
        const dark = scopedVariables("#16a34a", {}, "dark");
        expect(light["--color-sky-50"]).not.toContain("/");
        expect(dark["--color-sky-50"]).toContain("/ 0.12");
        expect(dark["--color-sky-600"]).toBe(dark["--wb-sky-d-600"]);
        expect(light["--radius"]).toBe(CORNER_RADIUS.sharp);
        expect(light["--wb-font"]).toBe(FONT_STACKS.system);
    });
});

describe("applyBrandTheme", () => {
    const root = document.documentElement;
    afterEach(() => applyBrandTheme(null));

    it("puts the theme on the root and takes it off again", () => {
        applyBrandTheme({ accent_color: "#dc2626", theme: { corners: "round", font: "system" } });
        expect(root.style.getPropertyValue("--wb-sky-600")).toMatch(/^oklch\(/);
        expect(root.style.getPropertyValue("--radius")).toBe("0.75rem");
        expect(root.style.getPropertyValue("--wb-font")).toBe(FONT_STACKS.system);
        expect(root.hasAttribute("data-accent")).toBe(true);

        applyBrandTheme(null);
        expect(root.style.getPropertyValue("--wb-sky-600")).toBe("");
        expect(root.style.getPropertyValue("--radius")).toBe("");
        expect(root.style.getPropertyValue("--wb-font")).toBe("");
        expect(root.hasAttribute("data-accent")).toBe(false);
    });

    it("drops what the next brand does not set", () => {
        applyBrandTheme({ accent_color: "#dc2626", theme: { corners: "round" } });
        applyBrandTheme({ theme: { font: "system" } });
        expect(root.style.getPropertyValue("--wb-sky-600")).toBe("");
        expect(root.style.getPropertyValue("--radius")).toBe("");
        expect(root.style.getPropertyValue("--wb-font")).toBe(FONT_STACKS.system);
        expect(root.hasAttribute("data-accent")).toBe(false);
    });

    it("ignores an accent that is not a colour and a theme of unknown choices", () => {
        applyBrandTheme({ accent_color: "red;}", theme: { corners: "huge" as never, font: "comic" as never } });
        expect(root.getAttribute("style") ?? "").not.toContain("--wb-");
        expect(root.hasAttribute("data-accent")).toBe(false);
    });

    it("leaves an unbranded page untouched", () => {
        const before = root.getAttribute("style");
        applyBrandTheme(null);
        expect(root.getAttribute("style")).toBe(before);
    });
});
