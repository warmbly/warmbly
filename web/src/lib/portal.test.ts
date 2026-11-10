import { describe, expect, it } from "vitest";
import {
    NO_PORTAL,
    captchaFrameURL,
    captchaMode,
    handoffCode,
    mainHost,
    mainSignInURL,
    mayBePortal,
    parsePortalInfo,
    portalAgencies,
    portalBrandName,
    portalHostParam,
    portalWorkspaces,
    readCaptchaMessage,
    safeAccent,
    safeHandoffRedirect,
    signsInOnMain,
    type PortalInfo,
} from "./portal";

const portal: PortalInfo = {
    portal: true,
    host: "portal.northwind.test",
    agency_id: "a1",
    brand: { name: "Northwind" },
    main_url: "https://app.warmbly.test",
    captcha_frame: true,
    sign_in: "portal",
};

describe("mayBePortal", () => {
    it("asks only off the main address", () => {
        expect(mayBePortal("https://app.warmbly.test", "https://app.warmbly.test")).toBe(false);
        expect(mayBePortal("https://app.warmbly.test/", "https://app.warmbly.test")).toBe(false);
        expect(mayBePortal("https://app.warmbly.test", "https://portal.northwind.test")).toBe(true);
    });
    it("never asks when the main address is unknown or not a web origin", () => {
        expect(mayBePortal("", "https://portal.northwind.test")).toBe(false);
        expect(mayBePortal("not a url", "https://portal.northwind.test")).toBe(false);
        expect(mayBePortal("javascript:alert(1)", "https://portal.northwind.test")).toBe(false);
    });
});

describe("parsePortalInfo", () => {
    it("keeps a well-formed answer", () => {
        const info = parsePortalInfo({
            portal: true,
            host: "portal.northwind.test",
            agency_id: "a1",
            brand: { name: "Northwind", logo_url: "https://cdn.test/l.png", powered_by: { name: "Warmbly", url: "https://warmbly.test" } },
            main_url: "https://app.warmbly.test/",
            captcha_frame: true,
        });
        expect(info).toEqual({
            portal: true,
            host: "portal.northwind.test",
            agency_id: "a1",
            brand: { name: "Northwind", logo_url: "https://cdn.test/l.png", accent_color: undefined, website_url: undefined, theme: {}, powered_by: { name: "Warmbly", url: "https://warmbly.test" } },
            main_url: "https://app.warmbly.test",
            captcha_frame: true,
            sign_in: "portal",
        });
    });
    it("reads where clients sign in, defaulting to the portal", () => {
        const base = { portal: true, agency_id: "a1", brand: { name: "N" } };
        expect(parsePortalInfo({ ...base, sign_in: "warmbly" })).toMatchObject({ sign_in: "warmbly" });
        expect(parsePortalInfo({ ...base, sign_in: "elsewhere" })).toMatchObject({ sign_in: "portal" });
        expect(parsePortalInfo(base)).toMatchObject({ sign_in: "portal" });
    });
    it("reads the theme, the icon and the sign-in image", () => {
        const info = parsePortalInfo({
            portal: true,
            agency_id: "a1",
            brand: {
                name: "N",
                icon_url: "https://cdn.test/i.png",
                sign_in_image_url: "https://cdn.test/s.jpg",
                theme: { font: "geometric", corners: "sharp", appearance: "dark", sign_in_layout: "cover", sign_in_background: "image", support_url: "https://n.test/help" },
            },
        });
        expect(info.portal && info.brand).toMatchObject({
            icon_url: "https://cdn.test/i.png",
            sign_in_image_url: "https://cdn.test/s.jpg",
            theme: { font: "geometric", corners: "sharp", appearance: "dark", sign_in_layout: "cover", sign_in_background: "image", support_url: "https://n.test/help" },
        });
    });
    it("reads an unknown theme value as the default and never passes a raw string through", () => {
        const info = parsePortalInfo({
            portal: true,
            agency_id: "a1",
            brand: {
                name: "N",
                icon_url: "javascript:alert(1)",
                sign_in_image_url: 'x") ; background: url(https://evil.test',
                theme: { font: "comic", corners: "9px", appearance: "auto", sign_in_layout: "grid", sign_in_background: "url(x)", terms_url: "http://n.test", extra: "kept out" },
            },
        });
        expect(info.portal && info.brand.theme).toEqual({});
        expect(info.portal && info.brand.icon_url).toBeUndefined();
        expect(info.portal && info.brand.sign_in_image_url).toBeUndefined();
        const none = parsePortalInfo({ portal: true, agency_id: "a1", brand: { name: "N", theme: "serif" } });
        expect(none.portal && none.brand.theme).toEqual({});
    });
    it("treats anything malformed as no portal", () => {
        expect(parsePortalInfo(null)).toEqual(NO_PORTAL);
        expect(parsePortalInfo({ portal: false })).toEqual(NO_PORTAL);
        expect(parsePortalInfo({ portal: true, brand: { name: "x" } })).toEqual(NO_PORTAL);
        expect(parsePortalInfo({ portal: true, agency_id: "a1" })).toEqual(NO_PORTAL);
    });
    it("drops a main address that is not a web origin and a captcha flag that is not true", () => {
        const info = parsePortalInfo({ portal: true, agency_id: "a1", brand: { name: "N" }, main_url: "javascript:x", captcha_frame: "yes" });
        expect(info.portal && info.main_url).toBe("");
        expect(info.portal && info.captcha_frame).toBe(false);
    });
});

