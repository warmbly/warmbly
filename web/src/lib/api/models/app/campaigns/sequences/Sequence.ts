export default interface Sequence {
    id: string;
    name: string;

    subject: string;

    body_plain: string;
    body_html: string;
    body_sync: boolean;
    body_code: boolean;

    wait_after: number;

    updated_at: Date;
    created_at: Date;
}
