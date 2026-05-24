import React, { useMemo } from 'react';
import { UserContext } from './context/user';
import useUser from '@/lib/api/hooks/auth/useUser';
import { AnimatePresence } from 'framer-motion';
import LoadingScreen from '@/components/LoadingScreen';
import useRoles from '@/lib/api/hooks/app/admin/roles/useRoles';
import useTimezones from '@/lib/api/hooks/app/useTimezones';
import type { AppError } from '@/lib/api/client/normalizeError';
import { AuthError } from '@/lib/errors/auth';
import { Navigate } from 'react-router-dom';
import { clearTokens } from '@/lib/auth';
import type Access from '@/lib/api/models/app/admin/Access';
import type Timezone from '@/lib/api/models/app/Timezone';
import type User from '@/lib/api/models/auth/User';

const EMPTY_ACCESS: Access = { roles: [], permissions: [] };
const EMPTY_TIMEZONES: Timezone[] = [];

export const UserProvider = ({ children }: { children: React.ReactNode }) => {
    const user = useUser();
    const access = useRoles();
    const timezones = useTimezones();

    const [tagsEdit, setTagsEdit] = React.useState<boolean>(false);
    const [foldersEdit, setFoldersEdit] = React.useState<boolean>(false);
    const [addEmail, setAddEmail] = React.useState<boolean>(false);
    const safeUser = useMemo((): User | null => {
        if (!user.data) return null;
        return {
            ...user.data,
            tags: Array.isArray(user.data.tags) ? user.data.tags : [],
            categories: Array.isArray(user.data.categories) ? user.data.categories : [],
            folders: Array.isArray(user.data.folders) ? user.data.folders : [],
            roles: Array.isArray(user.data.roles) ? user.data.roles : [],
        };
    }, [user.data]);

    const error = useMemo(() => {
        const errs = [user.error, access.error, timezones.error].filter(Boolean);
        for (const err of errs) {
            if (err instanceof AuthError) {
                return { redirect: true, title: "Authentication Required", message: err.message };
            }
            const myerr = err as unknown as AppError;
            if (myerr.status === 401 || myerr.redirect) {
                return { redirect: true, title: "Authentication Required", message: myerr.message ?? "Session expired." };
            }
        }

        if (user.error) {
            const myerr = user.error as unknown as AppError;
            return {
                title: `${myerr.error ?? "Error"}${myerr.status ? ` (${myerr.status})` : ""}`,
                message: myerr.message ?? "An unexpected error occurred.",
            };
        }
    }, [user.error, access.error, timezones.error]);

    if (error?.redirect) {
        clearTokens();
        return <Navigate to="/auth/login" replace />;
    }

    if (safeUser && !safeUser.onboarding_completed_at) {
        return <Navigate to="/onboarding" replace />;
    }

    if (!safeUser) {
        return (
            <AnimatePresence>
                <LoadingScreen
                    errorTitle={error?.title}
                    errorMessage={error?.message}
                />
            </AnimatePresence>
        );
    }

    // NOTE: TagsModal / FoldersModal / AddEmailModal are NOT rendered
    // here anymore. They depend on ConfirmContext (via useConfirm),
    // which lives below this provider in the tree. Mounting them here
    // put them outside the ConfirmProvider and crashed with
    // "ConfirmProvider not found" on first interaction. They're now
    // rendered in app/app/layout.tsx after ConfirmProvider is in scope.
    return (
        <UserContext.Provider value={{
            user: safeUser,
            access: access.data ?? EMPTY_ACCESS,
            timezones: timezones.data ?? EMPTY_TIMEZONES,
            tagsEdit,
            setTagsEdit,
            foldersEdit,
            setFoldersEdit,
            addEmail,
            setAddEmail,
        }}>
            {children}
        </UserContext.Provider>
    );
};
