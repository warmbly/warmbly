// Bit math for API permission and OAuth scope masks. They pass bit 31, where JavaScript's `&`, `|` and `~`
// work on 32-bit integers and silently drop the high bits, so every operation goes through BigInt.

const big = (n: number): bigint => BigInt(Math.trunc(n || 0));
const num = (b: bigint): number => Number(b);

/** Every bit of `bits` is set in `mask`. */
export function hasBits(mask: number, bits: number): boolean {
    const b = big(bits);
    return (big(mask) & b) === b;
}

/** At least one bit of `bits` is set in `mask`. */
export function anyBits(mask: number, bits: number): boolean {
    return (big(mask) & big(bits)) !== 0n;
}

export function orBits(...masks: number[]): number {
    return num(masks.reduce((acc, m) => acc | big(m), 0n));
}

export function andBits(a: number, b: number): number {
    return num(big(a) & big(b));
}

/** `mask` without any bit of `bits`. */
export function clearBits(mask: number, bits: number): number {
    return num(big(mask) & ~big(bits));
}

/** Clears `bits` when any of them is set, sets them otherwise: a checkbox for one permission. */
export function toggleBits(mask: number, bits: number): number {
    return anyBits(mask, bits) ? clearBits(mask, bits) : orBits(mask, bits);
}
