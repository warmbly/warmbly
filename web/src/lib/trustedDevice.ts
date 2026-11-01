// Trusted-device tokens, one per account, issued when someone ticks "Remember
// this device" on the emailed-code step. A token only lets the next password
// sign-in skip that code; the server still asks for the password, TOTP and,
// when the sign-in looks unusual, the code anyway.
//
// Signing out forgets the tokens this browser holds, on the server and here.
// The server forgets every token when the password changes or the user signs
// out everywhere.
const KEY = "warmbly-trusted-devices";

type Store = Record<string, string>;

const accountKey = (email: string) => email.trim().toLowerCase();

function read(): Store {
    try {
        const raw = localStorage.getItem(KEY);
        const parsed: unknown = raw ? JSON.parse(raw) : {};
        return parsed && typeof parsed === "object" && !Array.isArray(parsed) ? (parsed as Store) : {};
    } catch {
        return {};
    }
}

function write(store: Store) {
    try {
        if (Object.keys(store).length) localStorage.setItem(KEY, JSON.stringify(store));
        else localStorage.removeItem(KEY);
    } catch {
        /* storage unavailable: the user just gets a code next time */
    }
}

export function getTrustedDevice(email: string): string | undefined {
    const tok = read()[accountKey(email)];
    return typeof tok === "string" && tok ? tok : undefined;
}

export function setTrustedDevice(email: string, token: string) {
    write({ ...read(), [accountKey(email)]: token });
}

export function forgetTrustedDevice(email: string) {
    const store = read();
    delete store[accountKey(email)];
    write(store);
}

export function allTrustedDevices(): string[] {
    return Object.values(read()).filter((tok): tok is string => typeof tok === "string" && tok !== "");
}

export function forgetAllTrustedDevices() {
    write({});
}
