// One of a brand's uploaded images: a thumbnail, Upload or Replace, and Remove.

import React from "react";
import { ImageIcon } from "lucide-react";
import toast from "react-hot-toast/headless";

import { DashboardImage } from "@/components/ui/dashboard-image";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import { cn } from "@/lib/utils";

export interface BrandImageControl {
    /** The stored image, or empty. */
    url: string;
    upload: (file: File) => Promise<unknown>;
    remove: () => Promise<unknown>;
    busy: boolean;
}

export default function ImageField({
    image,
    name,
    accept,
    maxBytes,
    canWrite,
    shape = "wide",
}: {
    image: BrandImageControl;
    /** What the image is called in a message: "logo", "icon", "sign-in image". */
    name: string;
    /** MIME types the route takes. */
    accept: string[];
    maxBytes: number;
    canWrite: boolean;
    shape?: "wide" | "square" | "landscape";
}) {
    const fileRef = React.useRef<HTMLInputElement>(null);
    const mb = Math.round(maxBytes / (1 << 20));

    async function pick(file: File) {
        if (!accept.includes(file.type)) {
            const kinds = accept.map((t) => (t === "image/jpeg" ? "JPG" : t.replace("image/", "").toUpperCase())).join(" or ");
            toast.error(`The ${name} must be a ${kinds} file.`);
            return;
        }
        if (file.size > maxBytes) {
            toast.error(`The ${name} must be ${mb} MB or smaller. Compress or resize it and try again.`);
            return;
        }
        try {
            await image.upload(file);
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    async function remove() {
        try {
            await image.remove();
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <div className="flex items-center gap-2">
            <span
                className={cn(
                    "rounded-md border border-slate-200 bg-slate-50 shrink-0 overflow-hidden inline-flex items-center justify-center",
                    shape === "square" ? "size-9" : shape === "landscape" ? "h-12 w-20" : "h-9 w-20",
                )}
            >
                {image.url ? (
                    <DashboardImage
                        src={image.url}
                        alt=""
                        className={shape === "landscape" ? "h-full w-full object-cover" : "max-h-full max-w-full object-contain"}
                    />
                ) : (
                    <ImageIcon className="w-3.5 h-3.5 text-slate-300" />
                )}
            </span>
            <button
                type="button"
                disabled={!canWrite || image.busy}
                onClick={() => fileRef.current?.click()}
                className="h-7 px-2.5 rounded-md border border-slate-200 text-[12px] text-slate-600 hover:bg-slate-50 disabled:opacity-50"
            >
                {image.busy ? "Working…" : image.url ? "Replace" : "Upload"}
            </button>
            {image.url && !image.busy && (
                <button
                    type="button"
                    disabled={!canWrite}
                    onClick={() => void remove()}
                    className="h-7 px-2 rounded-md text-[12px] text-slate-500 hover:text-rose-600 hover:bg-rose-50 disabled:opacity-50"
                >
                    Remove
                </button>
            )}
            <input
                ref={fileRef}
                type="file"
                accept={accept.join(",")}
                className="hidden"
                aria-label={`Upload the ${name}`}
                onChange={(e) => {
                    const f = e.target.files?.[0];
                    e.target.value = "";
                    if (f) void pick(f);
                }}
            />
        </div>
    );
}
