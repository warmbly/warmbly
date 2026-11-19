// The checkout's order: the senders, set once, and the domains, each with one mailbox per sender unless the buyer
// customized it. Kept in localStorage per workspace so a reload, or a trip to save a card, picks up where it left off.
// It holds no prices (those are always read live) and no images, only uploaded photo ids.
import type { QuoteItem, QuoteMailbox, StoreProvider } from "@/lib/api/models/app/store/Store";
import { nameError } from "@/lib/displayName";
import { checkLocalParts, forwardingProblem, normalizeForwarding, normalizeTyped, type Person } from "./addresses";
import { fillSignature, type SignatureTemplate } from "@/lib/email/signaturePlaceholders";
import { defaultPatterns, isPattern, pickAddress, patternsFromStyle, type AddressPattern } from "./patterns";

export const PROVIDER_NAMES: Record<StoreProvider, string> = { google: "Google Workspace", microsoft: "Microsoft 365" };
export const PROVIDER_SHORT: Record<StoreProvider, string> = { google: "Google", microsoft: "Microsoft" };

/** An uploaded photo, by id; never the image. `at` lets a stale one be dropped before the server sweeps it. */
export interface CartPhoto {
    id: string;
    at: number;
}

/** An upload nobody ordered is removed after a day; the cart lets go of one a little before that. */
export const PHOTO_KEEP_MS = 20 * 60 * 60 * 1000;

export const MAX_SENDERS = 5;

/** A person the mailboxes send as: the name on every mailbox of theirs, and one photo for all of them. */
export interface Sender {
    id: string;
    first: string;
    last: string;
    /** undefined until chosen (the first sender then takes the buyer's profile photo); null is none. */
    photo?: CartPhoto | null;
}

/** One mailbox on a customized domain: whose it is, and its address, or null to follow the name and the style. */
export interface CartSlot {
    sender: string;
    local_part: string | null;
}

/** "new" is registered with the order; "owned" is already in the workspace's store, so only mailboxes are bought. */
export type CartDomainKind = "new" | "owned";

export interface CartDomain {
    domain: string;
    kind: CartDomainKind;
    /** Registration term for a new domain, from the search result; 0 for an owned one. */
    years: number;
    /** This domain's own provider, or null to follow the order's. */
    provider: StoreProvider | null;
    /** Set when the domain already has mailboxes, which fixes its provider and its admin. */
    providerFixed: boolean;
    /** null: one mailbox per sender, following every change to the senders. */
    custom: CartSlot[] | null;
    /** Index of the Google Workspace / Microsoft 365 admin mailbox, or null when the domain already has one. */
    admin: number | null;
}

/** What the cart is told about a domain the workspace already has. */
export interface OwnedDomain {
    domain: string;
    /** The provider its existing mailboxes use, or null when it has none yet. */
    provider: StoreProvider | null;
    /** True when it already has mailboxes, and so an admin. */
    hasAdmin: boolean;
}

/** Default signs with the sender's name, None with nothing, Custom with the buyer's template. */
export type SignatureChoice = "default" | "none" | "custom";

export interface CartSignature {
    choice: SignatureChoice;
    /** The buyer's template, kept while another choice is picked; it starts as the default. */
    custom: SignatureTemplate;
}

/** The default signature: the sender's full name, the only line a signature placeholder can fill for every mailbox. */
export const DEFAULT_SIGNATURE: SignatureTemplate = { html: "{{.Sender.Name}}", plain: "{{.Sender.Name}}", plainSync: true };

export const SIGNATURE_CHOICE_NAMES: Record<SignatureChoice, string> = { default: "Default", none: "None", custom: "Custom" };

/** The template every mailbox's signature is filled from, or null for none. */
export function signatureTemplate(sig: CartSignature): SignatureTemplate | null {
    if (sig.choice === "none") return null;
    return sig.choice === "custom" ? sig.custom : DEFAULT_SIGNATURE;
}

/** One mailbox's finished signature, filled in like a bulk signature; empty for none. */
export function mailboxSignature(sig: CartSignature, sender: Pick<Sender, "first" | "last">, address: string): { html: string; plain: string } {
    const t = signatureTemplate(sig);
    if (!t) return { html: "", plain: "" };
    return fillSignature(t, { name: `${sender.first.trim()} ${sender.last.trim()}`, email: address });
}

export interface Cart {
    senders: Sender[];
    /** The address patterns each sender's mailboxes take in order, or null to follow the number of senders. */
    patterns: AddressPattern[] | null;
    /** The signature every mailbox gets, filled in per mailbox. */
    signature: CartSignature;
    /** Mailboxes per sender the buyer set by hand, or null to follow the number of senders (see autoPerSender). */
    perSender: number | null;
    /** The provider every domain uses unless it picked its own. */
    provider: StoreProvider;
    domains: CartDomain[];
    forwarding: string;
    /** True once the buyer typed or cleared the forward, so the suggested one no longer replaces it. */
    forwardingTouched?: boolean;
}

