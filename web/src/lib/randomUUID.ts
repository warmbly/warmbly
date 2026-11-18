// crypto.randomUUID exists only in secure contexts; a plain-http self-host has getRandomValues alone.
export function installRandomUUID(c: Crypto | undefined = globalThis.crypto): void {
    if (!c || typeof c.randomUUID === "function" || typeof c.getRandomValues !== "function") return;
    c.randomUUID = function randomUUID() {
        const b = c.getRandomValues(new Uint8Array(16));
        b[6] = (b[6] & 0x0f) | 0x40;
        b[8] = (b[8] & 0x3f) | 0x80;
        const h = Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
        return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}` as `${string}-${string}-${string}-${string}-${string}`;
    };
}
