export const ADMIN_BROWSE_PREFIX = "warmbly:admin:browse:v1:";
let generation = 0;
let active = true;
export const browseGeneration = () => generation;
export const canPersistBrowse = (value: number) => active && value === generation;
export const resumeBrowse = () => { active = true; };

export function clearAdminBrowse() {
    active = false;
    generation++;
    try {
        for (let index = sessionStorage.length - 1; index >= 0; index--) {
            const key = sessionStorage.key(index);
            if (key?.startsWith(ADMIN_BROWSE_PREFIX)) sessionStorage.removeItem(key);
        }
    } catch { /* Storage may be disabled. */ }
}
