export class AuthError extends Error {
    constructor(message = "Authorization failed") {
        super(message)
        this.name = "AuthError"
    }
}

export const SessionExpired = new AuthError("Session expired")
export const NoToken = new AuthError("No authorization token")