export interface CartLimits {
    max: number;
    recommended: number;
}

export const DEFAULT_LIMITS: CartLimits = { max: 5, recommended: 3 };

export function emptyCart(person: Person = { first: "", last: "" }): Cart {
    return {
        senders: [{ id: "s1", first: person.first, last: person.last }],
        patterns: null,
        signature: { choice: "default", custom: { ...DEFAULT_SIGNATURE } },
        perSender: null,
        provider: "google",
        domains: [],
        forwarding: "",
        forwardingTouched: false,
    };
}

// ---- addresses ----

/** The patterns the cart's addresses follow: the buyer's, or the default for the number of senders. */
export function effectivePatterns(cart: Pick<Cart, "senders" | "patterns">): AddressPattern[] {
    return cart.patterns ?? defaultPatterns(cart.senders.length);
}

/** Mailboxes per sender while the buyer has not set it: three variants of one person, two each for two, one each beyond. */
export function autoPerSender(senders: number, max = DEFAULT_LIMITS.max): number {
    const n = senders <= 1 ? 3 : senders === 2 ? 2 : 1;
    return Math.max(1, Math.min(n, Math.floor(max / Math.max(1, senders))));
}

/** The most mailboxes each sender can have before a domain passes the per-domain limit. */
export function perSenderMax(cart: Pick<Cart, "senders">, limits: CartLimits = DEFAULT_LIMITS): number {
    return Math.max(1, Math.floor(limits.max / Math.max(1, cart.senders.length)));
}

/** Mailboxes each sender gets on a following domain: the buyer's number, lowered to fit the domain, or the automatic one. */
export function effectivePerSender(cart: Pick<Cart, "senders" | "perSender">, limits: CartLimits = DEFAULT_LIMITS): number {
    if (cart.perSender === null) return autoPerSender(cart.senders.length, limits.max);
    return Math.min(cart.perSender, perSenderMax(cart, limits));
}

/** The buyer's number when it had to be lowered to fit the domain, or null. */
export function perSenderClamped(cart: Pick<Cart, "senders" | "perSender">, limits: CartLimits = DEFAULT_LIMITS): number | null {
    return cart.perSender !== null && cart.perSender > perSenderMax(cart, limits) ? cart.perSender : null;
}

/** A domain's mailboxes before addresses: the custom list, or the per-sender count for each sender in order. */
function slotsOf(cart: Cart, d: CartDomain): CartSlot[] {
    if (d.custom) return d.custom;
    const n = effectivePerSender(cart);
    return cart.senders.flatMap((s) => Array.from({ length: n }, () => ({ sender: s.id, local_part: null })));
}

export interface CartMailbox {
    local_part: string;
    sender: Sender;
    /** Typed by the buyer, so a name or pattern change leaves it alone. */
    typed: boolean;
    /** Why it is not the sender's own next pattern: another sender took that address, or every pattern was taken. */
    moved?: "pattern" | "numbered";
}

/** Every mailbox a domain gets. Typed addresses are kept as typed; the rest follow the patterns and step aside for them. */
export function domainMailboxes(cart: Cart, d: CartDomain): CartMailbox[] {
    const slots = slotsOf(cart, d);
    const patterns = effectivePatterns(cart);
    const byId = new Map(cart.senders.map((s) => [s.id, s]));
    const typed = slots.flatMap((s) => (s.local_part !== null ? [s.local_part.trim()] : []));
    const used = new Set(typed);
    // What each sender would get with nobody else on the domain, so a collision between senders can be told apart.
    const own = new Map<string, Set<string>>();
    return slots.map((slot) => {
        const sender = byId.get(slot.sender) ?? cart.senders[0];
        if (slot.local_part !== null) return { local_part: slot.local_part, sender, typed: true };
        const mine = own.get(sender.id) ?? new Set(typed);
        own.set(sender.id, mine);
        const alone = pickAddress(sender, patterns, mine).local_part;
        const { local_part, numbered } = pickAddress(sender, patterns, used);
        used.add(local_part);
        mine.add(local_part);
        const moved = numbered ? "numbered" : local_part !== alone ? "pattern" : undefined;
        return { local_part, sender, typed: false, ...(moved ? { moved } : {}) };
    });
}

/** One line explaining an address that moved because two senders share a name, or null. */
export function collisionNote(boxes: readonly CartMailbox[], domain: string): string | null {
    const numbered = boxes.find((b) => b.moved === "numbered");
    if (numbered) return `Two senders share a name, so ${numbered.local_part}@${domain} ends in a number. Customize the domain to type another address.`;
    const moved = boxes.find((b) => b.moved === "pattern");
    if (moved) return `Two senders would share an address, so ${moved.sender.first.trim() || "one"} gets the next pattern: ${moved.local_part}@.`;
    return null;
}

