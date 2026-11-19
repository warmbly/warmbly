// Mailbox addresses for a purchased domain: suggested from the buyer's name, checked as they are typed.

export interface Person {
    first: string;
    last: string;
}

/** Used when the account has no name to build addresses from. */
const NAMELESS = ["hello", "team", "contact", "sales", "outreach", "growth", "partners"];

// Role addresses providers keep for themselves or that recipients filter as bulk.
const RESERVED = new Set(["postmaster", "abuse", "hostmaster", "webmaster", "mailer-daemon", "noreply", "no-reply", "root"]);

export const LOCAL_PART_MAX = 64;

// Letters NFKD leaves whole.
const FOLD: Record<string, string> = { ß: "ss", æ: "ae", œ: "oe", ø: "o", ł: "l", đ: "d", ð: "d", þ: "th", ı: "i" };

/** "Müller" -> "muller", "Łukasz" -> "lukasz": lowercase, accents and ligatures folded to ASCII. Other characters stay. */
export function toAscii(text: string): string {
    return text
        .toLowerCase()
        .replace(/[ßæœøłđðþı]/g, (c) => FOLD[c])
        .normalize("NFKD")
        .replace(/\p{M}/gu, "");
}

/** "Zoë O'Brien" -> "zoeobrien": lowercase ASCII letters and digits only. */
export function slug(text: string): string {
    return toAscii(text).replace(/[^a-z0-9]/g, "");
}

/** Every address worth offering for a person, most natural first, without repeats. */
export function addressCandidates(person: Person): string[] {
    const f = slug(person.first);
    const l = slug(person.last);
    const out: string[] = [];
    const push = (s: string) => {
        if (s && s.length <= LOCAL_PART_MAX && !out.includes(s) && !RESERVED.has(s)) out.push(s);
    };
    if (f && l) {
        push(f);
        push(`${f}.${l}`);
        push(`${f[0]}${l}`);
        push(`${f}${l[0]}`);
        push(`${f}_${l}`);
        push(`${f[0]}.${l}`);
        push(`${f}${l}`);
        push(l);
        push(`${l}.${f}`);
    } else if (f || l) {
        const n = f || l;
        push(n);
        for (const role of ["hello", "team", "sales"]) push(`${n}.${role}`);
    }
    for (const n of NAMELESS) push(n);
    return out;
}

export type LocalPartProblem = "empty" | "too_long" | "invalid_chars" | "edge" | "dots" | "reserved" | "duplicate";

const MESSAGES: Record<LocalPartProblem, string> = {
    empty: "Type the part before the @.",
    too_long: `Keep it to ${LOCAL_PART_MAX} characters or fewer.`,
    invalid_chars: "Use lowercase letters, numbers, dots, dashes or underscores.",
    edge: "Start and end with a letter or number.",
    dots: "Two dots in a row are not allowed.",
    reserved: "That address is reserved by mail providers.",
    duplicate: "This address is already in the list.",
};

export function localPartMessage(p: LocalPartProblem): string {
    return MESSAGES[p];
}

/** What is wrong with one local part on its own, or null when it can be ordered. */
export function checkLocalPart(raw: string): LocalPartProblem | null {
    const v = raw.trim();
    if (!v) return "empty";
    if (v.length > LOCAL_PART_MAX) return "too_long";
    if (!/^[a-z0-9._-]+$/.test(v)) return "invalid_chars";
    if (!/^[a-z0-9]/.test(v) || !/[a-z0-9]$/.test(v)) return "edge";
    if (v.includes("..")) return "dots";
    if (RESERVED.has(v)) return "reserved";
    return null;
}

/** One verdict per address in a domain's list; the second of two equal addresses is the duplicate. */
export function checkLocalParts(parts: readonly string[]): (LocalPartProblem | null)[] {
    const seen = new Set<string>();
    return parts.map((raw) => {
        const own = checkLocalPart(raw);
        if (own) return own;
        const v = raw.trim();
        if (seen.has(v)) return "duplicate";
        seen.add(v);
        return null;
    });
}

/** What a typed character becomes in an address field: lowercase, and spaces read as dots. */
export function normalizeTyped(raw: string): string {
    return raw.toLowerCase().replace(/\s+/g, ".").replace(/@.*$/, "");
}

/** An optional website the bare domain forwards to; null when fine (empty included). */
export function forwardingProblem(raw: string): string | null {
    const v = raw.trim();
    if (!v) return null;
    let url: URL;
    try {
        url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? v : `https://${v}`);
    } catch {
        return "That does not look like a web address.";
    }
    if (url.protocol !== "https:" && url.protocol !== "http:") return "Use a web address that starts with https://.";
    if (!url.hostname.includes(".")) return "Include the full domain, like acme.com.";
    return null;
}

/** The forwarding URL as it is sent: https:// added when typed without a scheme. */
export function normalizeForwarding(raw: string): string {
    const v = raw.trim();
    if (!v) return "";
    return /^[a-z][a-z0-9+.-]*:\/\//i.test(v) ? v : `https://${v}`;
}
