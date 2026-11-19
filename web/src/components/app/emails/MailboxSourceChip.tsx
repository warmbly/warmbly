// How a mailbox is connected, as one compact chip: bought in the store, the inbox
// vendor it came from, the admin grant it signs in through, or else its mail host.
import { mailboxSource, type MailboxSourceBox } from "@/lib/mailboxSource";
import { cn } from "@/lib/utils";
import ProviderLogo from "./ProviderLogo";

export default function MailboxSourceChip({
    box,
    labelClassName = "hidden md:inline",
    className,
}: {
    box: MailboxSourceBox;
    /** Where the text shows; the logo always does. Defaults to md and up. */
    labelClassName?: string;
    className?: string;
}) {
    const s = mailboxSource(box);
    if (!s.label && !s.logo) return null;
    const tone =
        s.kind === "host"
            ? "border-slate-200 bg-white text-slate-500"
            : s.kind === "purchased"
              ? "border-sky-200 bg-sky-50 text-sky-700"
              : "border-slate-200 bg-slate-50 text-slate-700";
    return (
        <span
            title={s.title}
            className={cn("inline-flex items-center h-[18px] px-0.5 rounded-full border shrink-0 min-w-0 max-w-[200px]", tone, className)}
        >
            <ProviderLogo id={s.logo} size="xs" framed={false} title="" />
            {s.label && (
                <span className={cn("truncate text-[10.5px] font-medium pl-0.5 pr-1", labelClassName)}>{s.label}</span>
            )}
        </span>
    );
}
