import type Access from "@/lib/api/models/app/admin/Access";
import type Timezone from "@/lib/api/models/app/Timezone";
import type User from "@/lib/api/models/auth/User";
import { createContext, useContext } from "react";

interface UserC {
    user: User;
    access: Access;
    timezones: Timezone[];
    tagsEdit: boolean;
    setTagsEdit: React.Dispatch<React.SetStateAction<boolean>>;
    foldersEdit: boolean;
    setFoldersEdit: React.Dispatch<React.SetStateAction<boolean>>;
    addEmail: boolean;
    setAddEmail: React.Dispatch<React.SetStateAction<boolean>>;
}

export const UserContext = createContext<UserC | null>(null);

export function useUserProfile() {
    const c = useContext(UserContext);
    if (!c) {
        throw Error("User not loaded.")
    }
    return c;
}
