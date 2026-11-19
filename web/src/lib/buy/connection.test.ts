import { describe, expect, it } from "vitest";
import type { DnsRecord, StoreDomain } from "@/lib/api/models/app/store/Store";
import { fullRecordName, looksLikeDomain, manualRecords, normalizeDomainInput, ownedFromDomain, readyForMailboxes, timeLeft } from "./connection";

const domain = (over: Partial<StoreDomain>): StoreDomain =>
    ({
        id: "d1",
        name: "acme.com",
        status: "connecting",
        provider: null,
        mailbox_count: 0,
        source: "connected",
        connection: { method: "nameservers", status: "pending", expires_at: null, checked_at: null, current_nameservers: [] },
        dns_conflicts: [],
        ...over,
    }) as StoreDomain;

const rec = (purpose: string, name = "@"): DnsRecord => ({ id: purpose, type: "TXT", name, content: "x", ttl: 300, priority: null, managed: true, purpose });

describe("domain input", () => {
    it("strips a pasted address down to the domain", () => {
        expect(normalizeDomainInput("  https://www.Acme.com/about ")).toBe("acme.com");
        expect(normalizeDomainInput("acme.co.uk.")).toBe("acme.co.uk");
    });

    it("accepts domain shapes only", () => {
        expect(looksLikeDomain("acme.com")).toBe(true);
        expect(looksLikeDomain("mail.acme.co.uk")).toBe(true);
        expect(looksLikeDomain("acme")).toBe(false);
        expect(looksLikeDomain("acme .com")).toBe(false);
        expect(looksLikeDomain("-acme.com")).toBe(false);
    });
});

describe("readyForMailboxes", () => {
    it("waits for an active connection with no conflicts", () => {
        expect(readyForMailboxes(domain({}))).toBe(false);
        const active = { method: "nameservers" as const, status: "active", expires_at: null, checked_at: null, current_nameservers: [] };
        expect(readyForMailboxes(domain({ connection: active }))).toBe(true);
        expect(readyForMailboxes(domain({ connection: active, dns_conflicts: [{ record_id: null, type: "MX", name: "@", content: "mx.old" }] }))).toBe(false);
    });

    it("takes a purchased domain once it is active", () => {
        expect(readyForMailboxes(domain({ source: "purchased", status: "active", connection: null }))).toBe(true);
        expect(readyForMailboxes(domain({ source: "purchased", status: "registering", connection: null }))).toBe(false);
    });
});

describe("ownedFromDomain", () => {
    it("fixes provider and admin only once the domain has mailboxes", () => {
        expect(ownedFromDomain(domain({ provider: "google" }))).toEqual({ domain: "acme.com", provider: null, hasAdmin: false });
        expect(ownedFromDomain(domain({ provider: "google", mailbox_count: 2 }))).toEqual({ domain: "acme.com", provider: "google", hasAdmin: true });
    });
});

describe("manual records", () => {
    it("puts the ownership record first", () => {
        expect(manualRecords([rec("mx"), rec("spf"), rec("ownership", "_warmbly")]).map((r) => r.purpose)).toEqual(["ownership", "mx", "spf"]);
        expect(manualRecords(undefined)).toEqual([]);
    });

    it("spells a relative name out in full", () => {
        expect(fullRecordName("@", "acme.com")).toBe("acme.com");
        expect(fullRecordName("_dmarc", "acme.com")).toBe("_dmarc.acme.com");
        expect(fullRecordName("_dmarc.acme.com", "acme.com")).toBe("_dmarc.acme.com");
    });
});

describe("timeLeft", () => {
    const now = Date.UTC(2026, 10, 18, 12, 0);
    it("counts down hours and minutes, and stops at zero", () => {
        expect(timeLeft(new Date(now + (47 * 60 + 12) * 60_000), now)).toBe("47 h 12 min left");
        expect(timeLeft(new Date(now + 5 * 60_000 + 30_000), now)).toBe("5 min left");
        expect(timeLeft(new Date(now - 1000), now)).toBeNull();
        expect(timeLeft(null, now)).toBeNull();
    });
});