describe("portal filters", () => {
    const orgs = [{ id: "1", agency: { id: "a1" } }, { id: "2", agency: { id: "a2" } }, { id: "3", agency: null }];
    it("keeps only the owning agency's workspaces and agency view", () => {
        expect(portalWorkspaces(orgs, portal).map((o) => o.id)).toEqual(["1"]);
        expect(portalAgencies([{ id: "a1" }, { id: "a2" }], portal)).toEqual([{ id: "a1" }]);
    });
    it("changes nothing on the main address", () => {
        expect(portalWorkspaces(orgs, NO_PORTAL)).toBe(orgs);
        expect(portalAgencies([{ id: "a2" }], NO_PORTAL)).toEqual([{ id: "a2" }]);
    });
});

describe("names and notes", () => {
    it("uses the agency's name on a portal", () => {
        expect(portalBrandName(portal, "Warmbly")).toBe("Northwind");
        expect(portalBrandName(NO_PORTAL, "Warmbly")).toBe("Warmbly");
        expect(mainHost(portal)).toBe("app.warmbly.test");
        expect(mainHost({ ...portal, main_url: "" })).toBe("the main address");
    });
    it("accepts only a six-digit hex accent", () => {
        expect(safeAccent("#0ea5e9")).toBe("#0ea5e9");
        expect(safeAccent("red")).toBe("");
        expect(safeAccent("#fff; background:url(x)")).toBe("");
        expect(safeAccent(undefined)).toBe("");
    });
});

describe("captcha bridge", () => {
    const frame = {};
    const api = "https://api.warmbly.test";
    it("picks the frame on a portal that can render it", () => {
        expect(captchaMode(NO_PORTAL)).toBe("widget");
        expect(captchaMode(portal)).toBe("frame");
        expect(captchaMode({ ...portal, captcha_frame: false })).toBe("unavailable");
    });
    it("builds the bridge URL for this page's origin", () => {
        expect(captchaFrameURL(`${api}/v1`, "https://portal.northwind.test")).toBe(`${api}/v1/auth/captcha-frame?origin=https%3A%2F%2Fportal.northwind.test`);
    });
    it("accepts a token only from its own frame on the API origin", () => {
        const data = { type: "warmbly:captcha", token: "t0k" };
        expect(readCaptchaMessage({ origin: api, source: frame, data }, api, frame)).toEqual({ token: "t0k", error: false });
        expect(readCaptchaMessage({ origin: "https://evil.test", source: frame, data }, api, frame)).toBeNull();
        expect(readCaptchaMessage({ origin: api, source: {}, data }, api, frame)).toBeNull();
        expect(readCaptchaMessage({ origin: api, source: frame, data }, api, null)).toBeNull();
        expect(readCaptchaMessage({ origin: api, source: frame, data }, "", frame)).toBeNull();
    });
    it("ignores other messages and reports expiry and errors", () => {
        expect(readCaptchaMessage({ origin: api, source: frame, data: { type: "other", token: "x" } }, api, frame)).toBeNull();
        expect(readCaptchaMessage({ origin: api, source: frame, data: "warmbly:captcha" }, api, frame)).toBeNull();
        expect(readCaptchaMessage({ origin: api, source: frame, data: { type: "warmbly:captcha", token: "" } }, api, frame)).toEqual({ token: "", error: false });
        expect(readCaptchaMessage({ origin: api, source: frame, data: { type: "warmbly:captcha", token: "", error: true } }, api, frame)).toEqual({ token: "", error: true });
    });
});

describe("sign-in on the main address", () => {
    const onMain: PortalInfo = { ...portal, sign_in: "warmbly" };
    it("is used only when the agency chose it and the main address is known", () => {
        expect(signsInOnMain(onMain)).toBe(true);
        expect(signsInOnMain(portal)).toBe(false);
        expect(signsInOnMain({ ...onMain, main_url: "" })).toBe(false);
        expect(signsInOnMain(NO_PORTAL)).toBe(false);
    });
    it("names the portal host on the main address", () => {
        expect(mainSignInURL(onMain, "portal.northwind.test")).toBe(
            "https://app.warmbly.test/portal/sign-in?host=portal.northwind.test",
        );
        expect(mainSignInURL(NO_PORTAL, "portal.northwind.test")).toBe("");
    });
    it("accepts only a bare host name", () => {
        expect(portalHostParam(" Portal.Northwind.Test ")).toBe("portal.northwind.test");
        for (const bad of ["", "https://portal.northwind.test", "portal.northwind.test/x", "evil.test:444", "localhost", "a b.test"]) {
            expect(portalHostParam(bad)).toBe("");
        }
    });
    it("reads the code only from the fragment's code parameter", () => {
        const code = "abcdefghijklmnopqrstuvwxyz012345_-AB";
        expect(handoffCode(`#code=${code}`)).toBe(code);
        expect(handoffCode("#code=short")).toBe("");
        expect(handoffCode("#code=has%20space%20in%20it%20padding")).toBe("");
        expect(handoffCode("")).toBe("");
    });
    it("follows a handoff redirect only to the portal return page on the host asked for", () => {
        const ok = "https://portal.northwind.test/portal/return#code=abc";
        expect(safeHandoffRedirect(ok, "portal.northwind.test")).toBe(ok);
        expect(safeHandoffRedirect("https://evil.test/portal/return#code=abc", "portal.northwind.test")).toBe("");
        expect(safeHandoffRedirect("https://portal.northwind.test/app#code=abc", "portal.northwind.test")).toBe("");
        expect(safeHandoffRedirect("javascript:alert(1)", "portal.northwind.test")).toBe("");
    });
});