export function effectiveProvider(cart: Cart, d: CartDomain): StoreProvider {
    return d.provider ?? cart.provider;
}

/** The admin mailbox's index on a domain, or null when it already has one. */
export function adminIndex(cart: Cart, d: CartDomain): number | null {
    if (d.admin === null) return null;
    return d.admin < slotsOf(cart, d).length ? d.admin : 0;
}

// ---- actions ----

export type CartAction =
    | { type: "add"; domain: string; years?: number; provider?: StoreProvider }
    | { type: "add_owned"; owned: OwnedDomain }
    | { type: "remove"; domain: string }
    | { type: "admin"; domain: string; index: number }
    | { type: "provider"; domain: string; provider: StoreProvider }
    | { type: "default_provider"; provider: StoreProvider }
    | { type: "offered_providers"; providers: readonly StoreProvider[] }
    | { type: "sender_add"; limits?: CartLimits }
    | { type: "sender_remove"; id: string }
    | { type: "sender_name"; id: string; first?: string; last?: string }
    | { type: "sender_photo"; id: string; photo: CartPhoto | null }
    | { type: "seed_sender"; person: Person }
    | { type: "seed_photo"; photo: CartPhoto | null }
    | { type: "patterns"; patterns: AddressPattern[] }
    | { type: "patterns_auto" }
    | { type: "signature"; choice: SignatureChoice }
    | { type: "signature_custom"; template: Partial<SignatureTemplate> }
    | { type: "per_sender"; count: number; limits?: CartLimits }
    | { type: "per_sender_auto" }
    | { type: "uncustomize"; domain: string }
    | { type: "slot_count"; domain: string; count: number; limits?: CartLimits }
    | { type: "slot_sender"; domain: string; index: number; sender: string }
    | { type: "local_part"; domain: string; index: number; value: string }
    | { type: "auto_address"; domain: string; index: number }
    | { type: "forwarding"; value: string }
    | { type: "default_forwarding"; value: string }
    | { type: "reset" };

// A registration term is 1..10 years; anything unreadable is the usual one.
function clampYears(n: number | undefined): number {
    if (n === undefined || !Number.isFinite(n)) return 1;
    return Math.max(1, Math.min(10, Math.round(n)));
}

/** Whether another sender can be added; the per-sender count shrinks to make room, so only the sender cap stops it. */
export function canAddSender(cart: Pick<Cart, "senders">): boolean {
    return cart.senders.length < MAX_SENDERS;
}

/** After the senders or the count changed, each following domain's admin stays with the same sender's same mailbox. */
function keepAdmins(before: Cart, after: Cart): Cart {
    const domains = after.domains.map((d, i) => {
        const prev = before.domains[i];
        if (d.custom || d.admin === null || !prev || prev.domain !== d.domain) return d;
        const old = slotsOf(before, prev);
        const was = old[Math.min(d.admin, old.length - 1)];
        if (!was) return d;
        const nth = old.slice(0, d.admin).filter((s) => s.sender === was.sender).length;
        // Their nth mailbox, or their last when they now have fewer; the first mailbox when they are gone.
        const theirs = slotsOf(after, d).flatMap((s, k) => (s.sender === was.sender ? [k] : []));
        const admin = theirs[Math.min(nth, theirs.length - 1)] ?? 0;
        return admin === d.admin ? d : { ...d, admin };
    });
    return { ...after, domains };
}

function mapDomain(cart: Cart, domain: string, fn: (d: CartDomain) => CartDomain): Cart {
    let changed = false;
    const domains = cart.domains.map((d) => {
        if (d.domain !== domain) return d;
        const next = fn(d);
        if (next !== d) changed = true;
        return next;
    });
    return changed ? { ...cart, domains } : cart;
}

/** A customized copy of a domain's mailboxes, so an edit to one domain never reaches the others. */
function customized(cart: Cart, d: CartDomain): CartSlot[] {
    return d.custom ? d.custom.map((s) => ({ ...s })) : slotsOf(cart, d);
}

function mapSender(cart: Cart, id: string, fn: (s: Sender) => Sender): Cart {
    let changed = false;
    const senders = cart.senders.map((s) => {
        if (s.id !== id) return s;
        const next = fn(s);
        if (next !== s) changed = true;
        return next;
    });
    return changed ? { ...cart, senders } : cart;
}

function nextSenderId(cart: Cart): string {
    const n = cart.senders.reduce((m, s) => Math.max(m, Number(s.id.replace(/^s/, "")) || 0), 0);
    return `s${n + 1}`;
}

