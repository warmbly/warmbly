import { beforeEach, describe, expect, it, vi } from "vitest";
import type { QuoteItem } from "@/lib/api/models/app/store/Store";
import {
    adminAddress,
    canAddSender,
    cartBlocker,
    cartEstimate,
    cartReducer,
    cartStorageKey,
    effectivePerSender,
    perSenderClamped,
    domainIssue,
    domainMailboxes,
    effectiveProvider,
    emptyCart,
    loadCart,
    mailboxCount,
    parseStoredCart,
    quoteItems,
    registersDomain,
    saveCart,
    serializeCart,
    collisionNote,
    effectivePatterns,
    DEFAULT_SIGNATURE,
    sendersSummary,
    type Cart,
    type CartAction,
} from "./cart";
import { fillSignature } from "@/lib/email/signaturePlaceholders";

const ada = { first: "Ada", last: "Lovelace" };
const run = (actions: CartAction[], start: Cart = emptyCart(ada)) => actions.reduce(cartReducer, start);
/** Ada with the per-sender count set to one by hand, for cases about something other than the count. */
const oneEach = (): Cart => cartReducer(emptyCart(ada), { type: "per_sender", count: 1 });
const add = (domain: string): CartAction => ({ type: "add", domain });
const addresses = (c: Cart, domain: string) => domainMailboxes(c, c.domains.find((d) => d.domain === domain)!).map((m) => m.local_part);
const boxesOf = (items: QuoteItem[], domain: string) => {
    const it = items.find((i) => i.type === "mailboxes" && i.domain === domain);
    return it?.type === "mailboxes" ? it : undefined;
};
const now = 1_000_000_000;
const photo = (id: string, at = now) => ({ id, at });

