import useDeleteContact from "@/lib/api/hooks/app/contacts/useDeleteContact";
import type Contact from "@/lib/api/models/app/contacts/Contact";
import Checkbox from "../Checkbox";
import { RiSendPlaneLine } from "@remixicon/react";
import { useConfirm } from "@/hooks/context/confirm";
import toast from "react-hot-toast";
import type { AppError } from "@/lib/api/client/normalizeError";
import buildError from "@/lib/helper/buildError";
import React from "react";
import { Loading } from "@/components/loader";

function NULL() {
    return <span className="text-slate-300">NULL</span>
}

export default function ContactEntry({
    contact,
    index,
    isSelected,
    onSelection,
    onEdit,
}: {
    contact: Contact,
    index: number,
    isSelected: boolean,
    onSelection: () => void,
    onEdit: () => void,
}) {
    const [deleteLoad, setDeleteLoad] = React.useState<boolean>(false);
    const confirm = useConfirm();
    const deleteContact = useDeleteContact(contact.id);

    async function onDelete() {
        setDeleteLoad(true)
        try {
            await toast.promise(
                deleteContact.mutateAsync(),
                {
                    loading: `Deleting contact... (${contact.email})`,
                    success: `Contact successfully deleted. (${contact.email})`,
                    error: (err: AppError) => buildError(err),
                }
            )
        } finally {
            setDeleteLoad(false)
        }
    }

    return (

        <tr className={`bg-white ${index === 0 ? "border-y" : "border-b"} border-x border-gray-200 hover:bg-gray-50`}>
            <td className="w-4 p-4">
                <input
                    id={`checkbox-all-contacts-${contact.id}`}
                    type="checkbox"
                    className="hidden"
                    onChange={onSelection}
                />
                <label className="cursor-pointer" htmlFor={`checkbox-all-contacts-${contact.id}`}>
                    <Checkbox
                        checked={isSelected}
                    />
                </label>
            </td>
            <th scope="row" className="px-6 py-4 font-medium text-gray-900 whitespace-nowrap">
                {contact.first_name ? contact.first_name : <NULL />}
            </th>
            <th scope="row" className="px-6 py-4 font-medium text-gray-900 whitespace-nowrap">
                {contact.last_name ? contact.last_name : <NULL />}
            </th>
            <td className="px-6 py-4">
                {contact.email}
            </td>
            <td className="px-6 py-4">
                {contact.company ? contact.company : <NULL />}
            </td>
            <td className="px-6 py-4">
                {contact.phone ? contact.phone : <NULL />}
            </td>
            <td className={`px-6 py-4 ${contact.subscribed ? "text-green-500" : "text-red-500"}`}>
                {contact.subscribed ? "True" : "False"}
            </td>
            <td className="px-6 py-4">
                {contact.campaigns.length === 0 ? <span className="text-slate-300">EMPTY</span> : <>
                    <div className="flex max-w-40 items-center gap-2">
                        <div className="bg-slate-100 py-0.5 px-2 flex gap-2 items-center rounded-lg overflow-hidden grow">
                            <RiSendPlaneLine className="h-3 w-3 shrink-0" />
                            <div className="overflow-hidden">
                                <p className="truncate">{contact.campaigns[0].name}</p>
                            </div>
                        </div>
                        {contact.campaigns.length > 1 && <span className="text-slate-400 text-sm shrink-0">+{contact.campaigns.length - 1}</span>}
                    </div>
                </>}
            </td>
            <td className="px-6 py-4 text-slate-400 font-poppins">
                {Object.keys(contact.custom_fields).length} Fields
            </td>
            <td className="flex items-center px-6 py-4 gap-1">
                <button
                    className="h-6 w-13 bg-blue-100 hover:bg-blue-200 ripple rounded-lg text-blue-500 transition cursor-pointer"
                    onClick={onEdit}>Edit</button>
                <button
                    className="h-6 w-15 bg-red-100 hover:bg-red-200 ripple rounded-lg text-red-500 transition cursor-pointer"
                    onClick={() => {
                        confirm?.show(
                            `Are you sure you want to delete this contact? (${contact.first_name ? contact.first_name + " " : ""}${contact.last_name ? contact.last_name + " " : ""}<${contact.email}>)`,
                            onDelete,
                        )
                    }}>
                    {deleteLoad ? <Loading className="h-4" /> : "Delete"}
                </button>
            </td>
        </tr>
    )
}
