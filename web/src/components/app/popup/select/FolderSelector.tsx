import React from "react";
import Selector from "./Selector";
import SelectMenu from "./SelectMenu";
import { useUserProfile } from "@/hooks/context/user";
import SelectOption from "./SelectOption";
import { RiFolderLine, RiSoundModuleLine } from "@remixicon/react";
import useClickOutside from "@/hooks/useClickOutside";

export default function FolderSelector({ onAdd, onRemove, selected }: { onAdd: (id: string) => void, onRemove: (id: string) => void, selected: string[] }) {
    const profile = useUserProfile();

    const [show, setShow] = React.useState<boolean>(false);
    const popupRef = React.useRef<HTMLDivElement>(null);

    useClickOutside(popupRef, () => setShow(false))

    return (
        <div className="relative" ref={popupRef}>
            <Selector show={show} setShow={setShow} caret>
                <div className="flex gap-1 flex-wrap select-none">
                    {selected.length > 0 ? selected.map((v) => {
                        const t = profile?.user.folders.find((t) => t.id === v)
                        if (!t) return null;

                        return (
                            <div key={v} className="flex gap-2 overflow-hidden cursor-default items-center py-px px-3 rounded-full bg-slate-100" style={{ color: `${t.color}` }}>
                                <div
                                >
                                    <RiFolderLine className="w-3 shrink-0" />
                                </div>
                                <span className="text-sm truncate">{t.title}</span>
                            </div>
                        )
                    }) : (
                        <span className="text-slate-400 py-px">No folders selected...</span>
                    )}
                </div>
            </Selector>
            <SelectMenu show={show}>
                {profile?.user.folders.sort((a, b) => a.position - b.position).map((folder) => {
                    const isSelected = selected.some((v) => v === folder.id)
                    return (
                        <SelectOption
                            key={folder.id}
                            onClick={() => {
                                if (isSelected) {
                                    onRemove(folder.id)
                                } else {
                                    onAdd(folder.id)
                                }
                            }}
                            color={folder.color}
                            selected={isSelected}
                        >
                            <RiFolderLine className="w-4 shrink-0" />
                            <span className="truncate">{folder.title}</span>
                        </SelectOption>
                    )
                })}
                <SelectOption onClick={() => {
                    profile?.setFoldersEdit(true);
                }}>
                    <RiSoundModuleLine className="w-4 shrink-0" />
                    <span className="truncate">Manage Folders</span>
                </SelectOption>
            </SelectMenu>
        </div>
    )
}
