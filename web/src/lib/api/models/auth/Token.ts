export default interface Token {
    access_token: string;
    access_token_expires_at: Date,
    refresh_token: string;
    refresh_token_expires_at: Date,
}
