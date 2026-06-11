import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { useResourceViewers } from "@/hooks/PresenceProvider";
import { cn } from "@/lib/utils";

function initialsOf(name: string | null) {
    if (!name) return "?";
    return name
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 2)
        .map((part) => part[0]?.toUpperCase())
        .join("");
}

const ACTION_LABEL: Record<string, string> = {
    viewing: "viewing",
    editing: "editing",
    replying: "replying",
};

/**
 * "Someone is already here" indicator for detail panes and editors. Renders
 * nothing when the record has no other live viewers; otherwise an avatar
 * stack plus the strongest activity ("editing"/"replying" beats "viewing").
 */
export default function ResourceViewers({
    resource,
    className,
}: {
    resource: string | null;
    className?: string;
}) {
    const viewers = useResourceViewers(resource);
    if (viewers.length === 0) return null;

    const strongest =
        viewers.find((v) => v.action === "replying") ??
        viewers.find((v) => v.action === "editing") ??
        viewers[0];
    const action = ACTION_LABEL[strongest.action ?? "viewing"] ?? "viewing";
    const hot = action === "editing" || action === "replying";
    const label =
        viewers.length === 1
            ? `${strongest.name ?? "A teammate"} is ${action}`
            : `${strongest.name ?? "A teammate"} +${viewers.length - 1} ${action}`;

    return (
        <span
            className={cn(
                "inline-flex items-center gap-1.5 h-5 pl-1 pr-2 rounded-full border text-[10px] font-medium whitespace-nowrap",
                hot
                    ? "border-amber-200 bg-amber-50 text-amber-700"
                    : "border-emerald-200 bg-emerald-50 text-emerald-700",
                className,
            )}
            title={viewers.map((v) => v.name ?? "Teammate").join(", ")}
        >
            <span className="flex -space-x-1">
                {viewers.slice(0, 3).map((v) => (
                    <Avatar key={v.userId} className="size-3.5 ring-1 ring-white">
                        {v.avatar ? <AvatarImage src={v.avatar} alt={v.name ?? ""} /> : null}
                        <AvatarFallback
                            className={cn(
                                "text-[6.5px] font-semibold",
                                hot ? "bg-amber-100 text-amber-700" : "bg-emerald-100 text-emerald-700",
                            )}
                        >
                            {initialsOf(v.name)}
                        </AvatarFallback>
                    </Avatar>
                ))}
            </span>
            {label}
            {hot && (
                <span className="relative flex size-1.5">
                    <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-amber-400 opacity-60" />
                    <span className="relative inline-flex size-1.5 rounded-full bg-amber-500" />
                </span>
            )}
        </span>
    );
}
