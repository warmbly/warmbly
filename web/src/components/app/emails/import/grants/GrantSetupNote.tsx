// What an operator sets to turn on admin grants, named exactly, for the
// connect picker's switched-off rows and the grant wizard's notice.
import { ExternalLinkIcon } from "lucide-react";
import type { GrantConfig, GrantProvider } from "@/lib/api/models/app/emails/MailboxSources";
import { MS_GRANT_PERMISSIONS, grantMissing } from "./grantSetup";
import { cn } from "@/lib/utils";
import { ADMIN_CONNECTIONS_DOCS } from "../importFields";

import HelpLink from "@/components/shared/HelpLink";
function joinAnd(items: string[]): string {
    if (items.length <= 1) return items.join("");
    return `${items.slice(0, -1).join(", ")} and ${items[items.length - 1]}`;
}

export function EnvChip({ name, tone = "slate" }: { name: string; tone?: "slate" | "amber" }) {
    return (
        <code
            className={cn(
                "inline-flex items-center h-[18px] px-1.5 rounded font-mono text-[10.5px] whitespace-nowrap",
                tone === "amber" ? "bg-white/70 text-amber-900" : "bg-slate-100 text-slate-700",
            )}
        >
            {name}
        </code>
    );
}

/** The setup sentence for a provider: what it needs, which settings, and the docs link. */
export default function GrantSetupNote({
    provider,
    config,
    tone = "slate",
    className,
}: {
    provider: GrantProvider;
    config: GrantConfig | undefined;
    tone?: "slate" | "amber";
    className?: string;
}) {
    const missing = grantMissing(config, provider);
    const chips = (
        <span className="inline-flex flex-wrap gap-1 align-middle">
            {missing.map((m) => (
                <EnvChip key={m} name={m} tone={tone} />
            ))}
        </span>
    );
    return (
        <div className={cn("space-y-1 leading-relaxed", className)}>
            {provider === "google" ? (
                <p>
                    Needs a Google service account with domain-wide delegation, which is separate from the Google sign-in app. Set{" "}
                    {chips}.
                </p>
            ) : (
                <>
                    <p>
                        Uses this instance&apos;s Outlook sign-in app. Set {chips}.
                    </p>
                    <p>
                        The Azure app also needs the Application permissions {joinAnd(MS_GRANT_PERMISSIONS)}, with admin consent.
                    </p>
                </>
            )}
            <HelpLink
                href={ADMIN_CONNECTIONS_DOCS}
                portalLabel={<>Contact support <ExternalLinkIcon className="w-2.5 h-2.5" /></>}
                onClick={(e) => e.stopPropagation()}
                className={cn(
                    "inline-flex items-center gap-0.5 underline",
                    tone === "amber" ? "font-medium" : "text-sky-700 decoration-sky-300 hover:decoration-sky-600",
                )}
            >
                Configuration docs
                <ExternalLinkIcon className="w-2.5 h-2.5" />
            </HelpLink>
        </div>
    );
}
