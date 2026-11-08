// Where a report's embed code is shown once embeds are enabled.

import type ClientReport from "@/lib/api/models/app/reports/ClientReport";

export default function ReportEmbedSnippet({ report }: { report: ClientReport }) {
    return (
        <div data-report-id={report.id} className="rounded-md border border-dashed border-slate-200 bg-slate-50/50 px-3 py-2.5">
            <p className="text-[11.5px] text-slate-500">Embed code appears here once embeds are enabled.</p>
        </div>
    );
}
