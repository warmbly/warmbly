// AvatarUploader — drop-in row for the Profile + Workspace settings.
//
//   <AvatarUploader
//     current={user.avatar_url}
//     fallbackInitials={initials(user.email)}
//     label="Profile photo"
//     onUpload={blob => uploadUserAvatar(blob)}
//     onRemove={() => removeUserAvatar()}
//   />
//
// Handles the file picker, client-side resize (lib/avatar.ts) and the
// preview/uploading/error states. Doesn't know about specific
// endpoints — the page wires `onUpload` to the right mutation.

import React from "react";
import { CameraIcon, ImageIcon, Loader2Icon, TrashIcon } from "lucide-react";
import toast from "react-hot-toast";
import {
    AVATAR_ACCEPT,
    AVATAR_OUTPUT_DIMENSION,
    resizeAvatar,
    type ResizedAvatar,
} from "@/lib/avatar";

interface Props {
    /** Existing avatar URL or undefined when none. */
    current?: string | null;
    /** Initials shown on the placeholder. */
    fallbackInitials: string;
    /** Avatar shape — circle for users, rounded square for workspaces. */
    shape?: "circle" | "square";
    /** Returns once the upload's done; throw with a user-readable message on failure. */
    onUpload: (blob: Blob) => Promise<void>;
    /** Remove handler. Hidden when no current avatar exists. */
    onRemove?: () => Promise<void>;
    /** Show a smaller variant — used in modals where 80px is too much. */
    size?: "sm" | "md";
}

export function AvatarUploader({
    current,
    fallbackInitials,
    shape = "circle",
    onUpload,
    onRemove,
    size = "md",
}: Props) {
    const inputRef = React.useRef<HTMLInputElement>(null);
    const [preview, setPreview] = React.useState<ResizedAvatar | null>(null);
    const [uploading, setUploading] = React.useState(false);
    const [removing, setRemoving] = React.useState(false);

    // Clean up the object URL when the component unmounts or replaces.
    React.useEffect(() => {
        return () => {
            if (preview) URL.revokeObjectURL(preview.previewUrl);
        };
    }, [preview]);

    async function pick() {
        inputRef.current?.click();
    }

    async function onPicked(e: React.ChangeEvent<HTMLInputElement>) {
        const f = e.target.files?.[0];
        e.target.value = ""; // allow re-pick of same file later
        if (!f) return;

        let resized: ResizedAvatar;
        try {
            resized = await resizeAvatar(f);
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Couldn't process that image.");
            return;
        }

        // Show the new image immediately while the upload runs.
        setPreview(resized);
        setUploading(true);
        try {
            await onUpload(resized.blob);
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Upload failed.");
            // Drop the optimistic preview on failure.
            URL.revokeObjectURL(resized.previewUrl);
            setPreview(null);
        } finally {
            setUploading(false);
        }
    }

    async function doRemove() {
        if (!onRemove) return;
        setRemoving(true);
        try {
            await onRemove();
            if (preview) URL.revokeObjectURL(preview.previewUrl);
            setPreview(null);
        } catch (err) {
            toast.error(err instanceof Error ? err.message : "Couldn't remove the avatar.");
        } finally {
            setRemoving(false);
        }
    }

    const displayUrl = preview?.previewUrl ?? current ?? null;
    const dim = size === "sm" ? "size-10 text-[12px]" : "size-16 text-[20px]";
    const radius = shape === "circle" ? "rounded-full" : "rounded-md";

    return (
        <div className="flex items-center gap-3">
            <button
                type="button"
                onClick={pick}
                disabled={uploading || removing}
                aria-label="Change avatar"
                className={`group relative ${dim} ${radius} bg-slate-900 text-white flex items-center justify-center shrink-0 overflow-hidden disabled:opacity-60 transition-shadow hover:ring-2 hover:ring-slate-300 ring-offset-1`}
            >
                {displayUrl ? (
                    <img
                        src={displayUrl}
                        alt=""
                        className={`absolute inset-0 w-full h-full object-cover ${radius}`}
                    />
                ) : (
                    <span className="font-semibold">{fallbackInitials}</span>
                )}
                <span
                    className={`absolute inset-0 ${radius} bg-slate-900/0 group-hover:bg-slate-900/40 group-focus:bg-slate-900/40 transition-colors flex items-center justify-center`}
                >
                    {uploading ? (
                        <Loader2Icon className="w-4 h-4 animate-spin text-white" />
                    ) : (
                        <CameraIcon className="w-3.5 h-3.5 text-white opacity-0 group-hover:opacity-100 group-focus:opacity-100 transition-opacity" />
                    )}
                </span>
            </button>
            <div className="min-w-0 flex-1">
                <div className="text-[11px] text-slate-500 leading-snug">
                    {uploading
                        ? "Uploading…"
                        : `PNG, JPG, WebP or GIF. We resize to ${AVATAR_OUTPUT_DIMENSION}px before upload.`}
                </div>
                <div className="flex items-center gap-1 mt-1.5">
                    <button
                        type="button"
                        onClick={pick}
                        disabled={uploading || removing}
                        className="h-7 px-2.5 rounded-md border border-slate-200 hover:border-slate-300 text-[12px] text-slate-700 hover:text-slate-900 transition-colors inline-flex items-center gap-1.5 disabled:opacity-60"
                    >
                        <ImageIcon className="w-3 h-3" />
                        {current || preview ? "Replace" : "Upload"}
                    </button>
                    {(current || preview) && onRemove && (
                        <button
                            type="button"
                            onClick={doRemove}
                            disabled={uploading || removing}
                            className="h-7 px-2.5 rounded-md text-[12px] text-slate-500 hover:text-red-700 hover:bg-red-50 transition-colors inline-flex items-center gap-1.5 disabled:opacity-60"
                        >
                            {removing ? (
                                <Loader2Icon className="w-3 h-3 animate-spin" />
                            ) : (
                                <TrashIcon className="w-3 h-3" />
                            )}
                            Remove
                        </button>
                    )}
                </div>
            </div>
            <input
                ref={inputRef}
                type="file"
                accept={AVATAR_ACCEPT}
                onChange={onPicked}
                className="hidden"
            />
        </div>
    );
}
