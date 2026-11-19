// Address patterns: how a sender's name becomes the part before the @, from matt@ to meszaros.m@. Names are folded
// to plain ASCII first.
import { addressCandidates, checkLocalPart, LOCAL_PART_MAX, toAscii } from "./addresses";

export type AddressPattern = "first" | "first.l" | "first.last" | "firstlast" | "first_last" | "first-last" | "f.last" | "flast" | "firstl" | "last" | "last.first" | "lastf";

/** Every pattern, most natural first; a sender's mailboxes fall through to these after the chosen ones. */
export const ADDRESS_PATTERNS: readonly AddressPattern[] = ["first", "first.l", "first.last", "firstlast", "first_last", "first-last", "f.last", "flast", "firstl", "last", "last.first", "lastf"];

export const PATTERN_NAMES: Record<AddressPattern, string> = {
    first: "First name",
    "first.l": "First name and last initial",
    "first.last": "First and last name with a dot",
    firstlast: "First and last name",
    first_last: "First and last name with an underscore",
    "first-last": "First and last name with a dash",
    "f.last": "First initial and last name with a dot",
    flast: "First initial and last name",
    firstl: "First name and last initial, joined",
    last: "Last name",
    "last.first": "Last and first name with a dot",
    lastf: "Last name and first initial",
};

/** A name for the address. */
export interface NamedPerson {
    first: string;
    last: string;
}

/** A name's words, each split at its hyphens into ASCII parts: "Núñez-Ortiz Vega" -> [["nunez", "ortiz"], ["vega"]]. */
function words(text: string): string[][] {
    return toAscii(text)
        .split(/\s+/)
        .map((w) =>
            w
                .split("-")
                .map((p) => p.replace(/[^a-z0-9]/g, ""))
                .filter(Boolean),
        )
        .filter((w) => w.length > 0);
}

/** The parts of a name an address is built from. */
export function nameParts(person: NamedPerson): { first: string; last: string; lastHyphen: string } {
    const first = words(person.first)[0]?.join("") ?? "";
    const last = words(person.last);
    // "Núñez-Ortiz" keeps its hyphen where the last name stands as a word; "van der Berg" is one word either way.
    return { first, last: last.flat().join(""), lastHyphen: last.map((w) => w.join("-")).join("") };
}

function build(p: AddressPattern, f: string, l: string, lh: string): string {
    const fi = f[0] ?? "";
    const li = l[0] ?? "";
    const both = (a: string, b: string, sep = "") => (a && b ? `${a}${sep}${b}` : "");
    switch (p) {
        case "first":
            return f;
        case "first.l":
            return both(f, li, ".");
        case "first.last":
            return both(f, lh, ".");
        case "firstlast":
            return both(f, l);
        case "first_last":
            return both(f, lh, "_");
        case "first-last":
            return both(f, l, "-");
        case "f.last":
            return both(fi, lh, ".");
        case "flast":
            return both(fi, l);
        case "firstl":
            return both(f, li);
        case "last":
            return lh;
        case "last.first":
            return both(lh, f, ".");
        case "lastf":
            return both(l, fi);
    }
}

/** The address a pattern makes from a name, or "" when the name cannot make a valid one (no last name for first.last@). */
export function patternAddress(person: NamedPerson, p: AddressPattern): string {
    const { first, last, lastHyphen } = nameParts(person);
    const out = build(p, first, last, lastHyphen).slice(0, LOCAL_PART_MAX).replace(/[._-]+$/, "");
    return out && checkLocalPart(out) === null ? out : "";
}

/** The patterns a cart uses until the buyer picks: three of one person, or first.last@ for a team. */
export function defaultPatterns(senders: number): AddressPattern[] {
    return senders <= 1 ? ["first", "first.l", "first.last"] : ["first.last"];
}

/** The chosen patterns, then every other one in catalog order. */
export function patternSequence(chosen: readonly AddressPattern[]): AddressPattern[] {
    return [...chosen, ...ADDRESS_PATTERNS.filter((p) => !chosen.includes(p))];
}

/** Every address a person can take, in the order they are given out, without repeats. */
export function addressChoices(person: NamedPerson, patterns: readonly AddressPattern[]): string[] {
    const out: string[] = [];
    for (const p of patternSequence(patterns)) {
        const a = patternAddress(person, p);
        if (a && !out.includes(a)) out.push(a);
    }
    // With no name to build from, role addresses like hello@ and team@.
    return out.length > 0 ? out : addressCandidates({ first: "", last: "" });
}

export interface Picked {
    local_part: string;
    /** True when every pattern was taken and the address ends in a number. */
    numbered: boolean;
}

/** The first address for a person not in `used`, numbering the first one only when every pattern is taken. */
export function pickAddress(person: NamedPerson, patterns: readonly AddressPattern[], used: ReadonlySet<string>): Picked {
    const choices = addressChoices(person, patterns);
    for (const c of choices) if (!used.has(c)) return { local_part: c, numbered: false };
    const base = (choices[0] ?? "mail").slice(0, LOCAL_PART_MAX - 3).replace(/[._-]+$/, "");
    for (let i = 2; ; i++) if (!used.has(`${base}${i}`)) return { local_part: `${base}${i}`, numbered: true };
}

/** A saved cart's single address style, as patterns: the old default reads as automatic. */
export function patternsFromStyle(style: unknown): AddressPattern[] | null {
    if (style === "first.last" || style === "flast" || style === "firstl") return [style];
    return null;
}

export function isPattern(v: unknown): v is AddressPattern {
    return typeof v === "string" && (ADDRESS_PATTERNS as readonly string[]).includes(v);
}
