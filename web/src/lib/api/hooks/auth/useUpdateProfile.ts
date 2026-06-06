import { useMutation, useQueryClient } from "@tanstack/react-query";
import updateProfile from "../../client/auth/updateProfile";

interface UpdateProfileData {
    first_name: string;
    last_name: string;
}

export default function useUpdateProfile() {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: (data: UpdateProfileData) => updateProfile(data),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ["auth", "me"] });
        },
    });
}
