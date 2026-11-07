import { useCreateRole } from "@/lib/api/hooks/app/organizations/useRoleMutations";
import useRoles from "@/lib/api/hooks/app/organizations/useRoles";
import { type ClientRolePreset } from "@/lib/accessScope";

/** The workspace role a client preset names, created on first use; resolves to its id. */
export default function useEnsureClientRole() {
    const roles = useRoles();
    const create = useCreateRole();
    // A role only stands for a preset when it grants exactly what the preset says, whatever it is called.
    const roleFor = (preset: ClientRolePreset) => {
        const matching = (roles.data ?? []).filter(
            (r) => r.permissions === preset.permissions && Boolean(r.acts_in_scope) === preset.acts_in_scope,
        );
        return matching.find((r) => r.name === preset.name) ?? matching[0];
    };
    const freeName = (base: string) => {
        const taken = new Set((roles.data ?? []).map((r) => r.name));
        let name = base;
        for (let n = 2; taken.has(name); n++) name = `${base} (${n})`;
        return name;
    };
    return {
        pending: create.isPending,
        roleFor,
        ensure: async (preset: ClientRolePreset): Promise<string> => {
            const existing = roleFor(preset);
            if (existing) return existing.id;
            const role = await create.mutateAsync({
                name: freeName(preset.name),
                description: preset.summary,
                color: preset.color,
                permissions: preset.permissions,
                acts_in_scope: preset.acts_in_scope,
            });
            return role.id;
        },
    };
}
