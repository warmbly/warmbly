export type APIPermissionCategory = "read" | "write" | "bulk" | "special";

export default interface APIPermission {
    name: string;
    value: number;
    description: string;
    category: APIPermissionCategory;
}

export interface APIPermissionsResponse {
    permissions: APIPermission[];
    presets: {
        read_only: number;
        full_access: number;
    };
}
