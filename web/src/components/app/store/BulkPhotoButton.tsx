// The mailboxes selection bar's "Set profile photo": one photo for every selected mailbox, shown only when all of
// them were bought through Warmbly, because only those have a provider photo Warmbly can set.
import { CameraIcon, Loader2Icon } from "lucide-react";
import toast from "react-hot-toast/headless";
import { useSetStoreMailboxPhoto, useStoreEnabled, useStoreMailboxes } from "@/lib/api/hooks/app/store/useStore";
import { storeErrorMessage } from "@/lib/buy/errors";
import { storeMailboxEnded } from "@/lib/buy/purchased";
import PhotoPicker from "./PhotoPicker";

export default function BulkPhotoButton({ ids }: { ids: string[] }) {
    const { enabled, status } = useStoreEnabled();
    const list = useStoreMailboxes(enabled);
    const set = useSetStoreMailboxPhoto();
    if (!enabled || !status?.photos || ids.length === 0) return null;
    const byAccount = new Map((list.data?.data ?? []).filter((m) => m.email_account_id && !storeMailboxEnded(m.status)).map((m) => [m.email_account_id as string, m.id]));
    const storeIds = ids.map((id) => byAccount.get(id));
    if (storeIds.some((id) => !id)) return null;
    const n = storeIds.length;
    return (
        <PhotoPicker
            hint={`${n === 1 ? "This mailbox shows" : `All ${n} mailboxes show`} this photo at Google or Microsoft. Recipients see it next to the sender name.`}
            onPicked={async ({ photo }) => {
                const res = await set.mutateAsync({ ids: storeIds as string[], photoId: photo.id });
                if (res.done.length === 0) throw res.error;
                if (res.failed > 0) toast.error(`${res.failed} of ${n} mailboxes kept their photo. ${storeErrorMessage(res.error)}`);
                else toast.success(n === 1 ? "Profile photo updated" : `Profile photo set on ${n} mailboxes`);
            }}
        >
            {(open, busy) => (
                <button
                    type="button"
                    onClick={open}
                    disabled={busy || set.isPending}
                    className="disabled:opacity-50 inline-flex items-center gap-1.5 h-7 px-2.5 rounded text-[12px] font-medium text-slate-600 hover:bg-slate-100 transition-colors"
                >
                    {busy || set.isPending ? <Loader2Icon className="w-3.5 h-3.5 animate-spin" /> : <CameraIcon className="w-3.5 h-3.5" />}
                    Set profile photo
                </button>
            )}
        </PhotoPicker>
    );
}
