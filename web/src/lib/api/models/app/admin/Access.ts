import type Permission from "./Permission";
import type Role from "./Role";

export default interface Access {
    roles: Role[];
    permissions: Permission[];
}
