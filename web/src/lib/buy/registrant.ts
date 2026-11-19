// The domain registrant: prefilled from billing and the account, saved quietly, asked for only where it has gaps.
import type { Registrant } from "@/lib/api/models/app/store/Store";

export type RegistrantField = keyof Registrant;

export const REGISTRANT_FIELDS: RegistrantField[] = [
    "first_name",
    "last_name",
    "organization",
    "email",
    "phone",
    "address1",
    "address2",
    "city",
    "state",
    "postal_code",
    "country",
];

export const REQUIRED_FIELDS: RegistrantField[] = ["first_name", "last_name", "email", "phone", "address1", "city", "postal_code", "country"];

export function blankRegistrant(seed?: Partial<Registrant>): Registrant {
    return {
        first_name: "",
        last_name: "",
        organization: "",
        email: "",
        phone: "",
        address1: "",
        address2: "",
        city: "",
        state: "",
        postal_code: "",
        country: "",
        ...seed,
    };
}

/** Field -> what is wrong with it. Empty when the registrant can be sent. */
export function registrantProblems(r: Registrant): Partial<Record<RegistrantField, string>> {
    const out: Partial<Record<RegistrantField, string>> = {};
    for (const f of REQUIRED_FIELDS) if (!r[f].trim()) out[f] = "Required";
    if (r.email.trim() && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(r.email.trim())) out.email = "Enter an email address.";
    if (r.phone.trim() && !/^\+\d{1,3}\.\d{4,14}$/.test(r.phone.trim())) out.phone = "Use the form +1.4155550100.";
    if (r.country.trim() && !/^[A-Z]{2}$/.test(r.country.trim())) out.country = "Two letters, like US.";
    return out;
}

export function registrantComplete(r: Registrant | null | undefined): boolean {
    return !!r && Object.keys(registrantProblems(r)).length === 0;
}

/** "+1 (415) 555-0100" or "+44 20 7946 0958" typed loosely -> "+1.4155550100" when it can tell. */
export function normalizePhone(raw: string): string {
    const v = raw.trim();
    if (/^\+\d{1,3}\.\d+$/.test(v)) return v;
    const m = /^\+(\d{1,3})[\s.-]+(.+)$/.exec(v);
    if (!m) return v;
    const rest = m[2].replace(/\D/g, "");
    return rest ? `+${m[1]}.${rest}` : v;
}

export const REGISTRANT_LABELS: Record<RegistrantField, string> = {
    first_name: "First name",
    last_name: "Last name",
    organization: "Company",
    email: "Email",
    phone: "Phone",
    address1: "Street address",
    address2: "Apartment, suite",
    city: "City",
    state: "State or region",
    postal_code: "Postal code",
    country: "Country code",
};

/** The fields the registry still needs, in form order: what is missing or malformed, plus what the server refused. */
export function registrantGaps(r: Registrant, refused: readonly RegistrantField[] = []): RegistrantField[] {
    const problems = registrantProblems(r);
    return REGISTRANT_FIELDS.filter((f) => problems[f] !== undefined || refused.includes(f));
}
