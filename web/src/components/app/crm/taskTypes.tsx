// Task-type helpers. Types are user-managed (see TaskTypePicker), so this no
// longer holds a fixed list — just the colour palette used by the create/edit
// form and a resolver from a task's type name to its colour.

export const TASK_TYPE_COLORS = [
    "#8b5cf6",
    "#0ea5e9",
    "#f59e0b",
    "#10b981",
    "#f43f5e",
    "#6366f1",
    "#14b8a6",
    "#94a3b8",
];

export function taskTypeColor(
    name: string | undefined,
    types: { name: string; color: string }[],
): string {
    if (!name) return "#94a3b8";
    return types.find((t) => t.name === name)?.color ?? "#94a3b8";
}
