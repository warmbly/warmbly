// A button gated by a write permission and by the workspace taking writes. When
// both allow it, it behaves like a normal button; otherwise it renders visibly
// locked (a lock icon, muted, not-allowed cursor) and clicking it pops the
// permission dialog with the reason instead of running the action — so edits
// are blocked BEFORE any request, with a clear explanation. Use across the dashboard for edit / save /
// delete / create affordances.

import React from "react";
import { LockIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { useWriteGuard, type PermissionKey } from "@/hooks/usePermission";

interface Props extends Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "onClick"> {
    permission: PermissionKey;
    onClick?: (e: React.MouseEvent<HTMLButtonElement>) => void;
    /** Show a small lock glyph before the label when locked (default true). */
    showLock?: boolean;
}

export default function PermissionButton({
    permission,
    onClick,
    showLock = true,
    children,
    className,
    disabled,
    title,
    ...rest
}: Props) {
    const { locked, reason, guard } = useWriteGuard(permission);
    return (
        <button
            {...rest}
            // When allowed, honor the caller's disabled (e.g. pending). When
            // locked, stay clickable so the guard can pop the explanation — a
            // truly-disabled button would swallow the click silently.
            disabled={locked ? undefined : disabled}
            aria-disabled={locked || disabled || undefined}
            title={locked ? reason : title}
            onClick={guard(onClick)}
            className={cn(className, locked && "opacity-60 cursor-not-allowed")}
        >
            {locked && showLock && <LockIcon className="w-3 h-3 shrink-0" />}
            {children}
        </button>
    );
}
