import LinkProvider from "@/hooks/LinkProvider";
import SocketProvider from "@/hooks/SocketProvider";
import { UserProvider } from "@/hooks/UserProvider";
import ConfirmProvider from "@/hooks/ConfirmProvider";
import { AppLayout } from "@/components/layout/AppLayout";
import { Toaster } from "@/components/ui/sonner";
import getToken from "@/lib/helper/getToken";
import { Navigate } from "react-router-dom";
import { DataSyncProvider } from "@/hooks/DataSyncProvider";
import { RealtimeManager } from "@/hooks/RealtimeManager";

export default function RootAppLayout() {
    const token = getToken();
    if (!token) {
        return <Navigate to="/auth/login" replace />;
    }

    return <UserProvider>
        <DataSyncProvider>
            <ConfirmProvider>
                <LinkProvider>
                    <SocketProvider>
                        <RealtimeManager>
                            <AppLayout />
                            <Toaster />
                        </RealtimeManager>
                    </SocketProvider>
                </LinkProvider>
            </ConfirmProvider>
        </DataSyncProvider>
    </UserProvider>
}
