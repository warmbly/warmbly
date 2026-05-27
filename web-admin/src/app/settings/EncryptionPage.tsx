import { BackendsPage } from "./BackendsPage";

export default function EncryptionPage() {
    return (
        <BackendsPage
            kind="kms"
            title="Encryption (KMS)"
            description="The platform's envelope-encryption root of trust. Per CLAUDE.md, KMS issues per-user 32-byte DEKs that are AES-256-GCM sealed and cached in Redis."
            notes="Encrypted DEK blobs live in DynamoDB; the plaintext DEK is only ever held in the Redis cache and in process memory. Swapping providers here re-routes new key-generation calls. Existing DEKs stay readable so long as the previous provider keeps the wrapping key alive."
        />
    );
}
