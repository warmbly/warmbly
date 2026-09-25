// The unibox bulk endpoints refuse more than this many ids per request
// (errx.ErrSeenMax, SnoozeMaxThreads), so a large selection goes in batches.
export const UNIBOX_BULK_MAX = 500;

/** Runs `send` once per slice, in order; the first failure stops the rest. */
export async function inChunks<T>(
    items: T[],
    size: number,
    send: (chunk: T[]) => Promise<unknown>,
): Promise<void> {
    for (let i = 0; i < items.length; i += size) {
        await send(items.slice(i, i + size));
    }
}

/** Slices two id lists in step, so each request stays under the cap for both. */
export function pairedChunks(
    a: string[],
    b: string[],
    size: number,
): { a: string[]; b: string[] }[] {
    const n = Math.max(1, Math.ceil(a.length / size), Math.ceil(b.length / size));
    return Array.from({ length: n }, (_, i) => ({
        a: a.slice(i * size, (i + 1) * size),
        b: b.slice(i * size, (i + 1) * size),
    }));
}
