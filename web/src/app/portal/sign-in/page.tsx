// /portal/sign-in?host= on the main address: where an agency portal that signs its
// clients in here sends them. Someone already signed in here continues with one
// click; anyone else signs in first, and either way the portal gets a one-time code.

import { useEffect, useState } from "react";
import { useNavigate } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";

import getUser from "@/lib/api/client/auth/getUser";
import getToken from "@/lib/helper/getToken";
import buildError from "@/lib/helper/buildError";
import type { AppError } from "@/lib/api/client/normalizeError";
import { currentPortal, portalHostParam } from "@/lib/portal";
import { goToPortal, usePortalForSignIn } from "@/lib/portalHandoff";
import { useSearchParams } from "@/hooks/useSearchParams";
import { PortalCard, PortalError } from "../PortalCard";

export default function PortalSignInPage() {
    const [params] = useSearchParams();
    const navigate = useNavigate();
    const host = portalHostParam(params.get("host"));
    // This page belongs to the main address; a portal serving it has nothing to hand over.
    const onPortal = currentPortal().portal;
    const lookup = usePortalForSignIn(onPortal ? "" : host);
    const signedIn = !!getToken();
    const me = useQuery({ queryKey: ["auth", "me"], queryFn: getUser, enabled: signedIn && !!lookup.data, retry: false });
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState("");

    const portal = lookup.data ?? null;
    const signInHere = () => navigate({ to: "/auth/login", search: { portal: host }, replace: true });

    // Nobody is signed in here, so the sign-in comes first.
    useEffect(() => {
        if (portal && (!signedIn || me.isError)) signInHere();
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [portal, signedIn, me.isError]);

    const continueAs = async () => {
        setBusy(true);
        setError("");
        try {
            await goToPortal(host);
        } catch (err) {
            setError(buildError(err as AppError));
            setBusy(false);
        }
    };

    if (onPortal || !host || lookup.isError || (lookup.isSuccess && !portal)) {
        return (
            <PortalCard title="Sign-in link not recognised">
                <PortalError message="This link does not name an agency portal that signs its clients in here. Go back to the portal and try again." />
            </PortalCard>
        );
    }

    if (!portal || !me.data) {
        return (
            <PortalCard brand={portal?.brand} title="Signing you in">
                <Loader2Icon className="w-5 h-5 animate-spin text-slate-400 mx-auto mt-4" />
            </PortalCard>
        );
    }

    return (
        <PortalCard brand={portal.brand} title={`Continue to ${portal.brand.name}`}>
            <p className="text-sm text-slate-500 mt-2">
                You are signed in as <span className="font-medium text-slate-700 break-all">{me.data.email}</span>. Continuing signs you in on{" "}
                <span className="font-medium text-slate-700 break-all">{portal.host}</span>, where you reach only this agency's client workspaces.
            </p>
            {error && <p className="mt-3 text-[12.5px] text-red-600">{error}</p>}
            <button
                type="button"
                onClick={continueAs}
                disabled={busy}
                className="mt-5 w-full h-10 rounded-md bg-sky-600 text-white text-[13px] font-medium hover:bg-sky-700 transition-colors disabled:opacity-60 inline-flex items-center justify-center gap-2"
            >
                {busy && <Loader2Icon className="w-4 h-4 animate-spin" />}
                Continue as {me.data.email}
            </button>
            <button
                type="button"
                onClick={signInHere}
                disabled={busy}
                className="mt-2 w-full h-9 rounded-md text-[12.5px] font-medium text-slate-600 hover:bg-slate-50 transition-colors"
            >
                Use a different account
            </button>
        </PortalCard>
    );
}
