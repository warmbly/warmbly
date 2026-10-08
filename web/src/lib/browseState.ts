export const BROWSE_STORAGE_PREFIX = "warmbly:browse:v1:";
let sessionGeneration = 0;
let sessionActive = true;

export const browseSessionGeneration = () => sessionGeneration;
export const canPersistBrowseState = (generation: number) => sessionActive && generation === sessionGeneration;
export const resumeBrowseState = () => { sessionActive = true; };

export function clearBrowseState() {
    sessionActive = false;
    sessionGeneration++;
    try {
        for (let index = sessionStorage.length - 1; index >= 0; index--) {
            const key = sessionStorage.key(index);
            if (key?.startsWith(BROWSE_STORAGE_PREFIX) || key?.startsWith("warmbly:mailbox-tag:")) sessionStorage.removeItem(key);
        }
    } catch { /* Storage may be disabled. */ }
}
