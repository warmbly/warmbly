// The unibox Compose button, in the list's title row on every viewport. The
// label is visually hidden on phones and the key hint below md.

import { PenLineIcon } from "lucide-react";
import ShortcutTooltip, { shortcutLabel } from "@/components/ui/shortcut-tooltip";
import { useComposeStore } from "@/hooks/useComposeStore";
import { useAccessRestricted } from "@/hooks/usePermission";
import useWorkspaceWritable, { showWorkspaceReadOnly } from "@/hooks/useWorkspaceWritable";
import { cn } from "@/lib/utils";

export default function ComposeButton() {
    const restricted = useAccessRestricted();
    const workspace = useWorkspaceWritable();
    // A member restricted to selected resources reads the inbox and sends nothing.
    if (restricted) return null;
    return (
        <ShortcutTooltip label={workspace.writable ? "New email" : "Sending is paused"} combo="n" side="bottom">
            <button
                type="button"
                aria-disabled={workspace.writable ? undefined : true}
                title={workspace.reason}
                onClick={() => (workspace.writable ? useComposeStore.getState().openCompose() : showWorkspaceReadOnly(workspace.reason))}
                className={cn(
                    "h-7 px-2 sm:pl-2 sm:pr-2.5 md:pr-1.5 rounded-md text-[12px] font-medium text-sky-700 inline-flex items-center gap-1.5 hover:bg-sky-50 active:bg-sky-100 transition-colors shrink-0",
                    !workspace.writable && "opacity-60 cursor-not-allowed",
                )}
            >
                <PenLineIcon className="w-3.5 h-3.5" />
                <span className="sr-only sm:not-sr-only">Compose</span>
                <kbd aria-hidden className="hidden md:inline-flex h-4 min-w-4 justify-center items-center px-1 ml-0.5 rounded border border-sky-200 bg-white font-mono text-[10px] leading-none text-sky-600">
                    {shortcutLabel("n")}
                </kbd>
            </button>
        </ShortcutTooltip>
    );
}
