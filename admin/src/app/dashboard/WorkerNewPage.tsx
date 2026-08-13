// Add a worker.
//
// Two ways to attach a machine, both ending at the same place:
//
//   SSH-managed  — we mint a keypair, you paste the public key into the VPS,
//                  then Test and Install run from here.
//   Enrollment   — we mint a one-time token, you run one curl on the VPS and
//                  it configures itself. No inbound SSH needed.
//
// Only shared workers can be created: the backend rejects worker_type
// "dedicated" because dedicated capacity is allocated by the control plane.

import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { ArrowLeft, CheckCircle2, Hammer, PlayCircle, Plug, XCircle } from "lucide-react";
import { PageHeader } from "@/components/layout/PageHeader";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { API_URL } from "@/lib/env";
import {
    createWorker,
    installWorker,
    preflightWorker,
    testWorker,
} from "@/lib/api/client/admin/workers";
import type { CreateWorkerResponse } from "@/lib/api/models/admin";

type Method = "ssh" | "enroll";

export default function WorkerNewPage() {
    const navigate = useNavigate();

    const [method, setMethod] = useState<Method>("ssh");
    const [name, setName] = useState("");
    const [notes, setNotes] = useState("");
    const [freeTier, setFreeTier] = useState(false);
    const [host, setHost] = useState("");
    const [port, setPort] = useState(22);
    const [user, setUser] = useState("root");

    const [preflight, setPreflight] = useState<{
        ok: boolean;
        latency_ms?: number;
        error?: string;
    } | null>(null);
    const [created, setCreated] = useState<CreateWorkerResponse | null>(null);

    const preflightMut = useMutation({
        mutationFn: () => preflightWorker(host, port),
        onSuccess: (res) => setPreflight(res),
        onError: (e: Error) => setPreflight({ ok: false, error: e.message }),
    });

    const createMut = useMutation({
        mutationFn: () =>
            createWorker({
                name,
                notes,
                worker_type: "shared",
                free_tier: freeTier,
                ssh_host: host,
                ssh_port: port,
                ssh_user: user,
                generate_enrollment_token: method === "enroll",
            }),
        onSuccess: (res) => {
            setCreated(res);
            toast.success("Worker created");
        },
        onError: (e: Error) => toast.error(e.message),
    });

    const testMut = useMutation({
        mutationFn: () => testWorker(created!.id),
        onSuccess: (res) =>
            res.ok
                ? toast.success("SSH reachable")
                : toast.error(res.error || "SSH unreachable"),
        onError: (e: Error) => toast.error(e.message),
    });

    const installMut = useMutation({
        mutationFn: () => installWorker(created!.id),
        onSuccess: () => toast.success("Install kicked off"),
        onError: (e: Error) => toast.error(e.message),
    });

    // The enrollment path does not need a reachable host up front, so only the
    // SSH path gates on name + host.
    const canCreate = name.trim() !== "" && host.trim() !== "";

    const enrollCommand = created?.enrollment_token
        ? `curl -fsSL ${API_URL}/worker-install.sh | sudo bash -s -- \\\n  --enroll ${created.enrollment_token} --api-base ${API_URL}`
        : "";

    const copy = (text: string, what: string) => {
        navigator.clipboard.writeText(text);
        toast.success(`${what} copied`);
    };

    if (created) {
        return (
            <div>
                <Link
                    to="/workers"
                    className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
                >
                    <ArrowLeft className="size-3" />
                    All workers
                </Link>
                <PageHeader
                    title={created.name}
                    description="Worker created. Finish the handshake on the machine, then install."
                />

                {created.enrollment_token ? (
                    <Card className="mb-4">
                        <CardHeader>
                            <CardTitle>Run this on the VPS</CardTitle>
                            <CardDescription>
                                One-time token, valid for{" "}
                                {Math.round((created.enrollment_token_ttl_seconds ?? 7200) / 3600)}{" "}
                                hours. It is shown once and cannot be retrieved later.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="pt-0">
                            <pre className="overflow-x-auto rounded bg-muted p-3 text-xs">
                                {enrollCommand}
                            </pre>
                            <Button
                                size="sm"
                                variant="outline"
                                className="mt-2"
                                onClick={() => copy(enrollCommand, "Command")}
                            >
                                Copy command
                            </Button>
                        </CardContent>
                    </Card>
                ) : (
                    <Card className="mb-4">
                        <CardHeader>
                            <CardTitle>Add this key to the VPS</CardTitle>
                            <CardDescription>
                                Append it to <code>~/.ssh/authorized_keys</code> for{" "}
                                <code>{created.ssh_user || user}</code>, then Test connection. The
                                first success pins the host fingerprint.
                            </CardDescription>
                        </CardHeader>
                        <CardContent className="pt-0">
                            <pre className="overflow-x-auto rounded bg-muted p-3 text-xs">
                                {created.ssh_public_key}
                            </pre>
                            <Button
                                size="sm"
                                variant="outline"
                                className="mt-2"
                                onClick={() => copy(created.ssh_public_key, "Public key")}
                            >
                                Copy public key
                            </Button>
                        </CardContent>
                    </Card>
                )}

                <div className="flex flex-wrap gap-2">
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => testMut.mutate()}
                        disabled={testMut.isPending}
                    >
                        <PlayCircle className="size-4" />
                        {testMut.isPending ? "Testing…" : "Test connection"}
                    </Button>
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => installMut.mutate()}
                        disabled={installMut.isPending}
                    >
                        <Hammer className="size-4" />
                        {installMut.isPending ? "Installing…" : "Install"}
                    </Button>
                    <Button size="sm" onClick={() => navigate(`/workers/${created.id}`)}>
                        Open worker
                    </Button>
                </div>
            </div>
        );
    }

    return (
        <div>
            <Link
                to="/workers"
                className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground mb-2"
            >
                <ArrowLeft className="size-3" />
                All workers
            </Link>
            <PageHeader
                title="Add worker"
                description="Attach a Linux machine you own. Outbound mail still leaves through each mailbox's own provider, so more workers means more parallelism, not more sending IPs."
            />

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
                <Card>
                    <CardHeader>
                        <CardTitle>How should we reach it?</CardTitle>
                        <CardDescription>
                            Both paths end with the same worker. Pick enrollment if the machine
                            cannot accept inbound SSH from here.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0 space-y-2">
                        <MethodOption
                            selected={method === "ssh"}
                            onSelect={() => setMethod("ssh")}
                            title="SSH-managed"
                            body="We generate a keypair. You paste the public key onto the machine, and every later action (install, restart, logs, reboot) runs from this panel."
                        />
                        <MethodOption
                            selected={method === "enroll"}
                            onSelect={() => setMethod("enroll")}
                            title="Enrollment token"
                            body="We generate a one-time token. You run a single curl on the machine and it configures itself and starts reporting in."
                        />
                    </CardContent>
                </Card>

                <Card>
                    <CardHeader>
                        <CardTitle>Machine</CardTitle>
                        <CardDescription>
                            The host is stored either way; it identifies the worker and is what SSH
                            actions dial.
                        </CardDescription>
                    </CardHeader>
                    <CardContent className="pt-0 space-y-3">
                        <div>
                            <Label htmlFor="w-name">Name</Label>
                            <Input
                                id="w-name"
                                value={name}
                                onChange={(e) => setName(e.target.value)}
                                placeholder="eu-west-1"
                            />
                        </div>
                        <div className="grid grid-cols-3 gap-2">
                            <div className="col-span-2">
                                <Label htmlFor="w-host">SSH host</Label>
                                <Input
                                    id="w-host"
                                    value={host}
                                    onChange={(e) => {
                                        setHost(e.target.value);
                                        setPreflight(null);
                                    }}
                                    placeholder="203.0.113.10"
                                />
                            </div>
                            <div>
                                <Label htmlFor="w-port">Port</Label>
                                <Input
                                    id="w-port"
                                    type="number"
                                    value={port}
                                    onChange={(e) => {
                                        setPort(Number(e.target.value) || 22);
                                        setPreflight(null);
                                    }}
                                />
                            </div>
                        </div>
                        <div>
                            <Label htmlFor="w-user">SSH user</Label>
                            <Input
                                id="w-user"
                                value={user}
                                onChange={(e) => setUser(e.target.value)}
                                placeholder="root"
                            />
                        </div>
                        <div>
                            <Label htmlFor="w-notes">Notes</Label>
                            <Textarea
                                id="w-notes"
                                value={notes}
                                onChange={(e) => setNotes(e.target.value)}
                                placeholder="Provider, region, anything worth remembering."
                                rows={2}
                            />
                        </div>
                        <div className="flex items-center justify-between rounded-md border p-3">
                            <div className="min-w-0 pr-3">
                                <p className="text-sm font-medium">Free-tier worker</p>
                                <p className="text-xs text-muted-foreground">
                                    Free-trial organizations place onto free-tier workers; paid
                                    organizations place onto the rest.
                                </p>
                            </div>
                            <Switch checked={freeTier} onCheckedChange={setFreeTier} />
                        </div>

                        <div className="flex items-center gap-2">
                            <Button
                                size="sm"
                                variant="outline"
                                onClick={() => preflightMut.mutate()}
                                disabled={!host.trim() || preflightMut.isPending}
                            >
                                <Plug className="size-4" />
                                {preflightMut.isPending ? "Checking…" : "Check reachability"}
                            </Button>
                            {preflight && (
                                <span
                                    className={`inline-flex items-center gap-1 text-xs ${preflight.ok ? "text-emerald-600" : "text-red-600"}`}
                                >
                                    {preflight.ok ? (
                                        <CheckCircle2 className="size-3.5" />
                                    ) : (
                                        <XCircle className="size-3.5" />
                                    )}
                                    {preflight.ok
                                        ? `Port open${preflight.latency_ms ? ` · ${preflight.latency_ms}ms` : ""}`
                                        : preflight.error || "Unreachable"}
                                </span>
                            )}
                        </div>
                    </CardContent>
                </Card>
            </div>

            <div className="mt-4 flex gap-2">
                <Button onClick={() => createMut.mutate()} disabled={!canCreate || createMut.isPending}>
                    {createMut.isPending ? "Creating…" : "Create worker"}
                </Button>
                <Button variant="outline" onClick={() => navigate("/workers")}>
                    Cancel
                </Button>
            </div>
        </div>
    );
}

function MethodOption({
    selected,
    onSelect,
    title,
    body,
}: {
    selected: boolean;
    onSelect: () => void;
    title: string;
    body: string;
}) {
    return (
        <button
            type="button"
            onClick={onSelect}
            className={`w-full rounded-md border p-3 text-left transition ${
                selected ? "border-primary bg-primary/5" : "hover:bg-muted/50"
            }`}
        >
            <p className="text-sm font-medium">{title}</p>
            <p className="mt-1 text-xs text-muted-foreground">{body}</p>
        </button>
    );
}
