// Client-side avatar pipeline.
//
// Approach lifted from how Linear / Notion / Slack handle avatars:
//
//   1. User picks a file.
//   2. We decode it in-browser, render onto a square canvas centered
//      and "cover"-cropped at 512px (2× a 256-display so retina stays
//      sharp without blowing up bandwidth).
//   3. Re-encode as image/jpeg at quality 0.85 — JPEG beats PNG for
//      photos and beats WebP for browser compatibility on the
//      receiving end.
//   4. Reject the upload if it's still too large after compression
//      (shouldn't happen at 512px) or if the source can't be decoded.
//
// The server enforces the same 1024px dimension cap so a bypass of
// the JS resize still gets a 400.

/** Final output dimension. 2× of a 256px display ratio. */
export const AVATAR_OUTPUT_DIMENSION = 512;

/** Cap on the original file the user can choose, in bytes. */
export const AVATAR_MAX_INPUT_BYTES = 10 * 1024 * 1024; // 10 MB raw

/** Accepted mime types on the input picker. PNG + JPG only — WebP,
 *  GIF and SVG are excluded server-side too because they each carry
 *  decoder CVE history or script-execution risk that isn't worth the
 *  surface area for a feature this small. */
export const AVATAR_ACCEPT = "image/png,image/jpeg,image/jpg";

export interface ResizedAvatar {
    /** Resized image as a Blob, ready to POST as multipart. */
    blob: Blob;
    /** Object URL of the blob — caller is responsible for revoking. */
    previewUrl: string;
    /** Final dimension in px (square). */
    size: number;
}

/**
 * Read a File, draw it onto a square canvas using a "cover" crop,
 * re-encode as JPEG. Resolves with the resulting Blob + an object
 * URL for preview.
 *
 * Errors thrown have user-readable messages — surface directly via
 * toast.
 */
export async function resizeAvatar(file: File): Promise<ResizedAvatar> {
    if (file.size > AVATAR_MAX_INPUT_BYTES) {
        throw new Error("That image is huge — pick something under 10 MB.");
    }

    const bitmap = await loadBitmap(file);
    const size = AVATAR_OUTPUT_DIMENSION;
    const canvas = document.createElement("canvas");
    canvas.width = size;
    canvas.height = size;
    const ctx = canvas.getContext("2d");
    if (!ctx) {
        throw new Error("Canvas isn't available in this browser.");
    }

    // Cover crop: scale so the shorter side fills the canvas, then
    // center the longer side.
    const scale = size / Math.min(bitmap.width, bitmap.height);
    const drawW = bitmap.width * scale;
    const drawH = bitmap.height * scale;
    const dx = (size - drawW) / 2;
    const dy = (size - drawH) / 2;

    // Slightly better resampling than browser default.
    ctx.imageSmoothingEnabled = true;
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(bitmap, dx, dy, drawW, drawH);

    // ImageBitmap exposes close(); HTMLImageElement doesn't. Narrow
    // via the instanceof check rather than optional chaining so TS's
    // erased-narrow stays valid under strict.
    if (typeof ImageBitmap !== "undefined" && bitmap instanceof ImageBitmap) {
        bitmap.close();
    }

    const blob = await new Promise<Blob | null>((resolve) =>
        canvas.toBlob(resolve, "image/jpeg", 0.85),
    );
    if (!blob) throw new Error("Couldn't encode the resized image.");

    return {
        blob,
        previewUrl: URL.createObjectURL(blob),
        size,
    };
}

/**
 * Decode a File into an ImageBitmap. createImageBitmap is the modern
 * path; we keep an HTMLImageElement fallback for older browsers that
 * don't have it (notably some embedded webviews).
 */
async function loadBitmap(file: File): Promise<ImageBitmap | HTMLImageElement> {
    if ("createImageBitmap" in window) {
        try {
            return await createImageBitmap(file);
        } catch {
            // Some browsers reject specific formats from createImageBitmap
            // (Safari + GIF historically). Fall through to <img>.
        }
    }
    const url = URL.createObjectURL(file);
    try {
        const img = new Image();
        await new Promise<void>((resolve, reject) => {
            img.onload = () => resolve();
            img.onerror = () => reject(new Error("That file isn't a valid image."));
            img.src = url;
        });
        return img;
    } finally {
        // Revoke after the bitmap is decoded; the Image already
        // holds the data.
        URL.revokeObjectURL(url);
    }
}
