// The warning a signature editor shows for tokens a signature cannot fill, such as a contact field.
import { AlertTriangleIcon } from "lucide-react";

export default function UnknownPlaceholders({ unknown }: { unknown: string[] }) {
    if (unknown.length === 0) return null;
    return (
        <p className="flex items-start gap-1.5 text-[11px] text-amber-700 leading-relaxed">
            <AlertTriangleIcon className="w-3 h-3 mt-0.5 shrink-0" />
            <span>
                {unknown.join(", ")} {unknown.length === 1 ? "is" : "are"} not filled in a signature and {unknown.length === 1 ? "is" : "are"} sent exactly as
                written. For the mailbox&apos;s own name, use <code className="font-mono">{"{{.Sender.FirstName}}"}</code>.
            </span>
        </p>
    );
}