export function cartReducer(cart: Cart, action: CartAction): Cart {
    switch (action.type) {
        case "add": {
            if (cart.domains.some((d) => d.domain === action.domain)) return cart;
            const provider = action.provider && action.provider !== cart.provider ? action.provider : null;
            const entry: CartDomain = { domain: action.domain, kind: "new", years: clampYears(action.years), provider, providerFixed: false, custom: null, admin: 0 };
            return { ...cart, domains: [...cart.domains, entry] };
        }
        case "add_owned": {
            const { owned } = action;
            if (cart.domains.some((d) => d.domain === owned.domain)) return cart;
            const fixed = owned.provider !== null && owned.hasAdmin;
            const entry: CartDomain = {
                domain: owned.domain,
                kind: "owned",
                years: 0,
                provider: fixed || (owned.provider && owned.provider !== cart.provider) ? owned.provider : null,
                providerFixed: fixed,
                custom: null,
                admin: owned.hasAdmin ? null : 0,
            };
            return { ...cart, domains: [...cart.domains, entry] };
        }
        case "remove": {
            const domains = cart.domains.filter((d) => d.domain !== action.domain);
            return domains.length === cart.domains.length ? cart : { ...cart, domains };
        }
        case "admin":
            return mapDomain(cart, action.domain, (d) => {
                if (d.admin === null || d.admin === action.index || action.index < 0 || action.index >= slotsOf(cart, d).length) return d;
                return { ...d, admin: action.index };
            });
        case "provider":
            return mapDomain(cart, action.domain, (d) => {
                if (d.providerFixed) return d;
                const provider = action.provider === cart.provider ? null : action.provider;
                return d.provider === provider ? d : { ...d, provider };
            });
        case "default_provider":
            if (cart.provider === action.provider) return cart;
            // A domain that had picked the new default simply follows it from now on.
            return { ...cart, provider: action.provider, domains: cart.domains.map((d) => (!d.providerFixed && d.provider === action.provider ? { ...d, provider: null } : d)) };
        case "offered_providers": {
            // A provider no longer offered falls back to the first one that is; a domain with mailboxes keeps its own.
            const first = action.providers[0];
            if (!first) return cart;
            const provider = action.providers.includes(cart.provider) ? cart.provider : first;
            const domains = cart.domains.map((d) => (!d.providerFixed && d.provider && (!action.providers.includes(d.provider) || d.provider === provider) ? { ...d, provider: null } : d));
            if (provider === cart.provider && domains.every((d, i) => d === cart.domains[i])) return cart;
            return { ...cart, provider, domains };
        }
        case "sender_add": {
            if (!canAddSender(cart)) return cart;
            return keepAdmins(cart, { ...cart, senders: [...cart.senders, { id: nextSenderId(cart), first: "", last: "", photo: null }] });
        }
        case "sender_remove": {
            if (!cart.senders.some((s) => s.id === action.id) || cart.senders.length === 1) return cart;
            const senders = cart.senders.filter((s) => s.id !== action.id);
            // A customized domain keeps its count: the removed sender's mailboxes go to the first one left.
            const domains = cart.domains.map((d): CartDomain => (d.custom ? { ...d, custom: d.custom.map((s) => (s.sender === action.id ? { ...s, sender: senders[0].id } : s)) } : d));
            return keepAdmins(cart, { ...cart, senders, domains });
        }
        case "sender_name":
            return mapSender(cart, action.id, (s) => {
                const first = action.first ?? s.first;
                const last = action.last ?? s.last;
                return first === s.first && last === s.last ? s : { ...s, first, last };
            });
        case "sender_photo":
            return mapSender(cart, action.id, (s) => (s.photo === action.photo ? s : { ...s, photo: action.photo }));
        case "seed_sender": {
            const s = cart.senders[0];
            if (s.first || s.last || (!action.person.first && !action.person.last)) return cart;
            return mapSender(cart, s.id, () => ({ ...s, first: action.person.first, last: action.person.last }));
        }
        case "seed_photo": {
            const s = cart.senders[0];
            return s.photo !== undefined ? cart : mapSender(cart, s.id, () => ({ ...s, photo: action.photo }));
        }
        case "patterns": {
            const patterns = action.patterns.filter((p, i) => isPattern(p) && action.patterns.indexOf(p) === i);
            if (patterns.length === 0) return cart;
            const same = cart.patterns !== null && cart.patterns.length === patterns.length && cart.patterns.every((p, i) => p === patterns[i]);
            return same ? cart : { ...cart, patterns };
        }
        case "patterns_auto":
            return cart.patterns === null ? cart : { ...cart, patterns: null };
        case "signature":
            return cart.signature.choice === action.choice ? cart : { ...cart, signature: { ...cart.signature, choice: action.choice } };
        case "signature_custom": {
            const custom = { ...cart.signature.custom, ...action.template };
            const same = (Object.keys(custom) as (keyof SignatureTemplate)[]).every((k) => custom[k] === cart.signature.custom[k]);
            return same ? cart : { ...cart, signature: { ...cart.signature, custom } };
        }
        case "per_sender": {
            const n = Math.max(1, Math.min(perSenderMax(cart, action.limits), Math.round(action.count) || 1));
            return n === cart.perSender ? cart : keepAdmins(cart, { ...cart, perSender: n });
        }
        case "per_sender_auto":
            return cart.perSender === null ? cart : keepAdmins(cart, { ...cart, perSender: null });
        case "uncustomize":
            return mapDomain(cart, action.domain, (d) => {
                if (!d.custom) return d;
                const next = { ...d, custom: null };
                return { ...next, admin: d.admin === null ? null : d.admin < slotsOf(cart, next).length ? d.admin : 0 };
            });
        case "slot_count":
            return mapDomain(cart, action.domain, (d) => {
                const limits = action.limits ?? DEFAULT_LIMITS;
                const slots = customized(cart, d);
                const n = Math.max(1, Math.min(limits.max, Math.round(action.count) || 1));
                if (n === slots.length && d.custom) return d;
                // New mailboxes take turns among the senders.
                while (slots.length < n) slots.push({ sender: cart.senders[slots.length % cart.senders.length].id, local_part: null });
                const custom = slots.slice(0, n);
                return { ...d, custom, admin: d.admin !== null && d.admin >= n ? 0 : d.admin };
            });
        case "slot_sender":
            return mapDomain(cart, action.domain, (d) => {
                const slots = customized(cart, d);
                if (!slots[action.index] || !cart.senders.some((s) => s.id === action.sender)) return d;
                if (d.custom && slots[action.index].sender === action.sender) return d;
                slots[action.index] = { ...slots[action.index], sender: action.sender };
                return { ...d, custom: slots };
            });
        case "local_part":
            return mapDomain(cart, action.domain, (d) => {
                const slots = customized(cart, d);
                if (!slots[action.index]) return d;
                slots[action.index] = { ...slots[action.index], local_part: normalizeTyped(action.value) };
                return { ...d, custom: slots };
            });
        case "auto_address":
            return mapDomain(cart, action.domain, (d) => {
                if (!d.custom?.[action.index] || d.custom[action.index].local_part === null) return d;
                return { ...d, custom: d.custom.map((s, i) => (i === action.index ? { ...s, local_part: null } : s)) };
            });
        case "forwarding":
            return cart.forwarding === action.value && cart.forwardingTouched ? cart : { ...cart, forwarding: action.value, forwardingTouched: true };
        case "default_forwarding":
            return cart.forwardingTouched || cart.forwarding === action.value ? cart : { ...cart, forwarding: action.value };
        case "reset":
            // The senders and choices stay for the next order; the domains and the forward go.
            return { ...cart, domains: [], forwarding: "", forwardingTouched: false };
    }
}