/** Ada, plus Grace and Alan added as senders. */
function threeSenders(): Cart {
    return run([
        { type: "sender_add" },
        { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" },
        { type: "sender_add" },
        { type: "sender_name", id: "s3", first: "Alan", last: "Turing" },
    ]);
}

describe("address patterns in the cart", () => {
    it("defaults to three patterns of one sender and first.last for a team", () => {
        expect(addresses(run([add("getacme.com")]), "getacme.com")).toEqual(["ada", "ada.l", "ada.lovelace"]);
        const two = run([add("getacme.com"), { type: "sender_add" }, { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" }]);
        expect(effectivePatterns(two)).toEqual(["first.last"]);
        // Two each, so each sender's second mailbox falls through to the next pattern in the catalog.
        expect(addresses(two, "getacme.com")).toEqual(["ada.lovelace", "ada", "grace.hopper", "grace"]);
    });

    it("applies the chosen patterns, in order, to every domain that follows the senders", () => {
        let cart = run([add("getacme.com"), add("tryacme.com")], oneEach());
        expect(addresses(cart, "getacme.com")).toEqual(["ada"]);
        cart = cartReducer(cart, { type: "patterns", patterns: ["flast", "first.last"] });
        expect(addresses(cart, "getacme.com")).toEqual(["alovelace"]);
        cart = cartReducer(cart, { type: "per_sender", count: 3 });
        expect(addresses(cart, "tryacme.com")).toEqual(["alovelace", "ada.lovelace", "ada"]);
        // An empty choice is refused; automatic comes back on request.
        expect(cartReducer(cart, { type: "patterns", patterns: [] })).toBe(cart);
        expect(cartReducer(cart, { type: "patterns_auto" }).patterns).toBeNull();
    });

    it("falls through to the next pattern when two senders collide, with a note, and numbers only as a last resort", () => {
        const twoAdas = run([add("getacme.com"), { type: "sender_add" }, { type: "sender_name", id: "s2", first: "Ada", last: "Byron" }, { type: "patterns", patterns: ["first"] }], oneEach());
        expect(addresses(twoAdas, "getacme.com")).toEqual(["ada", "ada.b"]);
        const boxes = domainMailboxes(twoAdas, twoAdas.domains[0]);
        expect(boxes.map((b) => b.moved)).toEqual([undefined, "pattern"]);
        expect(collisionNote(boxes, "getacme.com")).toBe("Two senders would share an address, so Ada gets the next pattern: ada.b@.");
        expect(domainIssue(twoAdas, twoAdas.domains[0])).toBeNull();

        // Two senders with the same name run out of patterns only after all twelve.
        const twins = run([{ type: "per_sender", count: 2 }, add("getacme.com"), { type: "sender_add" }, { type: "sender_name", id: "s2", first: "Ada", last: "Lovelace" }]);
        const t = domainMailboxes(twins, twins.domains[0]);
        expect(t.map((b) => b.local_part)).toEqual(["ada.lovelace", "ada", "ada.l", "adalovelace"]);
        expect(t.some((b) => b.moved === "numbered")).toBe(false);
        const many = run([{ type: "per_sender", count: 1 }, { type: "sender_add" }, { type: "sender_name", id: "s2", first: "Bo", last: "X" }, add("getacme.com")], emptyCart({ first: "Bo", last: "X" }));
        const custom = run([{ type: "slot_count", domain: "getacme.com", count: 5 }, { type: "slot_sender", domain: "getacme.com", index: 4, sender: "s2" }], many);
        expect(new Set(addresses(custom, "getacme.com")).size).toBe(5);
        expect(domainIssue(custom, custom.domains[0])).toBeNull();
    });
});

describe("senders to mailboxes", () => {
    it("starts with one sender, the buyer, and three distinct address styles of them per domain", () => {
        const cart = run([add("getacme.com")]);
        expect(cart.senders).toEqual([{ id: "s1", first: "Ada", last: "Lovelace" }]);
        expect(quoteItems(cart)).toEqual([
            { type: "domain", domain: "getacme.com", years: 1 },
            {
                type: "mailboxes",
                domain: "getacme.com",
                provider: "google",
                mailboxes: [
                    { local_part: "ada", first_name: "Ada", last_name: "Lovelace", admin: true, signature: { html: "Ada Lovelace", plain: "Ada Lovelace" } },
                    { local_part: "ada.l", first_name: "Ada", last_name: "Lovelace", signature: { html: "Ada Lovelace", plain: "Ada Lovelace" } },
                    { local_part: "ada.lovelace", first_name: "Ada", last_name: "Lovelace", signature: { html: "Ada Lovelace", plain: "Ada Lovelace" } },
                ],
            },
        ]);
    });

    it("runs one sender's addresses through the chosen patterns, then the rest, never repeating", () => {
        const cart = run([add("getacme.com"), { type: "patterns", patterns: ["first.last"] }]);
        expect(addresses(cart, "getacme.com")).toEqual(["ada.lovelace", "ada", "ada.l"]);
        // A name with no last name has one pattern; the next mailboxes are numbered.
        const bo = cartReducer(run([add("getacme.com")], emptyCart({ first: "Bo", last: "" })), { type: "patterns", patterns: ["flast"] });
        const got = addresses(bo, "getacme.com");
        expect(new Set(got).size).toBe(3);
        expect(got[0]).toBe("bo");
    });

    it("sends each mailbox its signature filled in like a bulk signature, and an empty one for none", () => {
        let cart = run([add("getacme.com")], oneEach());
        const sig = () => boxesOf(quoteItems(cart), "getacme.com")?.mailboxes[0].signature;
        const ada = { name: "Ada Lovelace", email: "ada@getacme.com" };
        expect(sig()).toEqual(fillSignature(DEFAULT_SIGNATURE, ada));
        expect(sig()).toEqual({ html: "Ada Lovelace", plain: "Ada Lovelace" });

        const html = '<p><b>{{.Sender.FirstName}}</b> {{ .Sender.LastName }}<br><a href="mailto:{{.Sender.Email}}">{{.Sender.Email}}</a></p>';
        cart = run([{ type: "signature", choice: "custom" }, { type: "signature_custom", template: { html } }], cart);
        expect(sig()).toEqual(fillSignature({ html, plain: "", plainSync: true }, ada));
        expect(sig()?.html).toBe('<p><b>Ada</b> Lovelace<br><a href="mailto:ada@getacme.com">ada@getacme.com</a></p>');
        expect(sig()?.plain).toBe("Ada Lovelace\nada@getacme.com");
        // A plain text written on its own is filled on its own.
        cart = cartReducer(cart, { type: "signature_custom", template: { plainSync: false, plain: "-- {{.Sender.Name}}" } });
        expect(sig()?.plain).toBe("-- Ada Lovelace");

        cart = cartReducer(cart, { type: "signature", choice: "none" });
        expect(sig()).toEqual({ html: "", plain: "" });
        // The custom template waits for the buyer to come back to it.
        expect(cartReducer(cart, { type: "signature", choice: "custom" }).signature.custom.html).toBe(html);
    });

    it("summarises what every domain gets in one line", () => {
        expect(sendersSummary(run([]))).toBe("3 mailboxes per domain · ada@, ada.l@, ada.lovelace@ · Google · Signature: Default");
        const team = run([{ type: "default_provider", provider: "microsoft" }, { type: "signature", choice: "none" }], threeSenders());
        expect(sendersSummary(team)).toBe("3 mailboxes per domain · ada.lovelace@, grace.hopper@, alan.turing@ · Microsoft · Signature: None");
        const pair = run([{ type: "sender_add" }, { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" }, { type: "per_sender", count: 2 }, { type: "signature", choice: "custom" }]);
        expect(sendersSummary(pair)).toBe("4 mailboxes per domain · ada.lovelace@, ada@, grace.hopper@ +1 · Google · Signature: Custom");
    });

    it("adapts the per-sender count to the senders until it is set by hand", () => {
        let cart = run([add("getacme.com")]);
        expect(effectivePerSender(cart)).toBe(3);
        cart = run([{ type: "sender_add" }, { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" }], cart);
        expect(effectivePerSender(cart)).toBe(2);
        expect(addresses(cart, "getacme.com")).toEqual(["ada.lovelace", "ada", "grace.hopper", "grace"]);
        for (const n of [3, 4, 5]) {
            cart = cartReducer(cart, { type: "sender_add" });
            expect(cart.senders).toHaveLength(n);
            expect(effectivePerSender(cart)).toBe(1);
        }
        cart = cartReducer(cart, { type: "sender_remove", id: "s5" });
        cart = cartReducer(cart, { type: "sender_remove", id: "s4" });
        cart = cartReducer(cart, { type: "sender_remove", id: "s3" });
        expect(effectivePerSender(cart)).toBe(2);
    });

    it("keeps a hand-set count, lowered to fit five per domain, and never blocks adding a sender", () => {
        let cart = run([add("getacme.com"), { type: "per_sender", count: 4 }]);
        expect(cart.perSender).toBe(4);
        expect(perSenderClamped(cart)).toBeNull();
        cart = cartReducer(cart, { type: "sender_add" });
        expect(cart.senders).toHaveLength(2);
        // 2 x 4 would pass five: it runs at 2 and says so, and the buyer's 4 comes back with one sender.
        expect(effectivePerSender(cart)).toBe(2);
        expect(perSenderClamped(cart)).toBe(4);
        expect(mailboxCount(cart)).toBe(4);
        cart = cartReducer(cart, { type: "sender_remove", id: "s2" });
        expect(effectivePerSender(cart)).toBe(4);
        expect(perSenderClamped(cart)).toBeNull();
        // Back to automatic on request.
        cart = cartReducer(cart, { type: "per_sender_auto" });
        expect(cart.perSender).toBeNull();
        expect(effectivePerSender(cart)).toBe(3);
    });

    it("gives every domain one mailbox per sender from three senders on, named after them", () => {
        const cart = cartReducer(threeSenders(), add("getacme.com"));
        const item = boxesOf(quoteItems(cart), "getacme.com");
        expect(item?.mailboxes.map((m) => [m.local_part, m.first_name, m.last_name])).toEqual([
            ["ada.lovelace", "Ada", "Lovelace"],
            ["grace.hopper", "Grace", "Hopper"],
            ["alan.turing", "Alan", "Turing"],
        ]);
        expect(mailboxCount(cart)).toBe(3);
    });

    it("renames every following domain when a sender changes", () => {
        let cart = cartReducer(threeSenders(), add("getacme.com"));
        cart = cartReducer(cart, add("tryacme.com"));
        cart = cartReducer(cart, { type: "sender_name", id: "s2", first: "Katherine", last: "Johnson" });
        expect(addresses(cart, "getacme.com")).toEqual(["ada.lovelace", "katherine.johnson", "alan.turing"]);
        expect(addresses(cart, "tryacme.com")).toEqual(["ada.lovelace", "katherine.johnson", "alan.turing"]);
    });

    it("takes a hand-set count within the domain's limit for the senders there are", () => {
        let cart = run([add("getacme.com"), { type: "per_sender", count: 9 }]);
        expect(cart.perSender).toBe(5);
        cart = run([{ type: "sender_add" }, { type: "per_sender", count: 9 }], cart);
        expect(cart.perSender).toBe(2);
        expect(canAddSender(cart)).toBe(true);
    });

    it("caps the order at five senders", () => {
        let cart = run([{ type: "per_sender", count: 5 }]);
        for (let i = 0; i < 7; i++) cart = cartReducer(cart, { type: "sender_add" });
        expect(cart.senders).toHaveLength(5);
        expect(canAddSender(cart)).toBe(false);
        expect(effectivePerSender(cart)).toBe(1);
    });

    it("needs a first and last name on every sender before paying", () => {
        const cart = run([add("getacme.com"), { type: "sender_add" }]);
        expect(cartBlocker(cart)).toMatch(/sender/);
        const named = cartReducer(cart, { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" });
        expect(cartBlocker(named)).toBeNull();
        const link = cartReducer(named, { type: "sender_name", id: "s2", first: "grace.example.com" });
        expect(cartBlocker(link)).toMatch(/sender/);
    });

    it("keeps the admin on the same sender's mailbox when the automatic count changes", () => {
        let cart = run([add("getacme.com"), { type: "admin", domain: "getacme.com", index: 1 }]);
        expect(adminAddress(cart, cart.domains[0])).toBe("ada.l@getacme.com");
        cart = run([{ type: "sender_add" }, { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" }], cart);
        // Ada's second mailbox is still the admin, now in the team's patterns.
        expect(adminAddress(cart, cart.domains[0])).toBe("ada@getacme.com");
        cart = run([{ type: "admin", domain: "getacme.com", index: 3 }, { type: "sender_add" }], cart);
        // Grace's second mailbox is gone at one each; the admin stays Grace's.
        expect(adminAddress(cart, cart.domains[0])).toBe("grace.hopper@getacme.com");
    });

    it("moves the admin with its sender when an earlier sender is removed", () => {
        let cart = cartReducer(threeSenders(), add("getacme.com"));
        cart = cartReducer(cart, { type: "admin", domain: "getacme.com", index: 2 });
        expect(adminAddress(cart, cart.domains[0])).toBe("alan.turing@getacme.com");
        cart = cartReducer(cart, { type: "sender_remove", id: "s1" });
        expect(adminAddress(cart, cart.domains[0])).toBe("alan.turing@getacme.com");
        cart = cartReducer(cart, { type: "sender_remove", id: "s3" });
        expect(adminAddress(cart, cart.domains[0])).toBe("grace@getacme.com");
        expect(cartReducer(cart, { type: "sender_remove", id: "s2" })).toBe(cart);
    });

    it("names a photo every mailbox shares once, and a photo per mailbox when senders differ", () => {
        let cart = run([add("getacme.com"), { type: "sender_photo", id: "s1", photo: photo("p-ada") }], oneEach());
        expect(boxesOf(quoteItems(cart), "getacme.com")).toMatchObject({ photo_id: "p-ada" });
        cart = cartReducer(cart, { type: "sender_add" });
        cart = cartReducer(cart, { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" });
        cart = cartReducer(cart, { type: "sender_photo", id: "s2", photo: photo("p-grace") });
        cart = cartReducer(cart, { type: "per_sender", count: 2 });
        const item = boxesOf(quoteItems(cart), "getacme.com");
        expect(item?.photo_id).toBeUndefined();
        // One upload per sender, reused on every mailbox of theirs.
        expect(item?.mailboxes.map((m) => m.photo_id)).toEqual(["p-ada", "p-ada", "p-grace", "p-grace"]);
        const none = cartReducer(cart, { type: "sender_photo", id: "s2", photo: null });
        expect(boxesOf(quoteItems(none), "getacme.com")?.mailboxes.map((m) => m.photo_id)).toEqual(["p-ada", "p-ada", undefined, undefined]);
    });

    it("seeds the first sender from the account and its photo only once", () => {
        let cart = emptyCart();
        cart = cartReducer(cart, { type: "seed_sender", person: ada });
        expect(cart.senders[0]).toMatchObject(ada);
        expect(cartReducer(cart, { type: "seed_sender", person: { first: "X", last: "Y" } })).toBe(cart);
        cart = cartReducer(cart, { type: "seed_photo", photo: photo("p") });
        expect(cartReducer(cart, { type: "seed_photo", photo: photo("q") })).toBe(cart);
        const cleared = cartReducer(cart, { type: "sender_photo", id: "s1", photo: null });
        expect(cartReducer(cleared, { type: "seed_photo", photo: photo("q") })).toBe(cleared);
    });
});

describe("customizing a domain", () => {
    it("keeps a customized domain to itself while the others follow the senders", () => {
        let cart = run([add("getacme.com"), add("tryacme.com")], oneEach());
        cart = cartReducer(cart, { type: "local_part", domain: "getacme.com", index: 0, value: "Founder" });
        cart = cartReducer(cart, { type: "slot_count", domain: "getacme.com", count: 2 });
        expect(addresses(cart, "getacme.com")).toEqual(["founder", "ada"]);
        expect(addresses(cart, "tryacme.com")).toEqual(["ada"]);

        cart = cartReducer(cart, { type: "sender_name", id: "s1", first: "Grace", last: "Hopper" });
        // The typed address stays; the untyped one still follows the name, so the global link is kept.
        expect(addresses(cart, "getacme.com")).toEqual(["founder", "grace"]);
        expect(addresses(cart, "tryacme.com")).toEqual(["grace"]);
    });

    it("does not change a customized domain's count when senders come and go", () => {
        let cart = run([add("getacme.com"), add("tryacme.com"), { type: "slot_count", domain: "getacme.com", count: 2 }], oneEach());
        cart = cartReducer(cart, { type: "sender_add" });
        cart = cartReducer(cart, { type: "sender_name", id: "s2", first: "Grace", last: "Hopper" });
        expect(addresses(cart, "getacme.com")).toHaveLength(2);
        expect(addresses(cart, "tryacme.com")).toEqual(["ada.lovelace", "grace.hopper"]);
        cart = cartReducer(cart, { type: "slot_sender", domain: "getacme.com", index: 1, sender: "s2" });
        expect(addresses(cart, "getacme.com")).toEqual(["ada.lovelace", "grace.hopper"]);
        cart = cartReducer(cart, { type: "sender_remove", id: "s2" });
        expect(addresses(cart, "getacme.com")).toEqual(["ada", "ada.l"]);
    });

    it("goes back to following the senders on reset", () => {
        let cart = run([add("getacme.com"), { type: "local_part", domain: "getacme.com", index: 0, value: "sales" }], oneEach());
        expect(cart.domains[0].custom).not.toBeNull();
        cart = cartReducer(cart, { type: "uncustomize", domain: "getacme.com" });
        expect(cart.domains[0].custom).toBeNull();
        expect(addresses(cart, "getacme.com")).toEqual(["ada"]);
    });

    it("flags duplicate and invalid addresses across senders, inline per domain", () => {
        let cart = cartReducer(threeSenders(), add("getacme.com"));
        // A suggested address steps aside for a typed one; two typed the same are flagged.
        cart = cartReducer(cart, { type: "local_part", domain: "getacme.com", index: 2, value: "grace.hopper" });
        expect(addresses(cart, "getacme.com")).toEqual(["ada.lovelace", "grace", "grace.hopper"]);
        cart = cartReducer(cart, { type: "local_part", domain: "getacme.com", index: 0, value: "grace.hopper" });
        expect(domainIssue(cart, cart.domains[0])).toBe("One address needs a fix.");
        expect(cartBlocker(cart)).toMatch(/getacme\.com/);
        cart = cartReducer(cart, { type: "local_part", domain: "getacme.com", index: 1, value: "-bad" });
        expect(domainIssue(cart, cart.domains[0])).toBe("2 addresses need a fix.");
        cart = cartReducer(cart, { type: "auto_address", domain: "getacme.com", index: 0 });
        cart = cartReducer(cart, { type: "auto_address", domain: "getacme.com", index: 1 });
        expect(domainIssue(cart, cart.domains[0])).toBeNull();
    });

    it("lets one domain pick its own provider and follows the order's otherwise", () => {
        let cart = run([add("getacme.com"), add("tryacme.com"), { type: "provider", domain: "tryacme.com", provider: "microsoft" }]);
        expect(cart.domains.map((d) => effectiveProvider(cart, d))).toEqual(["google", "microsoft"]);
        cart = cartReducer(cart, { type: "default_provider", provider: "microsoft" });
        expect(cart.domains.map((d) => effectiveProvider(cart, d))).toEqual(["microsoft", "microsoft"]);
        // The override that matched the new default now just follows it.
        expect(cart.domains[1].provider).toBeNull();
        cart = cartReducer(cart, { type: "default_provider", provider: "google" });
        expect(cart.domains.map((d) => effectiveProvider(cart, d))).toEqual(["google", "google"]);
    });
});

describe("owned domains", () => {
    const owned = (hasAdmin: boolean, provider: "google" | "microsoft" | null = null): CartAction => ({ type: "add_owned", owned: { domain: "acme.com", provider, hasAdmin } });

    it("buys only mailboxes on a domain the workspace already has", () => {
        const cart = run([owned(false)], oneEach());
        expect(registersDomain(cart)).toBe(false);
        expect(quoteItems(cart)).toEqual([{ type: "mailboxes", domain: "acme.com", provider: "google", mailboxes: [{ local_part: "ada", first_name: "Ada", last_name: "Lovelace", admin: true, signature: { html: "Ada Lovelace", plain: "Ada Lovelace" } }] }]);
    });

    it("sends no admin and keeps the provider for a domain that already has mailboxes", () => {
        let cart = run([owned(true, "microsoft")]);
        expect(cart.domains[0].admin).toBeNull();
        cart = cartReducer(cart, { type: "provider", domain: "acme.com", provider: "google" });
        cart = cartReducer(cart, { type: "default_provider", provider: "microsoft" });
        cart = cartReducer(cart, { type: "default_provider", provider: "google" });
        expect(effectiveProvider(cart, cart.domains[0])).toBe("microsoft");
        expect(JSON.stringify(quoteItems(cart))).not.toMatch(/admin/);
    });

    it("holds an owned domain to its addresses only, since it keeps the site it has", () => {
        const cart = run([owned(false), { type: "forwarding", value: "not a site" }]);
        expect(cartBlocker(cart)).toBeNull();
        expect(cartBlocker(cartReducer(cart, add("getacme.com")))).toMatch(/website forward/);
    });
});

describe("cartEstimate", () => {
    it("adds live domain prices to a month of every mailbox, and waits for every price", () => {
        const cart = run([add("getacme.com"), add("tryacme.com"), { type: "slot_count", domain: "tryacme.com", count: 2 }, { type: "add_owned", owned: { domain: "acme.com", provider: null, hasAdmin: false } }], oneEach());
        expect(cartEstimate(cart, { "getacme.com": { price_micros: 10_000_000, renew_micros: 12_000_000 } }, 3_000_000)).toBeNull();
        const both = { "getacme.com": { price_micros: 10_000_000, renew_micros: 12_000_000 }, "tryacme.com": { price_micros: 20_000_000, renew_micros: 25_000_000 } };
        expect(cartEstimate(cart, both, 3_000_000)).toEqual({ due: 30_000_000 + 4 * 3_000_000, monthly: 12_000_000, yearly: 37_000_000 });
    });
});

describe("persistence", () => {
    // setupTests stubs localStorage with bare mocks; back them with a map here.
    const store = new Map<string, string>();
    beforeEach(() => {
        store.clear();
        vi.mocked(localStorage.getItem).mockImplementation((k) => store.get(k) ?? null);
        vi.mocked(localStorage.setItem).mockImplementation((k, v) => void store.set(k, v));
        vi.mocked(localStorage.removeItem).mockImplementation((k) => void store.delete(k));
    });

    it("round-trips senders, choices and customized domains per workspace, with photo ids and no price", () => {
        let cart = cartReducer(threeSenders(), { type: "sender_photo", id: "s2", photo: photo("p-grace", Date.now()) });
        cart = run(
            [
                { type: "add", domain: "acme.ai", years: 2 },
                add("getacme.com"),
                { type: "local_part", domain: "getacme.com", index: 0, value: "sales" },
                { type: "admin", domain: "acme.ai", index: 1 },
                { type: "patterns", patterns: ["flast", "first"] },
                { type: "signature", choice: "custom" },
                { type: "signature_custom", template: { html: "<b>{{.Sender.Name}}</b>", plain: "{{.Sender.FirstName}}", plainSync: false } },
                { type: "default_provider", provider: "microsoft" },
                { type: "add_owned", owned: { domain: "acme.com", provider: "google", hasAdmin: true } },
                { type: "forwarding", value: "acme.com" },
            ],
            cart,
        );
        saveCart("org-1", cart);
        expect(loadCart("org-1")).toEqual(cart);
        expect(loadCart("org-2")).toBeNull();
        const raw = store.get(cartStorageKey("org-1")) ?? "";
        expect(raw).not.toMatch(/_micros|price|data:|blob:/);
        expect(raw).toMatch(/p-grace/);
    });

    it("stores only a hand-set count, so a cart saved with the old fixed count starts automatic", () => {
        const auto = run([add("getacme.com")]);
        expect(parseStoredCart(serializeCart(auto))?.perSender).toBeNull();
        const set = cartReducer(auto, { type: "per_sender", count: 2 });
        expect(parseStoredCart(serializeCart(set))?.perSender).toBe(2);
        const old = { ...JSON.parse(serializeCart(auto)), perSender: 1 };
        delete old.perSenderSet;
        expect(effectivePerSender(parseStoredCart(JSON.stringify(old)) as Cart)).toBe(3);
    });

    it("drops a photo id the server will have swept", () => {
        const cart = run([{ type: "sender_photo", id: "s1", photo: photo("old", now - 21 * 3600_000) }]);
        expect(parseStoredCart(serializeCart(cart), now)?.senders[0].photo).toBeUndefined();
    });

    it("migrates a v2 cart: its person becomes the sender and its domains keep their mailboxes", () => {
        const v2 = JSON.stringify({
            v: 2,
            person: ada,
            forwarding: "https://acme.com",
            forwardingTouched: true,
            profilePhoto: { id: "p-profile", at: now },
            domains: [
                { domain: "getacme.com", kind: "new", years: 2, provider: "microsoft", providerFixed: false, admin: 1, mailboxes: [{ local_part: "ada", edited: false }, { local_part: "sales", edited: true }, { local_part: "alovelace", edited: false }] },
                { domain: "tryacme.com", kind: "new", years: 1, provider: "google", providerFixed: false, admin: 0, mailboxes: [{ local_part: "ada", edited: false }] },
                { domain: "acme.com", kind: "owned", years: 0, provider: "microsoft", providerFixed: true, admin: null, mailboxes: [{ local_part: "ada", edited: false }] },
            ],
        });
        const cart = parseStoredCart(v2, now) as Cart;
        expect(cart.senders).toEqual([{ id: "s1", first: "Ada", last: "Lovelace", photo: { id: "p-profile", at: now } }]);
        expect(cart.provider).toBe("microsoft");
        expect(cart.domains.map((d) => effectiveProvider(cart, d))).toEqual(["microsoft", "google", "microsoft"]);
        expect(addresses(cart, "getacme.com")).toEqual(["ada", "sales", "ada.l"]);
        expect(adminAddress(cart, cart.domains[0])).toBe("sales@getacme.com");
        // v2's untouched default (three untyped) follows the senders; any other shape keeps its mailboxes.
        expect(cart.domains[1].custom).toEqual([{ sender: "s1", local_part: null }]);
        expect(addresses(cart, "tryacme.com")).toEqual(["ada"]);
        const untouched = parseStoredCart(
            JSON.stringify({ v: 2, person: ada, forwarding: "", domains: [{ domain: "a.com", provider: "google", mailboxes: ["ada", "ada.lovelace", "alovelace"].map((l) => ({ local_part: l, edited: false })) }] }),
            now,
        ) as Cart;
        expect(untouched.domains[0].custom).toBeNull();
        expect(cart.domains[2]).toMatchObject({ kind: "owned", admin: null, providerFixed: true });
        expect(cart.forwarding).toBe("https://acme.com");
    });

    it("migrates a v3 cart's address style to patterns, the old default to automatic", () => {
        const base = JSON.parse(serializeCart(run([add("getacme.com")])));
        delete base.patterns;
        delete base.signature;
        const v3 = (style: string) => parseStoredCart(JSON.stringify({ ...base, v: 3, style })) as Cart;
        expect(v3("first").patterns).toBeNull();
        expect(v3("first.last").patterns).toEqual(["first.last"]);
        expect(v3("flast").patterns).toEqual(["flast"]);
        expect(v3("firstl").patterns).toEqual(["firstl"]);
        expect(v3("first").signature.choice).toBe("default");
        expect(addresses(v3("first"), "getacme.com")).toEqual(["ada", "ada.l", "ada.lovelace"]);
    });

    it("migrates a v4 cart's fixed signature to a template and drops the per-sender details", () => {
        const base = JSON.parse(serializeCart(run([add("getacme.com")])));
        const v4 = (signature: string) =>
            parseStoredCart(JSON.stringify({ ...base, v: 4, signature, senders: [{ id: "s1", first: "Ada", last: "Lovelace", goesBy: "Addy", title: "CTO", phone: "+1 415 555 0100" }] })) as Cart;
        expect(v4("simple").signature).toEqual({ choice: "default", custom: DEFAULT_SIGNATURE });
        expect(v4("none").signature.choice).toBe("none");
        expect(v4("simple").senders).toEqual([{ id: "s1", first: "Ada", last: "Lovelace" }]);
        expect(addresses(v4("simple"), "getacme.com")).toEqual(["ada", "ada.l", "ada.lovelace"]);
        // A stored template survives a reload as typed.
        const custom = run([{ type: "signature", choice: "custom" }, { type: "signature_custom", template: { html: "<i>{{.Sender.Email}}</i>" } }]);
        expect(parseStoredCart(serializeCart(custom))?.signature).toEqual(custom.signature);
    });

    it("reads a v1 cart as new one-year domains", () => {
        const v1 = JSON.stringify({ v: 1, step: "mailboxes", person: ada, forwarding: "", domains: [{ domain: "a.com", provider: "google", mailboxes: [{ local_part: "ada", edited: false }] }] });
        expect(parseStoredCart(v1)?.domains[0]).toMatchObject({ domain: "a.com", kind: "new", years: 1, admin: 0, providerFixed: false });
    });

    it("refuses stored data it does not recognise", () => {
        expect(parseStoredCart("{not json")).toBeNull();
        expect(parseStoredCart(JSON.stringify({ v: 99, domains: [] }))).toBeNull();
        expect(parseStoredCart(JSON.stringify({ v: 2, domains: [{ domain: "a.com", provider: "yahoo" }] }))).toBeNull();
        expect(parseStoredCart(JSON.stringify({ v: 3, senders: [], domains: [] }))).toBeNull();
    });

    it("keeps working when storage throws", () => {
        vi.mocked(localStorage.getItem).mockImplementation(() => {
            throw new Error("blocked");
        });
        vi.mocked(localStorage.setItem).mockImplementation(() => {
            throw new Error("blocked");
        });
        expect(loadCart("org-1")).toBeNull();
        expect(() => saveCart("org-1", run([add("a.com")]))).not.toThrow();
    });

    it("keeps the senders for the next order after one is placed", () => {
        const placed = cartReducer(cartReducer(threeSenders(), add("getacme.com")), { type: "reset" });
        expect(placed.domains).toHaveLength(0);
        expect(placed.senders).toHaveLength(3);
    });
});

describe("default website forward", () => {
    it("fills the suggested forward until the buyer changes it, across a reload", () => {
        const suggested = run([add("getacme.com"), { type: "default_forwarding", value: "https://acme.com" }]);
        expect(suggested.forwarding).toBe("https://acme.com");
        const cleared = cartReducer(suggested, { type: "forwarding", value: "" });
        const back = parseStoredCart(serializeCart(cleared)) as Cart;
        expect(cartReducer(back, { type: "default_forwarding", value: "https://acme.com" }).forwarding).toBe("");
    });
});

describe("offered providers", () => {
    it("moves a saved Microsoft cart onto the provider still offered, keeping domains that already have mailboxes", () => {
        let cart = emptyCart({ first: "Ada", last: "Lovelace" });
        cart = cartReducer(cart, { type: "default_provider", provider: "microsoft" });
        cart = cartReducer(cart, { type: "add", domain: "fresh.com" });
        cart = cartReducer(cart, { type: "add", domain: "picked.com", provider: "google" });
        cart = { ...cart, domains: [...cart.domains, { domain: "owned.com", kind: "owned", years: 1, provider: "microsoft", providerFixed: true, custom: null, admin: 0 }] };

        const next = cartReducer(cart, { type: "offered_providers", providers: ["google"] });

        expect(next.provider).toBe("google");
        expect(next.domains.map((d) => [d.domain, d.provider])).toEqual([["fresh.com", null], ["picked.com", null], ["owned.com", "microsoft"]]);
        expect(cartReducer(next, { type: "offered_providers", providers: ["google"] })).toBe(next);
    });
});
