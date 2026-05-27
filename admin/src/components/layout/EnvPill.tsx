import { cn } from "@/lib/utils";
import { ENV_LABEL, type EnvLabel } from "@/lib/env";

// Small environment indicator — sits in the topbar so you can tell a
// production admin window from a staging one at a glance. Colour is
// deliberately distinct from --admin-accent (which is environment-
// agnostic) so PROD doesn't blend into the rest of the chrome.

const STYLES: Record<EnvLabel, string> = {
    production: "bg-red-50 text-red-700 border-red-200",
    staging: "bg-amber-50 text-amber-700 border-amber-200",
    development: "bg-emerald-50 text-emerald-700 border-emerald-200",
};

const LABELS: Record<EnvLabel, string> = {
    production: "Production",
    staging: "Staging",
    development: "Development",
};

export function EnvPill({ className }: { className?: string }) {
    return (
        <span
            className={cn(
                "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] font-medium",
                STYLES[ENV_LABEL],
                className,
            )}
            title={`Connected to ${LABELS[ENV_LABEL]} environment`}
        >
            <span className="size-1.5 rounded-full bg-current opacity-80" />
            {LABELS[ENV_LABEL]}
        </span>
    );
}
