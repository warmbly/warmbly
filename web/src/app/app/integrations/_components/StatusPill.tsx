import { cn } from "@/lib/utils";
import type { IntegrationHealth, IntegrationStatus } from "@/lib/api/models/app/integrations/Integration";

const TONES: Record<string, { bg: string; text: string; dot: string; label: string }> = {
    connected: { bg: "bg-emerald-50", text: "text-emerald-700", dot: "bg-emerald-500", label: "connected" },
    authorizing: { bg: "bg-sky-50", text: "text-sky-700", dot: "bg-sky-500", label: "authorizing" },
    pending: { bg: "bg-sky-50", text: "text-sky-700", dot: "bg-sky-500", label: "pending" },
    degraded: { bg: "bg-amber-50", text: "text-amber-700", dot: "bg-amber-500", label: "degraded" },
    reauth_required: { bg: "bg-amber-50", text: "text-amber-700", dot: "bg-amber-500", label: "reconnect" },
    disconnected: { bg: "bg-slate-100", text: "text-slate-500", dot: "bg-slate-400", label: "not connected" },
};

export default function StatusPill({ status }: { status: IntegrationStatus | string }) {
    const tone = TONES[status] ?? TONES.disconnected;
    return (
        <span
            className={cn(
                "inline-flex items-center gap-1 h-5 px-1.5 rounded text-[9.5px] uppercase tracking-[0.08em] font-medium",
                tone.bg,
                tone.text,
            )}
        >
            <span className={cn("size-1.5 rounded-full", tone.dot)} />
            {tone.label}
        </span>
    );
}

const HEALTH_DOT: Record<string, string> = {
    healthy: "bg-emerald-500",
    degraded: "bg-amber-500",
    down: "bg-rose-500",
    unknown: "bg-slate-300",
};

export function HealthDot({ health }: { health: IntegrationHealth | string }) {
    return <span className={cn("size-1.5 rounded-full", HEALTH_DOT[health] ?? HEALTH_DOT.unknown)} />;
}
