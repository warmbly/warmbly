import { Loading } from "@/components/loader";
import { useError } from "@/hooks/ErrorProvider";
import type { Folder} from "@/hooks/UserProvider";
import { useUser } from "@/hooks/UserProvider";
import { APIError, Call } from "@/lib/api";
import {
  DndContext,
  closestCenter,
  PointerSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  verticalListSortingStrategy,
  useSortable,
  arrayMove,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { RiAddLine, RiCloseLine, RiFolderLine } from "@remixicon/react";
import { AnimatePresence, motion } from "framer-motion";
import React from "react";
import { twColors } from "tailwindv4-colors";
import ColorBox from "../colors/ColorBox";
import ColorPanel from "../colors/ColorPanel";

export default function FoldersModal(){
    const { showError } = useError();
    const user = useUser();

    const [foldersLoad, setFoldersLoad] = React.useState<boolean>(false);
    const [addFolder, setAddFolder] = React.useState<string>("");
    const [expandedFolderId, setExpandedFolderId] = React.useState<string | null>(null);

   const sensors = useSensors(
        useSensor(PointerSensor, {
            activationConstraint: { distance: 5 },
            disabled: foldersLoad,
        })
    );

    function updateFolder(id: string, updates: Partial<Folder>) {
        if (user) {
            user.setUser((prev) => prev ? ({...prev, folders: prev.folders.map((t) => (t.id === id ? { ...t, ...updates } : t))}):null);
        }
    }

    function deleteFolder(id: string) {
        if (user) {
            user.setUser((prev) => prev ? ({...prev, folders: prev.folders.filter((t) => t.id !== id)}):null);
        }
    }

    return (
        <AnimatePresence>
                {user && user.foldersEdit && (
                    <motion.div
                     className='bg-black/45 fixed z-100 inset-0 p-3'
                     transition={{type: "spring", duration: 0.3, bounce: 0.3}}
                     initial={{opacity: 0}}
                     animate={{opacity: 1}}
                     exit={{opacity: 0}}
                     >
                        <motion.div
                         className='bg-white h-full w-full relative rounded-3xl py-10 px-10 sm:py-15 sm:px-20'
                         transition={{type: "spring", duration: 0.3, bounce: 0.3}}
                         initial={{scale: .6}}
                         animate={{scale: 1}}
                         exit={{scale: .6}}
                         >
                            <div onClick={() => user.setFoldersEdit(false)} className='absolute top-7 right-7 text-slate-500 hover:opacity-70 transition cursor-pointer'>
                                <RiCloseLine className='w-5'/>
                            </div>
                            <div className='grid h-full lg:grid-cols-2 gap-10 overflow-y-scroll md:overflow-y-auto'>
                                <div className='md:overflow-y-scroll'>
                                    <h1 className='text-slate-600 font-inter font-bold text-4xl mb-8'>Campaign Folders</h1>
                                    <p className='text-slate-500 text-lg font-inter mb-10'>Create folders to organize and manage your campaigns more effectively. Group campaigns by goal, audience, or region (e.g., “Product Launches”, “Customer Retention”, “US Market”). Folders make it easier to find, track, and analyze campaigns without clutter.</p>
                                    <DndContext
                                    sensors={sensors}
                                    collisionDetection={closestCenter}
                                    onDragEnd={async(event) => {
                                        const { active, over } = event;
                                        if (active.id !== over?.id) {
                                            const oldIndex = user.user.folders.findIndex((t) => t.id === active.id);
                                            const newIndex = user.user.folders.findIndex((t) => t.id === over?.id);
                                            const newfolders = arrayMove(user.user.folders, oldIndex, newIndex).map((t, i) => ({
                                                ...t,
                                                position: i,
                                            }));
                                            const originalfolders = user.user.folders;
                                            user.setUser((bef) => bef ? ({...bef, folders: newfolders}):null);
                                            try {
                                                setFoldersLoad(true);
                                                await Call(`/folders/${active.id}/pos`, "POST", { position: newIndex }, true);
                                            } catch (err) {
                                                user.setUser(prev => prev ? { ...prev, folders: originalfolders } : null);
                                                if (err instanceof APIError) {
                                                    showError(err.message, err.body.message)
                                                } else {
                                                    showError("Client Error", `${err}`)
                                                }
                                            } finally {
                                                setFoldersLoad(false);
                                            }
                                        }
                                    }}
                                    >
                                        <SortableContext
                                        items={user.user.folders.map((t) => t.id)}
                                        strategy={verticalListSortingStrategy}
                                        >
                                            <div className='space-y-2'>
                                                {user.user.folders.sort((a, b) => a.position - b.position).map((folder, i) => {
                                                    return (<FolderItem
                                                        disabled={foldersLoad}
                                                        key={folder.id}
                                                        folder={folder}
                                                        expanded={expandedFolderId === folder.id}
                                                        onExpand={() =>
                                                            setExpandedFolderId(
                                                                expandedFolderId === folder.id ? null : folder.id
                                                            )
                                                        }
                                                        onUpdate={(updates) => updateFolder(folder.id, updates)}
                                                        onDelete={() => deleteFolder(folder.id)}
                                                        />
                                                    )
                                                })}
                                            </div>
                                        </SortableContext>
                                    </DndContext>
                                    {user.user.folders.length < 50 && (
                                        <div className='flex bg-slate-100 rounded-xl mt-2'>
                                            <input 
                                             type="text"
                                             className='grow py-3 px-4 outline-none placeholder:text-slate-400 text-slate-600'
                                             placeholder='Folder Name'
                                             value={addFolder}
                                             onChange={(e) => setAddFolder(e.target.value)}
                                             />
                                            <button onClick={async() => {
                                                try {
                                                    setFoldersLoad(true);
                                                    const resp: Folder = await Call("/folders", "POST", {title: addFolder});
                                                    user.setUser((prev) => prev ? ({...prev, folders: [...prev.folders, resp]}):null);
                                                } catch(err) {
                                                    if (err instanceof APIError) {
                                                        showError(err.message, err.body.message)
                                                    } else {
                                                        showError("Client Error", `${err}`)
                                                    }
                                                } finally {
                                                    setFoldersLoad(false);
                                                }
                                            }} className={`shrink-0 ripple w-32 my-1 mr-1 bg-slate-300/70 ${!foldersLoad ? "cursor-pointer hover:bg-slate-300":"cursor-not-allowed"} transition flex items-center justify-center gap-1 px-3 py-2 rounded-lg text-slate-600`}>   
                                                {foldersLoad ? <Loading className='h-4' color={twColors.slate[600]}/>:<>
                                                <RiAddLine className='w-4'/>
                                                New Folder
                                                </>}
                                            </button>
                                        </div>
                                    )}
                                </div>
                                <div className='flex justify-center items-center'>
                                    <div>
                                        <RiFolderLine className='w-15 h-15 text-slate-400'/>
                                    </div>
                                </div>
                            </div>
                        </motion.div>
                    </motion.div>
                )}
            </AnimatePresence>
    )
}

const grabClass = "w-1 h-1 bg-slate-400"

const FolderItem = ({
    folder,
    disabled,
    expanded,
    onExpand,
    onUpdate,
    onDelete,
}:{
    folder: Folder,
    disabled: boolean,
    expanded: boolean,
    onExpand: () => void,
    onUpdate: (updates: Partial<Folder>) => void,
    onDelete: () => void,
}) => {
    const { showError } = useError();
    const [color, setColor] = React.useState<string>(folder.color);
    const [title, setTitle] = React.useState<string>(folder.title);
    const [saveLoad, setSaveLoad] = React.useState<boolean>(false);
    const [deleteLoad, setDeleteLoad] = React.useState<boolean>(false);

    const { attributes, listeners, setNodeRef, transform, transition } =
    useSortable({ id: folder.id, disabled: disabled });

    const style = {
        transform: CSS.Transform.toString(transform),
        transition,
    };

    return (
    <div
      ref={setNodeRef}
      style={style}
      className="bg-slate-200 rounded-lg shadow-sm"
    >
      <div className="flex items-center h-12 px-4 gap-3 select-none">
        <div
          {...attributes}
          {...listeners}
          className="grid grid-cols-2 gap-1 shrink-0"
        >
            <div className='space-y-1'>
                <div className={grabClass}/>
                <div className={grabClass}/>
                <div className={grabClass}/>
            </div>
            <div className='space-y-1'>
                <div className={grabClass}/>
                <div className={grabClass}/>
                <div className={grabClass}/>
            </div>
        </div>
        <div
          className="flex-1 px-2 py-1 cursor-pointer flex justify-between gap-1 items-center"
          onClick={onExpand}
        >
            <span className='text-lg text-slate-600 truncate'>{folder.title}</span>
            <div
             className="w-5 h-5 rounded-full shadow-sm shrink-0"
             style={{ backgroundColor: `${folder.color}` }}
             />
        </div>
      </div>

      <AnimatePresence>
        {expanded && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: "auto", opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            transition={{ type: "spring", duration: .3 }}
            style={{ overflow: "visible" }}
            onAnimationStart={(def) => {
                if (def === "animate" || def === "exit") {
                (def as any).style = { overflow: "hidden" };
                }
            }}
            onAnimationComplete={(def) => {
                (def as any).style = { overflow: "visible" };
            }}
            className=" bg-slate-100 space-y-3"
          >
            <div className='p-3 overflow-visible'>
                <div className='flex gap-3 items-center'>
                    <ColorInput color={color} setColor={setColor}/>
                    <input
                    type="text"
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    placeholder='Folder Name'
                    className="outline-none border-b border-slate-300 focus:border-slate-400 transition px-2 py-1 placeholder:text-slate-400 text-slate-600"
                    />
                </div>
                <div className='flex justify-between gap-4 flex-wrap items-center mt-4'>
                    <div className='flex gap-2 relative'>
                        <button
                         onClick={async() => {
                            if (!saveLoad){
                                try {
                                    setSaveLoad(true);
                                    const data = {
                                        ...(title !== folder.title && {title: title}),
                                        ...(color !== folder.color && {color: color})
                                    }
                                    const nf: Folder = await Call(`/folders/${folder.id}`, "PATCH", data)
                                    onUpdate(nf);
                                } catch (err) {
                                    if (err instanceof APIError){
                                        showError(err.message, err.body.message)
                                    } else {
                                        showError("Client Error", `${err}`)
                                    }
                                } finally {
                                    setSaveLoad(false);
                                }
                            }
                         }}
                         className={`px-3 py-2 w-33 flex justify-center items-center bg-blue-500 text-slate-50 hover:bg-blue-600 ripple rounded-lg cursor-pointer`}
                        >
                            {saveLoad ? <Loading className='h-5'/>:"Save Changes"}
                        </button>
                        <button
                         onClick={() => {
                            setTitle(folder.title);
                            setColor(folder.color);
                         }}
                         className='px-3 py-2 bg-slate-300 hover:bg-slate-400 text-slate-700 ripple rounded-lg cursor-pointer'
                        >
                            Reset
                        </button>
                        <div className={`bg-slate-200 absolute inset-0 transition ${(folder.color === color && folder.title === title) ? "opacity-60 visible":"invisible opacity-0"}`}/>
                    </div>
                    <button
                     onClick={async() => {
                        if (!deleteLoad){
                            try {
                                setDeleteLoad(true);
                                await Call(`/folders/${folder.id}`, "DELETE", undefined, true)
                                onDelete()
                            } catch (err) {
                                if (err instanceof APIError){
                                    showError(err.message, err.body.message)
                                } else {
                                    showError("Client Error", `${err}`)
                                }
                            } finally {
                                setDeleteLoad(false);
                            }
                        }
                     }}
                     className='px-3 py-2 w-20 transition bg-red-400 hover:bg-red-500 ripple text-slate-50 rounded-lg cursor-pointer'
                     >
                        {saveLoad ? <Loading className='h-5'/>:"Delete"}
                     </button>
                </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

const ColorInput = ({color, setColor}:{color: string, setColor: React.Dispatch<React.SetStateAction<string>>}) => {
    const [show, setShow] = React.useState<boolean>(false);
    const popupRef = React.useRef<HTMLDivElement>(null);
    
    React.useEffect(() => {
        function handleClickOutside(event: MouseEvent) {
          if (
            show && 
            popupRef.current && 
            !popupRef.current.contains(event.target as Node)
          ) {
            setShow(false);
          }
        }
        document.addEventListener("mousedown", handleClickOutside);
    
        return () => {
          document.removeEventListener("mousedown", handleClickOutside);
        };
    }, [show]);

    return (
        <div className='relative z-2' ref={popupRef}>
            <div className='w-5 h-5 rounded-full border border-slate-300' onClick={() => setShow(!show)} style={{backgroundColor: `${color}`}}/>
            <ColorBox show={show}>
                <ColorPanel color={color} submitColor={(c) => {setColor(c);setShow(false)}}/>
            </ColorBox>
        </div>
    )
}