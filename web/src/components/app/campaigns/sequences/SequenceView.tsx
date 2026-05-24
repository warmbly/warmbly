import React, { useMemo } from "react";
import type Sequence from "@/lib/api/models/app/campaigns/sequences/Sequence";
import SubTitle from "../../text/SubTitle";
import MiniInput from "../../popup/MiniInput";
import EmailEditor from "../../EmailEditor";
import { Loading } from "@/components/loader";
import useUpdateSequence from "@/lib/api/hooks/app/campaigns/sequences/useUpdateSequence";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";

export default function SequenceView({
    campaign_id,
    def_sequence,
    sequence,

    setName,
    setSubject,
    setBodyPlain,
    setBodyHTML,
    setBodySync,
    setBodyCode,
    onUpdate,
}: {
    campaign_id: string,
    def_sequence: Sequence,
    sequence: Sequence,

    setName: (v: string) => void,
    setSubject: (v: string) => void,
    setBodyPlain: (v: string) => void,
    setBodyHTML: (v: string) => void,
    setBodySync: (v: boolean) => void,
    setBodyCode: (v: boolean) => void,
    onUpdate: (v: Sequence) => void,
}) {
    const updateSequence = useUpdateSequence(campaign_id, sequence.id)

    const [load, setLoad] = React.useState<boolean>(false);

    const savable = useMemo(
        () => JSON.stringify(def_sequence) !== JSON.stringify(sequence),
        [def_sequence, sequence]
    )

    async function submit() {
        if (load) return;
        setLoad(true);
        try {
            const data = {
                ...(sequence.name !== def_sequence.name && { name: sequence.name }),
                ...(sequence.subject !== def_sequence.subject && { subject: sequence.subject }),

                ...(sequence.body_plain !== def_sequence.body_plain && { body_plain: sequence.body_plain }),
                ...(sequence.body_html !== def_sequence.body_html && { body_html: sequence.body_html }),
                ...(sequence.body_sync !== def_sequence.body_sync && { body_sync: sequence.body_sync }),
                ...(sequence.body_code !== def_sequence.body_code && { body_code: sequence.body_code }),
            }
            const resp = await toast.promise(
                updateSequence.mutateAsync(data),
                {
                    loading: "Updating sequence...",
                    success: "Sequence successfully updated.",
                    error: (err: AppError) => buildError(err),
                }
            )
            onUpdate(resp)
        } finally {
            setLoad(false)
        }
    }

    return (
        <div className="grid gap-5">
            <div>
                <SubTitle>Display Name</SubTitle>
                <MiniInput
                    placeholder={def_sequence.name}
                    value={sequence.name}
                    id={sequence.id}
                    onChange={(e) => setName(e.target.value)}
                />
            </div>

            <div className="overflow-hidden">
                <input
                    className="outline-none border-none font-medium font-inter px-3 text-slate-700 placeholder:text-slate-300 w-full py-4"
                    placeholder={def_sequence.subject ? def_sequence.subject : "Subject"}
                    value={sequence.subject}
                    onChange={(e) => setSubject(e.target.value)}
                    type="text"
                />
                <div className="overflow-x-scroll no-scrollbar pb-1">
                    <EmailEditor
                        key={sequence.id}
                        id={`sequence-edit-${sequence.id}`}
                        htmlText={sequence.body_html}
                        plainText={sequence.body_plain}
                        sync={sequence.body_sync}
                        code={sequence.body_code}
                        setHtmlText={setBodyHTML}
                        setPlainText={setBodyPlain}
                        setSync={setBodySync}
                        setCode={setBodyCode}
                    />
                </div>
            </div>
            <div className="flex relative justify-end gap-2">
                <button
                    className={`bg-slate-200 select-none ripple transition flex justify-center items-center cursor-pointer ${!load && "hover:bg-slate-300"} px-3 py-2 rounded-lg text-slate-600`}
                    onClick={() => {
                        setName(def_sequence.name)
                        setSubject(def_sequence.subject)
                        setBodyPlain(def_sequence.body_plain)
                        setBodyHTML(def_sequence.body_html)
                        setBodySync(def_sequence.body_sync)
                        setBodyCode(def_sequence.body_code)
                    }}
                >
                    Reset
                </button>
                <button
                    className={`bg-blue-500 select-none ripple transition w-33 flex justify-center items-center cursor-pointer ${!load && "hover:bg-blue-600"} px-3 py-2 rounded-lg text-slate-50`}
                    onClick={submit}
                >
                    {load ? <Loading className="h-6" /> : "Save Changes"}
                </button>
                <div className={`bg-gray-50 absolute transition select-none left-0 top-0 w-full h-full ${!savable ? "opacity-60 visible" : "opacity-0 invisible"}`} />
            </div>
        </div>
    )
}
