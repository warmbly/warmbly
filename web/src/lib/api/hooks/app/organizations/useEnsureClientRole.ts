import { useCreateRole } from "@/lib/api/hooks/app/organizations/useRoleMutations";
import useRoles from "@/lib/api/hooks/app/organizations/useRoles";
import { type ClientRolePreset } from "@/lib/accessScope";

/** The workspace role a client preset names, created on first use; resolves to its id. */
export default function useEnsureClientRole() {
    const roles = useRoles();
    const create = useCreateRole();
    return {
        pending: create.isPending,
        roleFor: (preset: ClientRolePreset) => (roles.data ?? []).find((r) => r.name === preset.name),
        ensure: async (preset: ClientRolePreset): Promise<string> => {
            const existing = (roles.data ?? []).find((r) => r.name === preset.name);
            if (existing) return existing.id;
            const role = await create.mutateAsync({
                name: preset.name,
                description: preset.summary,
                color: preset.color,
                permissions: preset.permissions,
                acts_in_scope: preset.acts_in_scope,
            });
            return role.id;
        },
    };
}
