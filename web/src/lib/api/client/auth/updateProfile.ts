import Request from "../Request";

interface UpdateProfileData {
    first_name: string;
    last_name: string;
}

export default async function updateProfile(data: UpdateProfileData): Promise<void> {
    await Request<void>({
        method: "PATCH",
        url: "/auth/me",
        data,
        authorization: true,
    });
}
