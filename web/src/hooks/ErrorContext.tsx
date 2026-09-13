// Tells the analytics and error backends who is signed in and where they are.
//
// Without this an issue in error tracking is a stack trace and nothing else:
// you can see that a page throws and never which workspace it throws for, or
// what the user did just before. With it, "customer X says campaigns are
// broken" is a search, the issue lists the route they came from, and the
// session replay and every product event belong to that account and that
// workspace. See lib/observability.
import { useEffect } from "react";
import { useLocation } from "react-router-dom";
import { useUserProfile } from "./context/user";
import { useAppStore } from "@/stores";
import { noteStep, setErrorIdentity } from "@/lib/observability";

export function ErrorContext() {
    const { user } = useUserProfile();
    const organization = useAppStore((s) => s.currentOrganization);
    const { pathname } = useLocation();

    const userId = user?.id ?? null;
    const email = user?.email ?? null;
    const name = [user?.first_name, user?.last_name].filter(Boolean).join(" ") || null;
    const organizationId = organization?.id ?? null;
    const organizationName = organization?.name ?? null;
    const plan = organization?.plan ?? null;

    useEffect(() => {
        setErrorIdentity(userId ? { userId, email, name, organizationId, organizationName, plan } : null);
        // Cleared on unmount, which is what a sign-out is: the next person on
        // a shared machine must not inherit this attribution.
        return () => setErrorIdentity(null);
    }, [userId, email, name, organizationId, organizationName, plan]);

    useEffect(() => {
        noteStep(`Opened ${pathname}`, { path: pathname });
    }, [pathname]);

    return null;
}
