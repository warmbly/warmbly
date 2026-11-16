// The hand-off between a request that needs a fresh confirmation and the prompt that asks for it.
// The prompt loads lazily, so a request can ask before anything is listening; it is held here
// and the prompt takes it the moment it mounts.

export interface ReauthDetail {
    resolve: () => void;
    reject: () => void;
    /** Called by a mounted prompt as it takes the request over. */
    ack?: () => void;
}

const held: ReauthDetail[] = [];

/** Offers a request to the prompt; one that nothing took is held until a prompt mounts or it is withdrawn. */
export function askForReauth(detail: ReauthDetail) {
    let taken = false;
    const offered: ReauthDetail = {
        ...detail,
        ack: () => {
            taken = true;
            detail.ack?.();
        },
    };
    window.dispatchEvent(new CustomEvent("reauth-required", { detail: offered }));
    if (!taken) held.push(offered);
    return () => {
        const i = held.indexOf(offered);
        if (i >= 0) held.splice(i, 1);
    };
}

/** The requests that asked before a prompt was listening. */
export function takeHeldReauth(): ReauthDetail[] {
    return held.splice(0);
}