// ---- what stops paying ----

export function mailboxCount(cart: Cart): number {
    return cart.domains.reduce((n, d) => n + slotsOf(cart, d).length, 0);
}

export interface SenderProblems {
    first: string | null;
    last: string | null;
}

/** Each name a mail provider needs, checked the way the server checks them. */
export function senderProblems(s: Sender): SenderProblems {
    return {
        first: nameError("First name", s.first, "person"),
        last: nameError("Last name", s.last, "person"),
    };
}

/** Why the senders cannot be ordered with, or null. */
export function sendersIssue(cart: Cart): string | null {
    const bad = cart.senders.filter((s) => {
        const p = senderProblems(s);
        return p.first || p.last;
    }).length;
    if (bad === 0) return null;
    if (cart.senders.length === 1) {
        const p = senderProblems(cart.senders[0]);
        return p.first ?? p.last;
    }
    return bad === 1 ? "One sender needs a first and last name." : `${bad} senders need a first and last name.`;
}

/** One verdict per mailbox on a domain, null where its address is fine. */
export function domainProblems(cart: Cart, d: CartDomain) {
    return checkLocalParts(domainMailboxes(cart, d).map((m) => m.local_part));
}

/** What is wrong with a domain's addresses, or null. */
export function domainIssue(cart: Cart, d: CartDomain): string | null {
    const bad = domainProblems(cart, d).filter((p) => p !== null).length;
    if (bad > 0) return bad === 1 ? "One address needs a fix." : `${bad} addresses need a fix.`;
    return null;
}

/** The first domain whose addresses need a fix, or null. */
export function addressIssue(cart: Cart): string | null {
    const d = cart.domains.find((x) => domainIssue(cart, x));
    return d ? `Fix the highlighted address on ${d.domain}.` : null;
}

