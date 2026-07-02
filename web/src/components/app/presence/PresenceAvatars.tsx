import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { ArrowRightIcon, EyeIcon, PencilIcon, ReplyIcon } from "lucide-react";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { useOnlineMembers, type PresenceUser } from "@/hooks/PresenceProvider";
import useClickOutside from "@/hooks/useClickOutside";
import { cn } from "@/lib/utils";

const MAX_VISIBLE = 4;

function initialsOf(name: string | null) {
    if (!name) return "?";
    return name
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 2)
        .map((part) => part[0]?.toUpperCase())
        .join("");
}

// Name the thing a teammate is focused on from its resource key, e.g.
// "automation:<id>" -> "an automation". Keeps the dropdown specific ("Editing
// an automation") instead of a bare "editing".
function resourceNoun(resource: string | null): string {
    const kind = resource?.split(":")[0];
    switch (kind) {
        case "automation":
            return "an automation";
        case "campaign":
            return "a campaign";
        case "contact":
            return "a contact";
        case "thread":
            return "a conversation";
        default:
            return "";
    }
}

type Tone = "hot" | "cool" | "idle";

function activityOf(user: PresenceUser): {
    label: string;
    tone: Tone;
    Icon: typeof EyeIcon | null;
} {
    if (user.action === "replying") return { label: "Replying to a message", tone: "hot", Icon: ReplyIcon };
    if (user.action === "editing") {
        const noun = resourceNoun(user.resource);
        return { label: noun ? `Editing ${noun}` : "Editing", tone: "hot", Icon: PencilIcon };
    }
    if (user.action === "viewing" && user.resource) {
        const noun = resourceNoun(user.resource);
        return { label: noun ? `Viewing ${noun}` : "Viewing", tone: "cool", Icon: EyeIcon };
    }
    if (user.page) {
        const section = user.page.split("/").filter(Boolean)[1];
        if (section) return { label: `In ${section.replace(/-/g, " ")}`, tone: "idle", Icon: null };
    }
    return { label: "Online", tone: "idle", Icon: null };
}

const TONE_TEXT: Record<Tone, string> = {
    hot: "text-amber-600",
    cool: "text-sky-600",
    idle: "text-slate-400",
};

// Online-teammates stack for the app header. Click to pin a dropdown listing
// everyone online and exactly what they're doing (editing / viewing / where),
// with profile pictures and a +N overflow — a Discord-like presence surface.
export default function PresenceAvatars() {
    const members = useOnlineMembers();
    const [open, setOpen] = useState(false);
    const ref = useRef<HTMLDivElement>(null);
    const navigate = useNavigate();
    useClickOutside(ref, () => setOpen(false));

    if (members.length === 0) return null;

    const visible = members.slice(0, MAX_VISIBLE);
    const overflow = members.length - visible.length;
    const editingCount = members.filter((m) => m.action === "editing" || m.action === "replying").length;

    return (
        <div ref={ref} className="relative hidden sm:block">
            <button
                type="button"
                aria-label={`${members.length} teammate${members.length === 1 ? "" : "s"} online`}
                className="flex items-center -space-x-1.5 rounded-full focus:outline-none focus-visible:ring-2 focus-visible:ring-sky-200"
                onClick={() => setOpen((v) => !v)}
            >
                {visible.map((m) => {
                    const hot = m.action === "editing" || m.action === "replying";
                    return (
                        <Avatar
                            key={m.userId}
                            size="sm"
                            className={cn(
                                "ring-2 ring-white border",
                                hot ? "border-amber-300" : "border-emerald-300/70",
                            )}
                        >
                            {m.avatar ? <AvatarImage src={m.avatar} alt={m.name ?? ""} /> : null}
                            <AvatarFallback
                                className={cn(
                                    "text-[9.5px] font-semibold",
                                    hot ? "bg-amber-50 text-amber-700" : "bg-emerald-50 text-emerald-700",
                                )}
                            >
                                {initialsOf(m.name)}
                            </AvatarFallback>
                        </Avatar>
                    );
                })}
                {overflow > 0 && (
                    <span className="relative z-10 inline-flex size-6 items-center justify-center rounded-full bg-slate-100 ring-2 ring-white text-[9.5px] font-semibold text-slate-500">
                        +{overflow}
                    </span>
                )}
            </button>

            <AnimatePresence>
                {open && (
                    <motion.div
                        initial={{ opacity: 0, y: 4, scale: 0.98 }}
                        animate={{ opacity: 1, y: 0, scale: 1 }}
                        exit={{ opacity: 0, y: 4, scale: 0.98 }}
                        transition={{ duration: 0.12 }}
                        className="absolute right-0 top-full mt-2 w-[260px] max-h-[60vh] overflow-y-auto rounded-md border border-slate-200 bg-white shadow-lg z-50 py-1.5"
                    >
                        <div className="px-3 pb-1.5 pt-0.5 flex items-center gap-1.5">
                            <span className="relative flex size-1.5">
                                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />
                                <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
                            </span>
                            <span className="text-[10px] uppercase tracking-[0.14em] text-slate-400">
                                {members.length} online
                            </span>
                            {editingCount > 0 && (
                                <span className="ml-auto text-[10px] font-medium text-amber-600">
                                    {editingCount} editing
                                </span>
                            )}
                        </div>
                        {members.map((m) => {
                            const act = activityOf(m);
                            // Click a teammate to jump to the page they are on.
                            const jumpable = !!m.page;
                            return (
                                <button
                                    key={m.userId}
                                    type="button"
                                    disabled={!jumpable}
                                    title={jumpable ? "Go where they are" : undefined}
                                    onClick={() => {
                                        if (!m.page) return;
                                        setOpen(false);
                                        navigate(m.page);
                                    }}
                                    className={cn(
                                        "group w-full px-3 py-1.5 flex items-center gap-2.5 text-left",
                                        jumpable ? "hover:bg-slate-50" : "cursor-default",
                                    )}
                                >
                                    <Avatar size="sm">
                                        {m.avatar ? <AvatarImage src={m.avatar} alt={m.name ?? ""} /> : null}
                                        <AvatarFallback className="bg-sky-50 text-sky-700 text-[9.5px] font-semibold">
                                            {initialsOf(m.name)}
                                        </AvatarFallback>
                                    </Avatar>
                                    <div className="min-w-0 flex-1">
                                        <div className="text-[12.5px] text-slate-900 font-medium truncate">
                                            {m.name ?? "Teammate"}
                                        </div>
                                        <div className={cn("text-[10.5px] truncate flex items-center gap-1", TONE_TEXT[act.tone])}>
                                            {act.Icon && <act.Icon className="w-3 h-3 shrink-0" />}
                                            {act.label}
                                        </div>
                                    </div>
                                    {jumpable && (
                                        <ArrowRightIcon className="w-3 h-3 shrink-0 text-slate-300 opacity-0 transition-opacity group-hover:opacity-100" />
                                    )}
                                </button>
                            );
                        })}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
