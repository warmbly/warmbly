export default interface ResetPasswordConfirm {
    // The backend binds this as `session`; it used to be sent as `token`,
    // which never matched.
    session: string;
    password: string;
    turnstile: string;
}