export function forwardIssue(cart: Cart): string | null {
    return registersDomain(cart) && forwardingProblem(cart.forwarding) ? "Fix the website forward, or leave it empty." : null;
}

/** Why the order cannot be paid yet, or null when it can. */
export function cartBlocker(cart: Cart): string | null {
    if (cart.domains.length === 0) return "Add at least one domain to continue.";
    return sendersIssue(cart) ?? addressIssue(cart) ?? forwardIssue(cart);
}

// ---- what is sent ----

/** What POST /store/quotes is asked for. A photo every mailbox on a domain shares is named once, for the domain.
 * Each mailbox carries its finished signature, empty for none. */
export function quoteItems(cart: Cart): QuoteItem[] {
    return cart.domains.flatMap((d): QuoteItem[] => {
        const boxes = domainMailboxes(cart, d);
        const admin = adminIndex(cart, d);
        const photos = boxes.map((b) => b.sender.photo?.id ?? null);
        const shared = photos[0] && photos.every((p) => p === photos[0]) ? photos[0] : null;
        const mailboxes = boxes.map(
            (b, i): QuoteMailbox => ({
                local_part: b.local_part.trim(),
                first_name: b.sender.first.trim(),
                last_name: b.sender.last.trim(),
                // The admin flag is sent only for a domain that has no admin yet; the server refuses it otherwise.
                ...(admin === i ? { admin: true } : {}),
                ...(!shared && photos[i] ? { photo_id: photos[i] as string } : {}),
                signature: mailboxSignature(cart.signature, b.sender, `${b.local_part.trim()}@${d.domain}`),
            }),
        );
        const item: QuoteItem = { type: "mailboxes", domain: d.domain, provider: effectiveProvider(cart, d), ...(shared ? { photo_id: shared } : {}), mailboxes };
        return d.kind === "owned" ? [item] : [{ type: "domain", domain: d.domain, years: d.years }, item];
    });
}

/** The address that becomes a domain's admin, before the quote names it. */
export function adminAddress(cart: Cart, d: CartDomain): string | null {
    const i = adminIndex(cart, d);
    if (i === null) return null;
    const m = domainMailboxes(cart, d)[i];
    return m ? `${m.local_part.trim()}@${d.domain}` : null;
}

/** True when the cart registers at least one domain, which is what needs a registrant. */
export function registersDomain(cart: Cart): boolean {
    return cart.domains.some((d) => d.kind === "new");
}

/** The website forward, which only a newly registered domain takes: an owned domain keeps the site it has. */
export function orderForwarding(cart: Cart): string | undefined {
    if (!registersDomain(cart)) return undefined;
    return normalizeForwarding(cart.forwarding) || undefined;
}

const SAMPLE_DOMAIN: CartDomain = { domain: "example.com", kind: "new", years: 1, provider: null, providerFixed: false, custom: null, admin: null };

/** The mailbox a signature preview is filled for: the order's first, or the first sender's at example.com before a domain is added. */
export function previewMailbox(cart: Cart): { sender: Sender; address: string } {
    const d = cart.domains[0] ?? SAMPLE_DOMAIN;
    const m = domainMailboxes(cart, d)[0];
    return m ? { sender: m.sender, address: `${m.local_part.trim()}@${d.domain}` } : { sender: cart.senders[0], address: `mail@${d.domain}` };
}

/** The mailboxes a domain following the senders gets, as one line: how many, their addresses, the provider and the signature. */
export function sendersSummary(cart: Cart, limits: CartLimits = DEFAULT_LIMITS): string {
    const each = effectivePerSender(cart, limits);
    const n = cart.senders.length * each;
    const custom = cart.senders.flatMap((x) => Array.from({ length: each }, () => ({ sender: x.id, local_part: null })));
    const parts = domainMailboxes(cart, { ...SAMPLE_DOMAIN, custom }).map((m) => `${m.local_part}@`);
    const shown = parts.length > 3 ? `${parts.slice(0, 3).join(", ")} +${parts.length - 3}` : parts.join(", ");
    return [`${n} ${n === 1 ? "mailbox" : "mailboxes"} per domain`, shown, PROVIDER_SHORT[cart.provider], `Signature: ${SIGNATURE_CHOICE_NAMES[cart.signature.choice]}`].join(" · ");
}

// ---- persistence ----

const STORAGE_PREFIX = "warmbly.store.cart.";
// v5 keeps the signature as a template; v4 a fixed one with per-sender details; v3 one address style; v1 and v2 one person.
const VERSION = 5;

export function cartStorageKey(orgId: string): string {
    return `${STORAGE_PREFIX}${orgId || "none"}`;
}

const isStr = (v: unknown): v is string => typeof v === "string";
const isProvider = (v: unknown): v is StoreProvider => v === "google" || v === "microsoft";
const isObj = (v: unknown): v is Record<string, unknown> => !!v && typeof v === "object";

