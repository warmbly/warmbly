// The checkout's square photo crop: pure geometry for the cropper and the file checks before it opens.

/** What the picker accepts; the server takes PNG or JPG only. */
export const PHOTO_ACCEPT = "image/png,image/jpeg";
/** The largest file the picker opens; the cropped result is far smaller. */
export const PHOTO_MAX_INPUT_BYTES = 15 * 1024 * 1024;
/** GraphKit's limit on the uploaded photo. */
export const PHOTO_MAX_BYTES = 2 * 1024 * 1024;
/** The square the crop is drawn to. */
export const PHOTO_OUTPUT = 512;
export const PHOTO_MAX_ZOOM = 4;

/** Why a chosen file cannot be used, or null when it can be cropped. */
export function photoFileProblem(file: { type: string; size: number }): string | null {
    if (file.type !== "image/png" && file.type !== "image/jpeg") return "Choose a PNG or JPG photo.";
    if (file.size > PHOTO_MAX_INPUT_BYTES) return "That photo is too large. Choose one under 15 MB.";
    if (file.size === 0) return "That file is empty.";
    return null;
}

export interface CropState {
    /** 1 fills the square with the photo's shorter side. */
    zoom: number;
    /** The photo's top-left corner inside the square, in square pixels; never positive. */
    x: number;
    y: number;
}

/** Pixels per photo pixel at a zoom, in a square of side view. */
export function cropScale(w: number, h: number, view: number, zoom: number): number {
    return (view / Math.min(w, h)) * zoom;
}

export function clampZoom(z: number): number {
    if (!Number.isFinite(z)) return 1;
    return Math.min(PHOTO_MAX_ZOOM, Math.max(1, z));
}

/** Keeps the photo covering the whole square. */
export function clampCrop(c: CropState, w: number, h: number, view: number): CropState {
    const zoom = clampZoom(c.zoom);
    const s = cropScale(w, h, view, zoom);
    const minX = view - w * s;
    const minY = view - h * s;
    return { zoom, x: Math.min(0, Math.max(minX, c.x)), y: Math.min(0, Math.max(minY, c.y)) };
}

/** The photo centred in the square at a zoom. */
export function centredCrop(w: number, h: number, view: number, zoom = 1): CropState {
    const s = cropScale(w, h, view, clampZoom(zoom));
    return clampCrop({ zoom, x: (view - w * s) / 2, y: (view - h * s) / 2 }, w, h, view);
}

/** Zooms about the square's centre, keeping what is in the middle in the middle. */
export function zoomCrop(c: CropState, zoom: number, w: number, h: number, view: number): CropState {
    const next = clampZoom(zoom);
    const before = cropScale(w, h, view, c.zoom);
    const after = cropScale(w, h, view, next);
    const cx = (view / 2 - c.x) / before;
    const cy = (view / 2 - c.y) / before;
    return clampCrop({ zoom: next, x: view / 2 - cx * after, y: view / 2 - cy * after }, w, h, view);
}

/** The square of the photo the crop shows, in photo pixels: what gets drawn to the output. */
export function cropSource(c: CropState, w: number, h: number, view: number): { sx: number; sy: number; size: number } {
    const s = cropScale(w, h, view, c.zoom);
    const size = Math.min(view / s, w, h);
    return { sx: Math.max(0, Math.min(w - size, -c.x / s)), sy: Math.max(0, Math.min(h - size, -c.y / s)), size };
}
