// Terminal states, word-for-word with the static pages the portal service serves for the same conditions.

export function NotFound() {
    return (
        <div className="plain">
            <h1>There is nothing at this address</h1>
            <p>Check the link you were sent.</p>
        </div>
    );
}

// StalePage: the tab outlived its render token (12h); only a reload mints a new one.
export function StalePage() {
    return (
        <div className="plain">
            <h1>This page went stale</h1>
            <p>It has been open for a while. Refresh to continue.</p>
            <button type="button" onClick={() => window.location.reload()}>
                Refresh
            </button>
        </div>
    );
}
