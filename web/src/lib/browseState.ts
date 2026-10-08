export const BROWSE_STORAGE_PREFIX = "warmbly:browse:v1:";

export function clearBrowseState() {
    try {
        for (let index = sessionStorage.length - 1; index >= 0; index--) {
            const key = sessionStorage.key(index);
            if (key?.startsWith(BROWSE_STORAGE_PREFIX) || key?.startsWith("warmbly:mailbox-tag:")) sessionStorage.removeItem(key);
        }
    } catch { /* Storage may be disabled. */ }
}
