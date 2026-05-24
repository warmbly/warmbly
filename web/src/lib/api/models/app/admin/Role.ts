export default interface Role {
    id: string;
    permissions: number;
    name: string;
    color: string;

    created_at: Date;
    updated_at: Date;
}
