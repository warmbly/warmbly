import { describe, expect, it } from "vitest";
import { appNameError, nameError } from "./displayName";

describe("nameError", () => {
    it.each(["Ada", "  Mary   Ann ", "O'Brien-Smith", "J.R.R. Tolkien", "St. John", "Zoë", "李小龙"])(
        "accepts %s",
        (v) => expect(nameError("Name", v, "person")).toBeNull(),
    );

    it.each([
        "https://www.google.com",
        "google.com",
        "www.example",
        "a@b",
        "javascript:alert(1)",
        "mailto:x",
        "ｇｏｏｇｌｅ．ｃｏｍ",
        "google。com",
        "10.0.0.1",
    ])("refuses the link %s", (v) => expect(nameError("Name", v, "person")).toMatch(/cannot contain a link/));

    it.each(["Ada‮evil", "Ada​", "<b>Ada</b>", "Ź́́́́"])(
        "refuses the characters in %s",
        (v) => expect(nameError("Name", v, "person")).toMatch(/not allowed/),
    );

    it("bounds length by kind", () => {
        expect(nameError("Name", "a".repeat(51), "person")).toMatch(/50/);
        expect(nameError("Name", "a".repeat(64), "workspace")).toBeNull();
    });

    it("allows an empty optional name", () => {
        expect(nameError("Name", " ", "person", true)).toBeNull();
        expect(nameError("Name", " ", "person")).toMatch(/required/);
    });

    it("lets a workspace be only digits", () => {
        expect(nameError("Workspace name", "3000", "workspace")).toBeNull();
        expect(nameError("Name", "3000", "person")).toMatch(/letter/);
    });
});

describe("appNameError", () => {
    it.each(["Warmbly", "Warmbly Support", "HubSpot for warmbly", "W a r m b l y", "WarmbIy", "Warrnbly", "VVarmbly", "Wаrmbly", "Ｗａｒｍｂｌｙ"])(
        "refuses %s",
        (v) => expect(appNameError("Name", v)).toMatch(/cannot include Warmbly/),
    );

    it.each(["Acme Sync", "Warm Leads", "Swarm"])("accepts %s", (v) => expect(appNameError("Name", v)).toBeNull());

    it("keeps an existing name editable", () => {
        expect(appNameError("Name", "Warmbly Sync", "Warmbly Sync")).toBeNull();
        expect(appNameError("Name", "Warmbly Sync 2", "Warmbly Sync")).toMatch(/cannot include Warmbly/);
    });
});
