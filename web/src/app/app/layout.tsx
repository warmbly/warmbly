import LinkProvider from "@/hooks/LinkProvider";
import SocketProvider from "@/hooks/SocketProvider";
import { UserProvider } from "@/hooks/UserProvider";
import ConfirmProvider from "@/hooks/ConfirmProvider";
import UpgradeDialogProvider from "@/hooks/UpgradeDialogProvider";
import { AppLayout } from "@/components/layout/AppLayout";

import { DataSyncProvider } from "@/hooks/DataSyncProvider";
import { RealtimeManager } from "@/hooks/RealtimeManager";
import { OrgGate } from "@/hooks/OrgGate";
import { RestrictedRouteGuard } from "@/hooks/RestrictedRouteGuard";
import { ErrorContext } from "@/hooks/ErrorContext";
import TagsModal from "@/components/app/modals/TagsModal";
import FoldersModal from "@/components/app/modals/FoldersModal";
import AddEmailModal from "@/components/app/modals/AddEmailModal";
import ComposeWindow from "@/components/app/unibox/compose/ComposeWindow";
import PasskeyEnrollPrompt from "@/components/app/modals/PasskeyEnrollPrompt";
import PermissionDeniedModal from "@/components/app/modals/PermissionDeniedModal";
import ReauthModal from "@/components/app/modals/ReauthModal";

export default function RootAppLayout() {
    // The route already checked the session (bootDashboard in router.tsx).
    //
    // Global modals live inside ConfirmProvider so useConfirm() works
    // inside their action handlers (delete confirmations etc.). They
    // need UserProvider too (state lives there: tagsEdit, foldersEdit,
    // addEmail), so they sit between the two providers.
    //
    // They are inside UpgradeDialogProvider for the same class of reason, and
    // it is easy to get wrong: they render below AppLayout in the file but are
    // siblings of it, so a provider that wraps only AppLayout does not reach
    // them. AddEmailModal renders MailboxAllowanceDialog, which calls
    // useUpgradeDialog to offer the plan that lifts the cap, so leaving it
    // outside threw the moment someone hit their mailbox allowance -- exactly
    // when the upgrade path is what they needed.
    return <UserProvider>
        <DataSyncProvider>
            <ConfirmProvider>
                {/* Plan-locked surfaces anywhere under /app open the full-screen
                    plan chooser through useUpgradeDialog(). */}
                <UpgradeDialogProvider>
                    <LinkProvider>
                        <SocketProvider>
                            <RealtimeManager>
                                {/* OrgGate runs ahead of AppLayout — if
                                    the user has no workspaces it redirects
                                    to /select-org before any org-scoped
                                    query (e.g. /unibox) runs with no org. */}
                                <OrgGate />
                                <RestrictedRouteGuard />
                                {/* Names the workspace and user on reported
                                    exceptions, and leaves the route on their
                                    trail. Renders nothing. */}
                                <ErrorContext />
                                <AppLayout />
                            </RealtimeManager>
                        </SocketProvider>
                    </LinkProvider>
                    <TagsModal />
                    <FoldersModal />
                    <AddEmailModal />
                    <ComposeWindow />
                    <PasskeyEnrollPrompt />
                    <PermissionDeniedModal />
                    <ReauthModal />
                </UpgradeDialogProvider>
            </ConfirmProvider>
        </DataSyncProvider>
    </UserProvider>
}
