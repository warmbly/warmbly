// Import an archive into a workspace: choose the file, run preflight (reads
// the manifest and reports conflicts without writing), then apply. Multipart
// field names match the dashboard's Settings > Data flow: "file", "options"
// (JSON), "passphrase".

import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { AlertTriangle, FileArchive, Loader2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from "@/components/ui/dialog";
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from "@/components/ui/select";
import { useConfirm } from "@/components/ConfirmDialog";
import {
    createOrgImport,
    expandGroups,
    formatBytes,
    preflightOrgImport,
    totalRows,
    type OrgDataGroup,
    type OrgImportConflict,
    type OrgImportPreflight,
} from "@/lib/api/client/admin/transfers";
import { GroupPicker } from "./GroupPicker";
import { fmtDateTime } from "../fleet/format";

export function ImportArchiveDialog({
    open,
    onOpenChange,
    orgId,
    orgName,
    onStarted,
}: {
    open: boolean;
    onOpenChange: (v: boolean) => void;
    orgId: string;
    orgName: string;
    onStarted?: () => void;
}) {
    const qc = useQueryClient();
    const confirm = useConfirm();
    const fileRef = useRef<HTMLInputElement>(null);
    const [file, setFile] = useState<File | null>(null);
    const [passphrase, setPassphrase] = useState("");
    const [report, setReport] = useState<OrgImportPreflight | null>(null);
    const [groups, setGroups] = useState<Set<OrgDataGroup>>(new Set());
    const [conflict, setConflict] = useState<OrgImportConflict>("skip");

    useEffect(() => {
        if (!open) return;
        setFile(null);
        setPassphrase("");
        setReport(null);
        setGroups(new Set());
        setConflict("skip");
    }, [open]);

    function pickFile(next: File | null) {
        setFile(next);
        setReport(null);
    }

    const preflight = useMutation({
        mutationFn: () => preflightOrgImport(orgId, file!, passphrase),
        onSuccess: (r) => {
            setReport(r);
            setGroups(expandGroups(r.archive.groups));
        },
        onError: (e: Error) => {
            setReport(null);
            toast.error(e.message || "That archive could not be read");
        },
    });

    const apply = useMutation({
        mutationFn: () =>
            createOrgImport(orgId, file!, { groups: Array.from(groups), conflict_strategy: conflict }, passphrase),
        onSuccess: () => {
            toast.success(`Import started into ${orgName}`);
            qc.invalidateQueries({ queryKey: ["admin", "transfers"] });
            onStarted?.();
            onOpenChange(false);
        },
        onError: (e: Error) => toast.error(e.message || "Could not start the import"),
    });

    async function onApply() {
        if (!file || !report) return;
        const conflictTotal = Object.values(report.conflicts ?? {}).reduce((a, b) => a + b, 0);
        const ok = await confirm({
            title: `Import "${report.archive.organization_name}" into ${orgName}?`,
            description:
                conflict === "overwrite" && conflictTotal > 0
                    ? `${conflictTotal.toLocaleString()} existing row(s) in this workspace will be replaced with the archive's versions. That cannot be undone.`
                    : `The archive's contents are added to this workspace. Rows that already exist here are kept as they are.`,
            confirmLabel: "Start import",
            destructive: conflict === "overwrite",
        });
        if (ok) apply.mutate();
    }

    const busy = preflight.isPending || apply.isPending;
    const conflictTotal = Object.values(report?.conflicts ?? {}).reduce((a, b) => a + b, 0);

    return (
        <Dialog
            open={open}
            onOpenChange={(v) => {
                if (!v && busy) return;
                onOpenChange(v);
            }}
        >
            <DialogContent className="sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle>Import an archive into {orgName}</DialogTitle>
                    <DialogDescription>
                        Preflight reads the archive and reports what would land, what already exists, and who has no
                        account here, without writing anything.
                    </DialogDescription>
                </DialogHeader>

                <div className="space-y-4">
                    <div className="flex flex-col gap-3 rounded-md border border-dashed border-border px-3 py-3 sm:flex-row sm:items-center">
                        <FileArchive className="size-4 shrink-0 text-muted-foreground" />
                        <div className="min-w-0 flex-1">
                            <div className="text-[12.5px] font-medium leading-tight">{file ? file.name : "Choose an archive"}</div>
                            <div className="text-[11px] leading-tight text-muted-foreground">
                                {file ? formatBytes(file.size) : "A .warmbly.zip exported from this or another instance."}
                            </div>
                        </div>
                        <input
                            ref={fileRef}
                            type="file"
                            accept=".zip,application/zip"
                            className="hidden"
                            onChange={(e) => pickFile(e.target.files?.[0] ?? null)}
                        />
                        <Button size="sm" variant="outline" onClick={() => fileRef.current?.click()} disabled={busy}>
                            {file ? "Choose another" : "Choose file"}
                        </Button>
                    </div>

                    {file && (
                        <div className="grid gap-3 sm:grid-cols-[1fr_auto] sm:items-end">
                            <div className="space-y-1">
                                <Label htmlFor="imp-pass" className="text-xs">
                                    Export passphrase <span className="font-normal text-muted-foreground">(only if the archive carries credentials)</span>
                                </Label>
                                <Input
                                    id="imp-pass"
                                    type="password"
                                    autoComplete="current-password"
                                    value={passphrase}
                                    onChange={(e) => {
                                        setPassphrase(e.target.value);
                                        setReport(null);
                                    }}
                                    className="h-8 text-[12.5px]"
                                />
                            </div>
                            <Button size="sm" onClick={() => preflight.mutate()} disabled={busy}>
                                {preflight.isPending && <Loader2 className="size-3.5 animate-spin" />}
                                {report ? "Check again" : "Check archive"}
                            </Button>
                        </div>
                    )}

                    {report && (
                        <div className="space-y-3">
                            <div className="rounded-md border border-border bg-card p-3 text-[12.5px]">
                                <div className="grid gap-x-4 gap-y-1 sm:grid-cols-2">
                                    <Row k="Source workspace" v={report.archive.organization_name} />
                                    <Row k="Exported" v={fmtDateTime(report.archive.exported_at)} />
                                    <Row k="Source instance" v={report.archive.source_instance || "—"} />
                                    <Row k="App version" v={report.archive.source_app_version || "—"} />
                                    <Row k="Rows" v={totalRows(report.archive.row_counts).toLocaleString()} />
                                    <Row k="Blobs" v={String(report.archive.blob_count)} />
                                    <Row
                                        k="Credentials"
                                        v={
                                            !report.archive.has_secrets
                                                ? "not in archive; mailboxes will need reconnecting"
                                                : report.secrets_unsealed
                                                  ? "unsealed; mailboxes reconnect automatically"
                                                  : "sealed; wrong or missing passphrase"
                                        }
                                    />
                                    <Row
                                        k="Conflicts"
                                        v={conflictTotal === 0 ? "none" : `${conflictTotal.toLocaleString()} existing row(s)`}
                                    />
                                </div>
                                {(report.unknown_members?.length ?? 0) > 0 && (
                                    <p className="mt-2 text-[11px] text-muted-foreground">
                                        {report.unknown_members!.length} member(s) have no account on this instance and arrive as
                                        pending invitations: {report.unknown_members!.map((m) => m.email).join(", ")}.
                                    </p>
                                )}
                                {(report.skipped_tables?.length ?? 0) > 0 && (
                                    <p className="mt-2 text-[11px] text-muted-foreground">
                                        Skipped (unknown here): {report.skipped_tables!.join(", ")}.
                                    </p>
                                )}
                                {(report.warnings?.length ?? 0) > 0 && (
                                    <ul className="mt-2 space-y-1">
                                        {report.warnings!.map((w, i) => (
                                            <li key={i} className="flex items-start gap-1.5 text-[11px] text-amber-800">
                                                <AlertTriangle className="mt-0.5 size-3 shrink-0" />
                                                {w}
                                            </li>
                                        ))}
                                    </ul>
                                )}
                            </div>

                            <div className="space-y-1.5">
                                <Label className="text-xs">Groups to apply</Label>
                                <GroupPicker selected={groups} onChange={setGroups} available={report.archive.groups} disabled={busy} />
                            </div>

                            <div className="space-y-1.5">
                                <Label className="text-xs">When a row already exists here</Label>
                                <Select value={conflict} onValueChange={(v) => setConflict(v as OrgImportConflict)}>
                                    <SelectTrigger className="h-8 w-full text-[12.5px] sm:w-80">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value="skip" className="text-[12.5px]">
                                            Skip: keep the row that is already here
                                        </SelectItem>
                                        <SelectItem value="overwrite" className="text-[12.5px]">
                                            Overwrite: replace it with the archive's version
                                        </SelectItem>
                                    </SelectContent>
                                </Select>
                            </div>
                        </div>
                    )}
                </div>

                <DialogFooter>
                    <Button variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
                        Cancel
                    </Button>
                    <Button onClick={() => void onApply()} disabled={!report || busy}>
                        {apply.isPending ? "Starting…" : "Apply import"}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function Row({ k, v }: { k: string; v: string }) {
    return (
        <div className="flex justify-between gap-3">
            <span className="text-muted-foreground">{k}</span>
            <span className="truncate text-right font-medium">{v}</span>
        </div>
    );
}
