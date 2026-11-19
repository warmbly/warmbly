// Pick a PNG or JPG, crop it square and upload it as a mailbox profile photo. No crop library: drag to move,
// the slider or the wheel to zoom, arrow keys to nudge. The square is redrawn to a 512 px JPEG before upload.
import React from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, motion, useReducedMotion } from "framer-motion";
import { ImageIcon, Loader2Icon, MinusIcon, PlusIcon, XIcon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { uploadStorePhoto } from "@/lib/api/client/app/store/store";
import type { StorePhoto } from "@/lib/api/models/app/store/Store";
import { storeErrorMessage } from "@/lib/buy/errors";
import {
    centredCrop,
    clampCrop,
    cropSource,
    photoFileProblem,
    PHOTO_ACCEPT,
    PHOTO_MAX_BYTES,
    PHOTO_MAX_ZOOM,
    PHOTO_OUTPUT,
    zoomCrop,
    type CropState,
} from "@/lib/buy/photo";

const VIEW = 240;

export interface PickedPhoto {
    photo: StorePhoto;
    /** The cropped JPEG that was uploaded, for an instant preview. */
    blob: Blob;
}

/** Renders `children(open, busy)`; `open` asks for a file, then crops and uploads it. */
export default function PhotoPicker({
    onPicked,
    title = "Profile photo",
    hint,
    children,
}: {
    onPicked: (p: PickedPhoto) => void | Promise<void>;
    title?: string;
    hint?: string;
    children: (open: () => void, busy: boolean) => React.ReactNode;
}) {
    const input = React.useRef<HTMLInputElement>(null);
    const [image, setImage] = React.useState<HTMLImageElement | null>(null);
    const [busy, setBusy] = React.useState(false);

    const open = React.useCallback(() => {
        if (input.current) {
            input.current.value = "";
            input.current.click();
        }
    }, []);

    const onFile = (file: File | undefined) => {
        if (!file) return;
        const problem = photoFileProblem(file);
        if (problem) {
            toast.error(problem);
            return;
        }
        const url = URL.createObjectURL(file);
        const img = new Image();
        img.onload = () => setImage(img);
        img.onerror = () => {
            URL.revokeObjectURL(url);
            toast.error("That file could not be read as a photo.");
        };
        img.src = url;
    };

    const close = React.useCallback(() => {
        setImage((img) => {
            if (img) URL.revokeObjectURL(img.src);
            return null;
        });
    }, []);

    const confirm = async (blob: Blob) => {
        setBusy(true);
        try {
            const photo = await uploadStorePhoto(blob);
            await onPicked({ photo, blob });
            close();
        } catch (e) {
            toast.error(storeErrorMessage(e));
        } finally {
            setBusy(false);
        }
    };

    return (
        <>
            <input ref={input} type="file" accept={PHOTO_ACCEPT} className="sr-only" tabIndex={-1} aria-hidden="true" onChange={(e) => onFile(e.target.files?.[0])} />
            {children(open, busy)}
            {createPortal(
                <AnimatePresence>{image && <CropDialog key={image.src} image={image} title={title} hint={hint} busy={busy} onCancel={close} onDone={confirm} />}</AnimatePresence>,
                document.body,
            )}
        </>
    );
}

function CropDialog({
    image,
    title,
    hint,
    busy,
    onCancel,
    onDone,
}: {
    image: HTMLImageElement;
    title: string;
    hint?: string;
    busy: boolean;
    onCancel: () => void;
    onDone: (blob: Blob) => void;
}) {
    const reduced = useReducedMotion();
    const w = image.naturalWidth;
    const h = image.naturalHeight;
    const [crop, setCrop] = React.useState<CropState>(() => centredCrop(w, h, VIEW));
    const drag = React.useRef<{ id: number; x: number; y: number; start: CropState } | null>(null);
    const frame = React.useRef<HTMLDivElement>(null);
    const [encodeError, setEncodeError] = React.useState<string | null>(null);

    React.useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (e.key !== "Escape" || busy) return;
            if (document.querySelector("[data-floating], [role='alertdialog']")) return;
            e.stopPropagation();
            onCancel();
        };
        document.addEventListener("keydown", onKey);
        return () => document.removeEventListener("keydown", onKey);
    }, [busy, onCancel]);

    // The wheel zooms; registered by hand so it can stop the page scrolling under the dialog.
    React.useEffect(() => {
        const el = frame.current;
        if (!el) return;
        const onWheel = (e: WheelEvent) => {
            e.preventDefault();
            setCrop((c) => zoomCrop(c, c.zoom * (e.deltaY < 0 ? 1.08 : 1 / 1.08), w, h, VIEW));
        };
        el.addEventListener("wheel", onWheel, { passive: false });
        return () => el.removeEventListener("wheel", onWheel);
    }, [w, h]);

    const scale = (VIEW / Math.min(w, h)) * crop.zoom;

    const encode = async () => {
        setEncodeError(null);
        const canvas = document.createElement("canvas");
        canvas.width = PHOTO_OUTPUT;
        canvas.height = PHOTO_OUTPUT;
        const ctx = canvas.getContext("2d");
        if (!ctx) {
            setEncodeError("This browser cannot crop photos.");
            return;
        }
        ctx.imageSmoothingEnabled = true;
        ctx.imageSmoothingQuality = "high";
        const src = cropSource(crop, w, h, VIEW);
        ctx.drawImage(image, src.sx, src.sy, src.size, src.size, 0, 0, PHOTO_OUTPUT, PHOTO_OUTPUT);
        let blob: Blob | null = null;
        for (const q of [0.88, 0.75, 0.6]) {
            blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, "image/jpeg", q));
            if (blob && blob.size <= PHOTO_MAX_BYTES) break;
        }
        if (!blob || blob.size > PHOTO_MAX_BYTES) {
            setEncodeError("That photo could not be made small enough. Try another one.");
            return;
        }
        onDone(blob);
    };

    const nudge = (dx: number, dy: number) => setCrop((c) => clampCrop({ ...c, x: c.x + dx, y: c.y + dy }, w, h, VIEW));

    return (
        <motion.div
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            onMouseDown={() => !busy && onCancel()}
            className="fixed inset-0 z-[130] flex items-center justify-center bg-slate-900/30 backdrop-blur-[2px] px-4"
        >
            <motion.div
                role="dialog"
                aria-modal="true"
                aria-labelledby="photo-crop-title"
                initial={reduced ? { opacity: 0 } : { opacity: 0, y: 8, scale: 0.98 }}
                animate={{ opacity: 1, y: 0, scale: 1 }}
                exit={reduced ? { opacity: 0 } : { opacity: 0, y: 8, scale: 0.98 }}
                transition={{ duration: 0.18, ease: [0.22, 1, 0.36, 1] }}
                onMouseDown={(e) => e.stopPropagation()}
                className="w-full max-w-[340px] rounded-xl border border-slate-200 bg-white shadow-[0_24px_48px_-16px_rgba(15,23,42,0.3)] overflow-hidden"
            >
                <div className="px-4 h-12 flex items-center gap-2 border-b border-slate-200">
                    <ImageIcon className="w-3.5 h-3.5 text-slate-400" />
                    <h2 id="photo-crop-title" className="text-[13px] font-semibold text-slate-900 flex-1 truncate">
                        {title}
                    </h2>
                    <button
                        type="button"
                        onClick={onCancel}
                        disabled={busy}
                        aria-label="Close"
                        className="size-7 rounded-md flex items-center justify-center text-slate-400 hover:text-slate-900 hover:bg-slate-100 disabled:opacity-40 transition-colors"
                    >
                        <XIcon className="w-4 h-4" />
                    </button>
                </div>
                <div className="p-4 flex flex-col items-center">
                    <div
                        ref={frame}
                        role="application"
                        aria-label="Drag to move the photo. Arrow keys move it, plus and minus zoom."
                        tabIndex={0}
                        onPointerDown={(e) => {
                            e.currentTarget.setPointerCapture(e.pointerId);
                            drag.current = { id: e.pointerId, x: e.clientX, y: e.clientY, start: crop };
                        }}
                        onPointerMove={(e) => {
                            const d = drag.current;
                            if (!d || d.id !== e.pointerId) return;
                            setCrop(clampCrop({ ...d.start, x: d.start.x + e.clientX - d.x, y: d.start.y + e.clientY - d.y }, w, h, VIEW));
                        }}
                        onPointerUp={() => {
                            drag.current = null;
                        }}
                        onPointerCancel={() => {
                            drag.current = null;
                        }}
                        onKeyDown={(e) => {
                            const step = e.shiftKey ? 20 : 4;
                            const moves: Record<string, [number, number]> = { ArrowLeft: [step, 0], ArrowRight: [-step, 0], ArrowUp: [0, step], ArrowDown: [0, -step] };
                            if (moves[e.key]) {
                                e.preventDefault();
                                nudge(...moves[e.key]);
                            } else if (e.key === "+" || e.key === "=") {
                                e.preventDefault();
                                setCrop((c) => zoomCrop(c, c.zoom * 1.1, w, h, VIEW));
                            } else if (e.key === "-") {
                                e.preventDefault();
                                setCrop((c) => zoomCrop(c, c.zoom / 1.1, w, h, VIEW));
                            }
                        }}
                        style={{ width: VIEW, height: VIEW }}
                        className="relative overflow-hidden rounded-lg bg-slate-100 cursor-grab active:cursor-grabbing touch-none select-none outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                    >
                        <img
                            src={image.src}
                            alt=""
                            draggable={false}
                            style={{ width: w * scale, height: h * scale, transform: `translate(${crop.x}px, ${crop.y}px)` }}
                            className="absolute left-0 top-0 max-w-none pointer-events-none"
                        />
                        {/* The round shape mail clients show; the corners are still part of the photo. */}
                        <div className="absolute inset-0 pointer-events-none rounded-full shadow-[0_0_0_999px_rgba(255,255,255,0.55)] ring-1 ring-white/80" />
                    </div>
                    <div className="mt-3 w-full flex items-center gap-2">
                        <button
                            type="button"
                            aria-label="Zoom out"
                            onClick={() => setCrop((c) => zoomCrop(c, c.zoom / 1.2, w, h, VIEW))}
                            className="size-7 rounded-md inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            <MinusIcon className="w-3.5 h-3.5" />
                        </button>
                        <input
                            type="range"
                            aria-label="Zoom"
                            min={1}
                            max={PHOTO_MAX_ZOOM}
                            step={0.01}
                            value={crop.zoom}
                            onChange={(e) => setCrop((c) => zoomCrop(c, Number(e.target.value), w, h, VIEW))}
                            className="flex-1 h-1 accent-sky-600 cursor-pointer"
                        />
                        <button
                            type="button"
                            aria-label="Zoom in"
                            onClick={() => setCrop((c) => zoomCrop(c, c.zoom * 1.2, w, h, VIEW))}
                            className="size-7 rounded-md inline-flex items-center justify-center text-slate-500 hover:text-slate-900 hover:bg-slate-100 transition-colors"
                        >
                            <PlusIcon className="w-3.5 h-3.5" />
                        </button>
                    </div>
                    <p className="mt-2 text-[11.5px] text-slate-500 text-center leading-relaxed">
                        {hint ?? "Recipients see this next to the sender name in Gmail and Outlook."}
                    </p>
                    {encodeError && <p className="mt-1 text-[11.5px] text-red-600 text-center">{encodeError}</p>}
                </div>
                <div className="px-4 py-3 border-t border-slate-200 flex items-center justify-end gap-2">
                    <button
                        type="button"
                        onClick={onCancel}
                        disabled={busy}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] font-medium text-slate-700 disabled:opacity-50 transition-colors"
                    >
                        Cancel
                    </button>
                    <button
                        type="button"
                        onClick={() => void encode()}
                        disabled={busy}
                        className="h-7 px-3 rounded-md bg-sky-600 hover:bg-sky-700 disabled:opacity-60 text-white text-[12px] font-semibold inline-flex items-center gap-1.5 transition-colors"
                    >
                        {busy && <Loader2Icon className="w-3 h-3 animate-spin" />}
                        Use photo
                    </button>
                </div>
            </motion.div>
        </motion.div>
    );
}
