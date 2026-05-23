// Folders modal — brae-density list editor.
//
// Replaces the old illustrated split-pane drawer that was completely
// off-theme. Same chrome as every other dialog: 48px header band,
// hairline divider rows, inline edit, slate-900 primary.
//
// Calls the folder client functions directly and invalidates the user
// cache so we don't need an id-scoped hook per row.

import { useQueryClient } from "@tanstack/react-query";
import { useUserProfile } from "@/hooks/context/user";
import createFolder from "@/lib/api/client/app/folders/createFolder";
import updateFolder from "@/lib/api/client/app/folders/updateFolder";
import deleteFolder from "@/lib/api/client/app/folders/deleteFolder";
import type Folder from "@/lib/api/models/app/Folder";
import type User from "@/lib/api/models/auth/User";
import { LabelListModal } from "@/components/app/shared/LabelListModal";

export default function FoldersModal() {
    const user = useUserProfile();
    const qc = useQueryClient();

    function updateUser(fn: (u: User) => User) {
        qc.setQueryData<User>(["auth", "me"], (old) => (old ? fn(old) : old));
    }

    return (
        <LabelListModal
            open={user?.foldersEdit ?? false}
            onClose={() => user?.setFoldersEdit(false)}
            eyebrow="Folders"
            subtitle="Group campaigns by goal, audience or region"
            addCta="New folder"
            items={user?.user.folders ?? []}
            onCreate={async (title, color) => {
                const f = await createFolder(title, color);
                updateUser((u) => ({ ...u, folders: [...u.folders, f] }));
            }}
            onUpdate={async (id, data) => {
                const updated = await updateFolder(id, data as Partial<Folder>);
                updateUser((u) => ({
                    ...u,
                    folders: u.folders.map((f) => (f.id === updated.id ? updated : f)),
                }));
            }}
            onDelete={async (id) => {
                await deleteFolder(id);
                updateUser((u) => ({ ...u, folders: u.folders.filter((f) => f.id !== id) }));
            }}
        />
    );
}
