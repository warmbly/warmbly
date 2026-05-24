// Tags modal — brae-density list editor.
//
// Same shape as FoldersModal but for the email-account tag store.
// Backed by the user.tags array on the cached User profile.

import { useQueryClient } from "@tanstack/react-query";
import { useUserProfile } from "@/hooks/context/user";
import createTag from "@/lib/api/client/app/tags/createTag";
import updateTag from "@/lib/api/client/app/tags/updateTag";
import deleteTag from "@/lib/api/client/app/tags/deleteTag";
import type Tag from "@/lib/api/models/app/Tag";
import type User from "@/lib/api/models/auth/User";
import { LabelListModal } from "@/components/app/shared/LabelListModal";

export default function TagsModal() {
    const user = useUserProfile();
    const qc = useQueryClient();

    function updateUser(fn: (u: User) => User) {
        qc.setQueryData<User>(["auth", "me"], (old) => (old ? fn(old) : old));
    }

    return (
        <LabelListModal
            open={user?.tagsEdit ?? false}
            onClose={() => user?.setTagsEdit(false)}
            eyebrow="Tags"
            subtitle="Label email accounts by purpose, region or audience"
            addCta="New tag"
            items={user?.user.tags ?? []}
            onCreate={async (title, color) => {
                const t = await createTag(title, color);
                updateUser((u) => ({ ...u, tags: [...u.tags, t] }));
            }}
            onUpdate={async (id, data) => {
                const updated = await updateTag(id, data as Partial<Tag>);
                updateUser((u) => ({
                    ...u,
                    tags: u.tags.map((t) => (t.id === updated.id ? updated : t)),
                }));
            }}
            onDelete={async (id) => {
                await deleteTag(id);
                updateUser((u) => ({ ...u, tags: u.tags.filter((t) => t.id !== id) }));
            }}
        />
    );
}
