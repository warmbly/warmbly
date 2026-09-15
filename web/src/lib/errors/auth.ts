export class AuthError extends Error {
    constructor(message = "Authorization failed") {
        super(message)
        this.name = "AuthError"
    }
}

// Built per throw, not shared. A module-level instance captures its stack once,
// at module evaluation, so every report pointed at "module code" instead of the
// call that failed, and one mutable Error was shared across concurrent requests.
export const sessionExpired = () => new AuthError("Session expired")
export const noToken = () => new AuthError("No authorization token")
