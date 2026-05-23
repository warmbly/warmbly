import LinkProvider from "@/hooks/LinkProvider";
import SocketProvider from "@/hooks/SocketProvider";
import { UserProvider } from "@/hooks/UserProvider";
import ConfirmProvider from "@/hooks/ConfirmProvider";
import { AppLayout } from "@/components/layout/AppLayout";

import getToken from "@/lib/helper/getToken";
import { Navigate } from "react-router-dom";
import { DataSyncProvider } from "@/hooks/DataSyncProvider";
import { RealtimeManager } from "@/hooks/RealtimeManager";
import { OrgGate } from "@/hooks/OrgGate";
import TagsModal from "@/components/app/modals/TagsModal";
import FoldersModal from "@/components/app/modals/FoldersModal";
import AddEmailModal from "@/components/app/modals/AddEmailModal";

export default function RootAppLayout() {
    const token = getToken();
    if (!token) {
        return <Navigate to="/auth/login" replace />;
    }

    // Global modals live inside ConfirmProvider so useConfirm() works
    // inside their action handlers (delete confirmations etc.). They
    // need UserProvider too (state lives there: tagsEdit, foldersEdit,
    // addEmail), so they sit between the two providers.
    return <UserProvider>
        <DataSyncProvider>
            <ConfirmProvider>
                <LinkProvider>
                    <SocketProvider>
                        <RealtimeManager>
                            {/* OrgGate runs ahead of AppLayout — if
                                the user has no workspaces it redirects
                                to /select-org before any org-scoped
                                query (e.g. /unibox) runs with no org. */}
                            <OrgGate />
                            <AppLayout />
                        </RealtimeManager>
                    </SocketProvider>
                </LinkProvider>
                <TagsModal />
                <FoldersModal />
                <AddEmailModal />
            </ConfirmProvider>
        </DataSyncProvider>
    </UserProvider>
}
