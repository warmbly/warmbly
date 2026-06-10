import React from "react";
import toast from "react-hot-toast";
import { useUserProfile } from "@/hooks/context/user";
import { TextInput } from "@/components/ui/field";
import { TopbarAction } from "@/components/layout/Page";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import useUpdateProfile from "@/lib/api/hooks/auth/useUpdateProfile";
import { AvatarUploader } from "@/components/app/avatar/AvatarUploader";
import {
    useDeleteUserAvatar,
    useUploadUserAvatar,
} from "@/lib/api/hooks/app/avatar/useUserAvatar";
import { Row, Section, SectionShell, initials } from "../_components/SectionShell";

export default function ProfileSettingsPage() {
    const { user } = useUserProfile();
    const [firstName, setFirstName] = React.useState(user.first_name ?? "");
    const [lastName, setLastName] = React.useState(user.last_name ?? "");
    const dirty =
        firstName !== (user.first_name ?? "") || lastName !== (user.last_name ?? "");

    const uploadAvatar = useUploadUserAvatar();
    const removeAvatar = useDeleteUserAvatar();
    const updateProfile = useUpdateProfile();

    async function save() {
        if (!dirty || updateProfile.isPending) return;
        if (!firstName.trim() || !lastName.trim()) {
            toast.error("First and last name are required.");
            return;
        }
        try {
            await updateProfile.mutateAsync({
                first_name: firstName.trim(),
                last_name: lastName.trim(),
            });
            toast.success("Profile saved");
        } catch (err) {
            toast.error(buildError(err as AppError));
        }
    }

    return (
        <SectionShell
            title="Profile"
            description="Used in emails sent on your behalf, the sidebar avatar, and any invitation you send out."
            actions={
                dirty ? (
                    <>
                        <TopbarAction
                            variant="ghost"
                            onClick={() => {
                                setFirstName(user.first_name ?? "");
                                setLastName(user.last_name ?? "");
                            }}
                        >
                            Discard
                        </TopbarAction>
                        <TopbarAction onClick={save} disabled={updateProfile.isPending}>
                            {updateProfile.isPending ? "Saving…" : "Save profile"}
                        </TopbarAction>
                    </>
                ) : null
            }
        >
            <Section eyebrow="Identity" description="Names appear on outgoing emails.">
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div>
                        <FieldLabel>First name</FieldLabel>
                        <TextInput value={firstName} onChange={setFirstName} className="w-full" />
                    </div>
                    <div>
                        <FieldLabel>Last name</FieldLabel>
                        <TextInput value={lastName} onChange={setLastName} className="w-full" />
                    </div>
                </div>
                <Row
                    label="Email"
                    description="Email changes go through support for now."
                    align="start"
                >
                    <input
                        type="email"
                        value={user.email}
                        readOnly
                        className="w-full max-w-[280px] h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12.5px] text-slate-500 font-mono"
                    />
                </Row>
                <Row
                    label="Timezone"
                    description="Detected from your browser. Used to render campaign schedules in local time."
                    align="start"
                >
                    <input
                        type="text"
                        value={Intl.DateTimeFormat().resolvedOptions().timeZone}
                        readOnly
                        className="w-full max-w-[280px] h-7 px-2.5 rounded-md border border-slate-200 bg-slate-50 text-[12.5px] text-slate-500 font-mono"
                    />
                </Row>
            </Section>

            <Section
                eyebrow="Avatar"
                description="Shown in the sidebar, on email previews, and next to your activity. Resized to 512px before upload."
            >
                <AvatarUploader
                    current={user.avatar_url}
                    fallbackInitials={initials(user.email)}
                    shape="circle"
                    onUpload={async (blob) => {
                        await uploadAvatar.mutateAsync(blob);
                    }}
                    onRemove={async () => {
                        await removeAvatar.mutateAsync();
                    }}
                />
            </Section>
        </SectionShell>
    );
}

function FieldLabel({ children }: { children: React.ReactNode }) {
    return (
        <div className="text-[10.5px] uppercase tracking-[0.12em] text-slate-400 font-medium mb-1">
            {children}
        </div>
    );
}
