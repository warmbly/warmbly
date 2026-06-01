// A registered passkey, as returned by the passkey management endpoints.
// created_at / last_used_at arrive as ISO strings and are revived to Date by
// the API client.
export default interface Passkey {
    id: string;
    name: string;
    provider?: string;
    credential_id: string;
    transports: string[];
    backup_state: boolean;
    created_at: Date;
    last_used_at: Date | null;
}