/** A stored photo still fresh enough to order with; undefined drops it, null keeps "no photo". */
function parsePhoto(v: unknown, now: number): CartPhoto | null | undefined {
    if (v === null) return null;
    if (!isObj(v)) return undefined;
    if (!isStr(v.id) || typeof v.at !== "number" || now - v.at > PHOTO_KEEP_MS) return undefined;
    return { id: v.id, at: v.at };
}

function parsePatterns(v: unknown): AddressPattern[] | null {
    if (!Array.isArray(v)) return null;
    const out = v.filter((p, i): p is AddressPattern => isPattern(p) && v.indexOf(p) === i);
    return out.length > 0 ? out : null;
}

function parseAdmin(v: unknown, kind: CartDomainKind, size: number): number | null {
    if (v === null && kind === "owned") return null;
    return typeof v === "number" && v >= 0 && v < size ? Math.floor(v) : 0;
}

function parseSenders(v: unknown, now: number): Sender[] | null {
    if (!Array.isArray(v) || v.length === 0 || v.length > MAX_SENDERS) return null;
    const out: Sender[] = [];
    for (const s of v) {
        if (!isObj(s) || !isStr(s.id) || out.some((x) => x.id === s.id)) return null;
        const photo = parsePhoto(s.photo, now);
        out.push({
            id: s.id,
            first: isStr(s.first) ? s.first : "",
            last: isStr(s.last) ? s.last : "",
            ...(photo !== undefined ? { photo } : {}),
        });
    }
    return out;
}

/** A stored signature; v4's "none" stays none and its fixed "simple" becomes the default. */
function parseSignature(v: unknown): CartSignature {
    if (v === "none") return { choice: "none", custom: { ...DEFAULT_SIGNATURE } };
    if (!isObj(v)) return { choice: "default", custom: { ...DEFAULT_SIGNATURE } };
    const c = isObj(v.custom) ? v.custom : {};
    const custom: SignatureTemplate = {
        html: isStr(c.html) ? c.html : DEFAULT_SIGNATURE.html,
        plain: isStr(c.plain) ? c.plain : DEFAULT_SIGNATURE.plain,
        plainSync: typeof c.plainSync === "boolean" ? c.plainSync : true,
    };
    const choice: SignatureChoice = v.choice === "none" || v.choice === "custom" ? v.choice : "default";
    return { choice, custom };
}

function parseDomainV3(v: unknown, senders: Sender[], perDomain: number): CartDomain | null {
    if (!isObj(v) || !isStr(v.domain)) return null;
    const kind: CartDomainKind = v.kind === "owned" ? "owned" : "new";
    const providerFixed = kind === "owned" && v.providerFixed === true && isProvider(v.provider);
    let custom: CartSlot[] | null = null;
    if (Array.isArray(v.custom) && v.custom.length > 0) {
        custom = [];
        for (const s of v.custom) {
            if (!isObj(s)) return null;
            const sender = isStr(s.sender) && senders.some((x) => x.id === s.sender) ? s.sender : senders[0].id;
            custom.push({ sender, local_part: isStr(s.local_part) ? s.local_part : null });
        }
    }
    return {
        domain: v.domain,
        kind,
        years: kind === "owned" ? 0 : clampYears(typeof v.years === "number" ? v.years : undefined),
        provider: isProvider(v.provider) ? v.provider : null,
        providerFixed,
        custom,
        admin: parseAdmin(v.admin, kind, custom?.length ?? perDomain),
    };
}

/** A v1 or v2 cart: one person's name, and each domain's own provider, mailboxes and photo. */
function migrateV2(o: Record<string, unknown>, now: number): Cart | null {
    if (!Array.isArray(o.domains)) return null;
    const p = isObj(o.person) ? o.person : {};
    const raws = o.domains as unknown[];
    const first = raws.find(isObj);
    const provider = first && isProvider(first.provider) ? first.provider : "google";
    // The person's photo was each domain's default; the first domain's own one wins when it had one.
    const domainPhoto = first ? parsePhoto(first.photo, now) : undefined;
    const photo = domainPhoto !== undefined ? domainPhoto : parsePhoto(o.profilePhoto, now);
    const sender: Sender = { id: "s1", first: isStr(p.first) ? p.first : "", last: isStr(p.last) ? p.last : "", ...(photo !== undefined ? { photo } : {}) };
    const domains: CartDomain[] = [];
    for (const d of raws) {
        if (!isObj(d) || !isStr(d.domain) || !isProvider(d.provider) || !Array.isArray(d.mailboxes) || d.mailboxes.length === 0) return null;
        const slots: CartSlot[] = [];
        for (const m of d.mailboxes) {
            if (!isObj(m) || !isStr(m.local_part)) return null;
            slots.push({ sender: sender.id, local_part: m.edited === true ? m.local_part : null });
        }
        const kind: CartDomainKind = d.kind === "owned" ? "owned" : "new";
        const fixed = kind === "owned" && d.providerFixed === true;
        // Untyped addresses at the automatic count for one sender are exactly what following the senders gives.
        const custom = slots.length === autoPerSender(1) && slots.every((x) => x.local_part === null) ? null : slots;
        domains.push({
            domain: d.domain,
            kind,
            years: kind === "owned" ? 0 : clampYears(typeof d.years === "number" ? d.years : undefined),
            provider: fixed || d.provider !== provider ? d.provider : null,
            providerFixed: fixed,
            custom,
            admin: parseAdmin(d.admin, kind, slots.length),
        });
    }
    return {
        senders: [sender],
        patterns: null,
        signature: parseSignature(undefined),
        perSender: null,
        provider,
        domains,
        forwarding: isStr(o.forwarding) ? o.forwarding : "",
        // A cart saved before the suggestion existed holds only forwards the buyer typed.
        forwardingTouched: o.forwardingTouched === true || (o.forwardingTouched === undefined && isStr(o.forwarding) && o.forwarding !== ""),
    };
}

