import { useState } from "react";
import { AnimatePresence, motion } from "framer-motion";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { useOnlineMembers, type PresenceUser } from "@/hooks/PresenceProvider";
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

function activityLabel(user: PresenceUser) {
    if (user.action === "replying") return "replying to an email";
    if (user.action === "editing") return "editing";
    if (user.page) {
        const section = user.page.split("/").filter(Boolean)[1];
        if (section) return `in ${section.replace(/-/g, " ")}`;
    }
    return "online";
}

// Online-teammates stack for the app header. Hover shows who is where; the
// green ring marks live sockets, mirroring the Discord-like presence feel.
export default function PresenceAvatars() {
    const members = useOnlineMembers();
    const [open, setOpen] = useState(false);

    if (members.length === 0) return null;

    const visible = members.slice(0, MAX_VISIBLE);
    const overflow = members.length - visible.length;

    return (
        <div
            className="relative hidden sm:block"
            onMouseEnter={() => setOpen(true)}
            onMouseLeave={() => setOpen(false)}
        >
            <button
                type="button"
                aria-label={`${members.length} teammate${members.length === 1 ? "" : "s"} online`}
                className="flex items-center -space-x-1.5 focus:outline-none"
                onClick={() => setOpen((v) => !v)}
            >
                {visible.map((m) => (
                    <Avatar
                        key={m.userId}
                        size="sm"
                        className="ring-2 ring-white border border-emerald-300/70"
                    >
                        {m.avatar ? <AvatarImage src={m.avatar} alt={m.name ?? ""} /> : null}
                        <AvatarFallback className="bg-emerald-50 text-emerald-700 text-[9.5px] font-semibold">
                            {initialsOf(m.name)}
                        </AvatarFallback>
                    </Avatar>
                ))}
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
                        className="absolute right-0 top-full mt-2 w-60 rounded-md border border-slate-200 bg-white shadow-lg z-50 py-1.5"
                    >
                        <div className="px-3 pb-1.5 pt-0.5 text-[10px] uppercase tracking-[0.14em] text-slate-400 flex items-center gap-1.5">
                            <span className="relative flex size-1.5">
                                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-60" />
                                <span className="relative inline-flex size-1.5 rounded-full bg-emerald-500" />
                            </span>
                            Online now
                        </div>
                        {members.map((m) => (
                            <div key={m.userId} className="px-3 py-1.5 flex items-center gap-2.5">
                                <Avatar size="sm">
                                    {m.avatar ? <AvatarImage src={m.avatar} alt={m.name ?? ""} /> : null}
                                    <AvatarFallback className="bg-sky-50 text-sky-700 text-[9.5px] font-semibold">
                                        {initialsOf(m.name)}
                                    </AvatarFallback>
                                </Avatar>
                                <div className="min-w-0">
                                    <div className="text-[12.5px] text-slate-900 font-medium truncate">
                                        {m.name ?? "Teammate"}
                                    </div>
                                    <div
                                        className={cn(
                                            "text-[10.5px] truncate capitalize",
                                            m.action === "replying" || m.action === "editing"
                                                ? "text-amber-600"
                                                : "text-slate-400",
                                        )}
                                    >
                                        {activityLabel(m)}
                                    </div>
                                </div>
                            </div>
                        ))}
                    </motion.div>
                )}
            </AnimatePresence>
        </div>
    );
}
