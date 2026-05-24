import type User from "../../models/auth/User";
import Request from "../Request";

function asArray<T>(value: T[] | null | undefined): T[] {
    return Array.isArray(value) ? value : [];
}

export default async function getUser(): Promise<User> {
    const user = await Request<User>({
        method: "GET",
        url: "/auth/me",
        authorization: true,
    });

    return {
        ...user,
        tags: asArray(user.tags),
        categories: asArray(user.categories),
        folders: asArray(user.folders),
        roles: asArray(user.roles),
    };
}
