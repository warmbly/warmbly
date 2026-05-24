import { Loading } from "@/components/loader";
import { useError } from "@/hooks/ErrorProvider";
import type { Tag} from "@/hooks/UserProvider";
import { User, useUser } from "@/hooks/UserProvider";
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
import { RiAddLine, RiCloseLine, RiPriceTag3Line } from "@remixicon/react";
import { AnimatePresence, motion } from "framer-motion";
import React from "react";
import { twColors } from "tailwindv4-colors";
import ColorBox from "../colors/ColorBox";
import ColorPanel from "../colors/ColorPanel";

export default function TagsModal(){
    const { showError } = useError();
    const user = useUser();

    const [tagsLoad, setTagsLoad] = React.useState<boolean>(false);
    const [addTag, setAddTag] = React.useState<string>("");
    const [expandedTagId, setExpandedTagId] = React.useState<string | null>(null);

   const sensors = useSensors(
        useSensor(PointerSensor, {
            activationConstraint: { distance: 5 },
            disabled: tagsLoad,
        })
    );

    function updateTag(id: string, updates: Partial<Tag>) {
        if (user) {
            user.setUser((prev) => prev ? ({...prev, tags: prev.tags.map((t) => (t.id === id ? { ...t, ...updates } : t))}):null);
        }
    }

    function deleteTag(id: string) {
        if (user) {
            user.setUser((prev) => prev ? ({...prev, tags: prev.tags.filter((t) => t.id !== id)}):null);
        }
    }

    return (
        <AnimatePresence>
                {user && user.tagsEdit && (
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
                            <div onClick={() => user.setTagsEdit(false)} className='absolute top-7 right-7 text-slate-500 hover:opacity-70 transition cursor-pointer'>
                                <RiCloseLine className='w-5'/>
                            </div>
                            <div className='grid h-full lg:grid-cols-2 gap-10 overflow-y-scroll md:overflow-y-auto'>
                                <div className='md:overflow-y-scroll'>
                                    <h1 className='text-slate-600 font-inter font-bold text-4xl mb-8'>Email Tags</h1>
                                    <p className='text-slate-500 text-lg font-inter mb-10'>Add or remove tags to organize your email accounts for campaigns. Use descriptive tags to group accounts by purpose, region, or audience (e.g., “VIP Clients”, “EU Outreach”, “Newsletter”).</p>
                                    <DndContext
                                    sensors={sensors}
                                    collisionDetection={closestCenter}
                                    onDragEnd={async(event) => {
                                        const { active, over } = event;
                                        if (active.id !== over?.id) {
                                            const oldIndex = user.user.tags.findIndex((t) => t.id === active.id);
                                            const newIndex = user.user.tags.findIndex((t) => t.id === over?.id);
                                            const newTags = arrayMove(user.user.tags, oldIndex, newIndex).map((t, i) => ({
                                                ...t,
                                                position: i,
                                            }));
                                            const originalTags = user.user.tags;
                                            user.setUser((bef) => bef ? ({...bef, tags: newTags}):null);
                                            try {
                                                setTagsLoad(true);
                                                await Call(`/tags/${active.id}/pos`, "POST", { position: newIndex }, true);
                                            } catch (err) {
                                                user.setUser(prev => prev ? { ...prev, tags: originalTags } : null);
                                                if (err instanceof APIError) {
                                                    showError(err.message, err.body.message)
                                                } else {
                                                    showError("Client Error", `${err}`)
                                                }
                                            } finally {
                                                setTagsLoad(false);
                                            }
                                        }
                                    }}
                                    >
                                        <SortableContext
                                        items={user.user.tags.map((t) => t.id)}
                                        strategy={verticalListSortingStrategy}
                                        >
                                            <div className='space-y-2'>
                                                {user.user.tags.sort((a, b) => a.position - b.position).map((tag, i) => {
                                                    return (<TagItem
                                                        disabled={tagsLoad}
                                                        key={tag.id}
                                                        tag={tag}
                                                        expanded={expandedTagId === tag.id}
                                                        onExpand={() =>
                                                            setExpandedTagId(
                                                                expandedTagId === tag.id ? null : tag.id
                                                            )
                                                        }
                                                        onUpdate={(updates) => updateTag(tag.id, updates)}
                                                        onDelete={() => deleteTag(tag.id)}
                                                        />
                                                    )
                                                })}
                                            </div>
                                        </SortableContext>
                                    </DndContext>
                                    {user.user.tags.length < 50 && (
                                        <div className='flex bg-slate-100 rounded-xl mt-2'>
                                            <input 
                                             type="text"
                                             className='grow py-3 px-4 outline-none placeholder:text-slate-400 text-slate-600'
                                             placeholder='Tag Name'
                                             value={addTag}
                                             onChange={(e) => setAddTag(e.target.value)}
                                             />
                                            <button onClick={async() => {
                                                try {
                                                    setTagsLoad(true);
                                                    const resp: Tag = await Call("/tags", "POST", {title: addTag});
                                                    user.setUser((prev) => prev ? ({...prev, tags: [...prev.tags, resp]}):null);
                                                } catch(err) {
                                                    if (err instanceof APIError) {
                                                        showError(err.message, err.body.message)
                                                    } else {
                                                        showError("Client Error", `${err}`)
                                                    }
                                                } finally {
                                                    setTagsLoad(false);
                                                }
                                            }} className={`shrink-0 ripple w-27 my-1 mr-1 bg-slate-300/70 ${!tagsLoad ? "cursor-pointer hover:bg-slate-300":"cursor-not-allowed"} transition flex items-center justify-center gap-1 px-3 py-2 rounded-lg text-slate-600`}>   
                                                {tagsLoad ? <Loading className='h-4' color={twColors.slate[600]}/>:<>
                                                <RiAddLine className='w-4'/>
                                                New Tag
                                                </>}
                                            </button>
                                        </div>
                                    )}
                                </div>
                                <div className='flex justify-center items-center'>
                                    <div>
                                        <RiPriceTag3Line className='w-15 h-15 text-slate-400'/>
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

const TagItem = ({
    tag,
    disabled,
    expanded,
    onExpand,
    onUpdate,
    onDelete,
}:{
    tag: Tag,
    disabled: boolean,
    expanded: boolean,
    onExpand: () => void,
    onUpdate: (updates: Partial<Tag>) => void,
    onDelete: () => void,
}) => {
    const { showError } = useError();
    const [color, setColor] = React.useState<string>(tag.color);
    const [title, setTitle] = React.useState<string>(tag.title);
    const [saveLoad, setSaveLoad] = React.useState<boolean>(false);
    const [deleteLoad, setDeleteLoad] = React.useState<boolean>(false);

    const { attributes, listeners, setNodeRef, transform, transition } =
    useSortable({ id: tag.id, disabled: disabled });

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
            <span className='text-lg text-slate-600 truncate'>{tag.title}</span>
            <div
             className="w-5 h-5 rounded-full shadow-sm shrink-0"
             style={{ backgroundColor: `${tag.color}` }}
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
                    placeholder='Tag Name'
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
                                        ...(title !== tag.title && {title: title}),
                                        ...(color !== tag.color && {color: color})
                                    }
                                    const t: Tag = await Call(`/tags/${tag.id}`, "PATCH", data)
                                    onUpdate(t);
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
                            setTitle(tag.title);
                            setColor(tag.color);
                         }}
                         className='px-3 py-2 bg-slate-300 hover:bg-slate-400 text-slate-700 ripple rounded-lg cursor-pointer'
                        >
                            Reset
                        </button>
                        <div className={`bg-slate-200 absolute inset-0 transition ${(tag.color === color && tag.title === title) ? "opacity-60 visible":"invisible opacity-0"}`}/>
                    </div>
                    <button
                     onClick={async() => {
                        if (!deleteLoad){
                            try {
                                setDeleteLoad(true);
                                await Call(`/tags/${tag.id}`, "DELETE", undefined, true)
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