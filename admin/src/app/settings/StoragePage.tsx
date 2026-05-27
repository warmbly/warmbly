import { BackendsPage } from "./BackendsPage";

export default function StoragePage() {
    return (
        <>
            <BackendsPage
                kind="blob"
                title="Storage — Blob"
                description="Object storage backing email attachments, exports, and other binary payloads. Today this is S3-compatible; the registry lets you swap to a different bucket or provider."
                notes="Workers receive pre-signed URLs to read/write encrypted payloads directly; they never see backend credentials, in line with the worker-as-execution-plane rule."
            />
            <div className="my-6 border-t border-border" />
            <BackendsPage
                kind="encrypted_keys"
                title="Storage — Encrypted Keys"
                description="DynamoDB-backed table that holds the encrypted DEK blobs produced by the KMS layer. Per the agent notes, do not migrate these to Postgres."
            />
        </>
    );
}
