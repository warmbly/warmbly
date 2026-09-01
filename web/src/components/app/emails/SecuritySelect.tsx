// Segmented picker for a mailbox leg's connection security. Shared by the
// connect modal and the reconnect dialog so both describe the choice the same
// way. Theme primitives only: h-7 control, slate border, sky active state.

import { cn } from "@/lib/utils";
import type { MailSecurity } from "@/lib/api/models/app/emails/Service";

const OPTIONS: { value: MailSecurity; label: string; hint: string }[] = [
    { value: "tls", label: "SSL / TLS", hint: "Encrypted from the first byte (SMTP 465, IMAP 993)" },
    { value: "starttls", label: "STARTTLS", hint: "Upgrades after connecting (SMTP 587 or 2525, IMAP 143)" },
];

export default function SecuritySelect({
    value,
    onChange,
}: {
    value: MailSecurity;
    onChange: (v: MailSecurity) => void;
}) {
    return (
        <div className="flex items-stretch h-7 rounded-md border border-slate-200 bg-white overflow-hidden">
            {OPTIONS.map((o, i) => (
                <button
                    key={o.value}
                    type="button"
                    title={o.hint}
                    aria-pressed={value === o.value}
                    onClick={() => onChange(o.value)}
                    className={cn(
                        "flex-1 min-w-0 px-2 text-[12.5px] transition-colors",
                        i > 0 && "border-l border-slate-200",
                        value === o.value
                            ? "bg-sky-50 text-sky-700 font-medium"
                            : "text-slate-600 hover:bg-slate-50",
                    )}
                >
                    {o.label}
                </button>
            ))}
        </div>
    );
}
