// Platform mail transport: what it is, whether it dials, and a way to send a
// real message through it.
//
// Platform mail is not a backing service that merely degrades. Login codes,
// password resets and team invitations all go through it, so a relay that
// cannot authenticate locks everyone out, and the only symptom used to be a
// generic 500 on the login screen. Mattermost, Gitea, Vaultwarden and Zulip all
// ship an equivalent, and it is consistently their most deflected support
// ticket.

import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { CheckCircle2, Mail, RefreshCw, Send, XCircle } from "lucide-react";
import toast from "react-hot-toast";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { getMailStatus, sendTestEmail } from "@/lib/api/client/admin/system";
import { APIError } from "@/lib/api/client";

export function MailStatusCard() {
    const [to, setTo] = useState("");

    const statusQ = useQuery({
        queryKey: ["admin", "mail", "status"],
        queryFn: getMailStatus,
        retry: false,
    });

    const testM = useMutation({
        mutationFn: (address: string) => sendTestEmail(address),
        onSuccess: (result) => {
            if (!result.sent) {
                toast.error(result.error || "The send failed.");
                return;
            }
            toast.success(result.note || `Sent through the ${result.transport} transport.`);
        },
        onError: (err) => toast.error(err instanceof APIError ? err.message : "The send failed."),
    });

    const status = statusQ.data;

    return (
        <Card className={status && !status.healthy ? "border-red-200 bg-red-50/60" : ""}>
            <CardHeader className="flex flex-row items-center justify-between">
                <CardTitle className="flex items-center gap-2 text-[13px]">
                    <Mail className="size-4" />
                    Platform mail
                </CardTitle>
                <div className="flex items-center gap-2">
                    {status && (
                        <Badge variant={status.healthy ? "outline" : "destructive"}>
                            {status.healthy ? "Reachable" : "Failing"}
                        </Badge>
                    )}
                    <Button
                        size="sm"
                        variant="outline"
                        onClick={() => statusQ.refetch()}
                        disabled={statusQ.isFetching}
                    >
                        <RefreshCw className={`size-4 ${statusQ.isFetching ? "animate-spin" : ""}`} />
                    </Button>
                </div>
            </CardHeader>

            <CardContent className="space-y-3">
                {statusQ.isLoading && <Skeleton className="h-16 w-full" />}

                {status && (
                    <>
                        <div className="flex items-start gap-2 text-[13px]">
                            {status.healthy ? (
                                <CheckCircle2 className="mt-0.5 size-4 shrink-0 text-emerald-600" />
                            ) : (
                                <XCircle className="mt-0.5 size-4 shrink-0 text-red-600" />
                            )}
                            <span className="text-muted-foreground">{status.detail}</span>
                        </div>

                        {status.error && (
                            <pre className="overflow-x-auto rounded-md border border-red-200 bg-white p-2 text-[11px] leading-relaxed text-red-700">
                                {status.error}
                            </pre>
                        )}

                        {!status.delivers && (
                            <p className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-[12px] leading-relaxed text-amber-800">
                                Nothing is being delivered. Every platform email, including login
                                codes, is written to the backend logs. Set MAIL_TRANSPORT=smtp and
                                the SMTP_ variables before anyone else relies on this instance.
                            </p>
                        )}

                        <div className="flex items-center gap-2 pt-1">
                            <Input
                                type="email"
                                value={to}
                                onChange={(e) => setTo(e.target.value)}
                                placeholder="you@example.com"
                                className="h-9 text-[13px]"
                            />
                            <Button
                                size="sm"
                                onClick={() => testM.mutate(to)}
                                disabled={!to || testM.isPending}
                            >
                                <Send className="size-4" />
                                {testM.isPending ? "Sending..." : "Send test"}
                            </Button>
                        </div>
                    </>
                )}
            </CardContent>
        </Card>
    );
}