/** The cart stored for a workspace, or null when there is none or it cannot be read. */
export function parseStoredCart(raw: string | null, now = Date.now()): Cart | null {
    if (!raw) return null;
    let data: unknown;
    try {
        data = JSON.parse(raw);
    } catch {
        return null;
    }
    if (!isObj(data)) return null;
    if (data.v === 1 || data.v === 2) return migrateV2(data, now);
    if ((data.v !== VERSION && data.v !== 4 && data.v !== 3) || !Array.isArray(data.domains)) return null;
    const senders = parseSenders(data.senders, now);
    if (!senders) return null;
    // Only a count the buyer set is stored (perSenderSet); an earlier fixed `perSender` reads as automatic.
    const perSender = typeof data.perSenderSet === "number" ? Math.max(1, Math.min(DEFAULT_LIMITS.max, Math.floor(data.perSenderSet))) : null;
    const domains = data.domains.map((d) => parseDomainV3(d, senders, senders.length * effectivePerSender({ senders, perSender })));
    if (domains.some((d) => d === null)) return null;
    return {
        senders,
        // A v3 cart's one style becomes its patterns; the old default reads as automatic.
        patterns: data.v === 3 ? patternsFromStyle(data.style) : parsePatterns(data.patterns),
        signature: parseSignature(data.signature),
        perSender,
        provider: isProvider(data.provider) ? data.provider : "google",
        domains: domains as CartDomain[],
        forwarding: isStr(data.forwarding) ? data.forwarding : "",
        forwardingTouched: data.forwardingTouched === true,
    };
}

/** Senders, choices, domains, addresses, the forward and photo ids; never a price and never an image. */
export function serializeCart(cart: Cart): string {
    return JSON.stringify({
        v: VERSION,
        senders: cart.senders.map((s) => ({ id: s.id, first: s.first, last: s.last, photo: s.photo })),
        patterns: cart.patterns,
        signature: cart.signature,
        perSenderSet: cart.perSender,
        provider: cart.provider,
        forwarding: cart.forwarding,
        forwardingTouched: cart.forwardingTouched === true,
        domains: cart.domains.map((d) => ({
            domain: d.domain,
            kind: d.kind,
            years: d.years,
            provider: d.provider,
            providerFixed: d.providerFixed,
            admin: d.admin,
            custom: d.custom,
        })),
    });
}

/** Live prices for the domains in a cart, from search results and quotes. */
export type PriceBook = Record<string, { price_micros: number; renew_micros: number }>;

/** Today's estimate from live prices, or null while any domain in the cart has none yet. */
export function cartEstimate(cart: Cart, prices: PriceBook, mailboxMonthly: number): { due: number; monthly: number; yearly: number } | null {
    let due = 0;
    let yearly = 0;
    for (const d of cart.domains) {
        // A domain the workspace already has costs nothing here.
        if (d.kind === "owned") continue;
        const p = prices[d.domain];
        if (!p) return null;
        due += p.price_micros;
        yearly += p.renew_micros;
    }
    const monthly = mailboxCount(cart) * mailboxMonthly;
    return { due: due + monthly, monthly, yearly };
}

export function loadCart(orgId: string): Cart | null {
    try {
        return parseStoredCart(window.localStorage.getItem(cartStorageKey(orgId)));
    } catch {
        return null;
    }
}

export function saveCart(orgId: string, cart: Cart): void {
    try {
        window.localStorage.setItem(cartStorageKey(orgId), serializeCart(cart));
    } catch {
        // Storage blocked or full: the cart lives for this page only.
    }
}
