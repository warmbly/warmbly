import React, { useState, useEffect } from 'react';
import type { Editor} from '@tiptap/react';
import { EditorContent, mergeAttributes, Node as TNode, useEditor } from '@tiptap/react';
import Text from '@tiptap/extension-text';
import Document from '@tiptap/extension-document';
import Bold from '@tiptap/extension-bold';
import Italic from '@tiptap/extension-italic';
import Underline from '@tiptap/extension-underline';
import Link from '@tiptap/extension-link';
import Image from '@tiptap/extension-image'
import { FontSize, TextStyle } from '@tiptap/extension-text-style'
import Color from '@tiptap/extension-color'
import Heading, { type Level } from '@tiptap/extension-heading'
import { BulletList } from '@tiptap/extension-list/bullet-list'
import { OrderedList } from '@tiptap/extension-list/ordered-list'
import ListItem from '@tiptap/extension-list-item'
import Strike from '@tiptap/extension-strike'
import Subscript from '@tiptap/extension-subscript'
import Superscript from '@tiptap/extension-superscript'
import { RiBold, RiCodeLine, RiFontColor, RiFontFamily, RiH1, RiH2, RiH3, RiH4, RiH5, RiH6, RiImageAddLine, RiItalic, RiLink, RiListOrdered2, RiListUnordered, RiPaletteLine, RiParagraph, RiSortAlphabetAsc, RiSortAlphabetDesc, RiStrikethrough, RiSubscript, RiSuperscript, RiText, RiUnderline } from '@remixicon/react';
import { useLink } from '@/hooks/context/link';
import MiniInput from './popup/MiniInput';
import clsx from 'clsx';
import Paragraph from '@tiptap/extension-paragraph';
import { AnimatePresence, motion } from "framer-motion";
import ColorBox from './colors/ColorBox';
import ColorPanel from './colors/ColorPanel';
import Highlight from '@tiptap/extension-highlight';
import Switch from './Switch';

const CustomListItem = ListItem.extend({
  content: '(paragraph | paragraphDiv) block*',
})


function stripHtml(html: string) {
  const div = document.createElement('div');
  div.innerHTML = html;
  div.querySelectorAll('div').forEach(block => {
    block.insertAdjacentText('afterend', '\n');
  });
  return div.innerText.trim();
}

const ParagraphDiv = TNode.create({
  name: 'paragraphDiv',

  priority: 1000,

  group: 'block',

  content: 'inline*',

  parseHTML() {
    return [
      { tag: 'div' },
    ]
  },

  renderHTML({ HTMLAttributes }) {
    return ['div', mergeAttributes(this.options.HTMLAttributes, HTMLAttributes), 0]
  },

  addCommands() {
    return {
      setParagraph: () => ({ commands }) => {
        return commands.toggleNode('paragraphDiv', 'paragraphDiv')
      },
    }
  },
});

interface EmailEditorProps {
  id: string,
  htmlText: string;
  setHtmlText: (value: string) => void;
  plainText: string;
  setPlainText: (value: string) => void;
  sync: boolean;
  setSync: (value: boolean) => void;
  code: boolean;
  setCode: (value: boolean) => void;
}

export default function EmailEditor({ id, htmlText, setHtmlText, plainText, setPlainText, sync, setSync, code, setCode }: EmailEditorProps) {
  const link = useLink();
  const [activeTab, setActiveTab] = useState<'html' | 'plain'>('html');
  const [more, setMore] = useState<boolean>(false);

  const editor = useEditor({
    extensions: [
      Document, Text, Bold, Italic, Underline,
      Link, ParagraphDiv, Image, TextStyle, Color, Paragraph,
      FontSize, BulletList, OrderedList, CustomListItem, Strike,
      Subscript, Superscript, Highlight,
      Heading.configure({
        levels: [1, 2, 3, 4, 5, 6],
      })
    ],
    content: htmlText,
    onUpdate: ({ editor }) => {
      console.log('onUpdate triggered', editor.getHTML());
      const newHtml = editor.getHTML();
      setHtmlText(newHtml);
      if (sync && activeTab == "html") {
        setPlainText(stripHtml(newHtml));
      }
    },
    editorProps: {
      attributes: {
        class:
          'prose prose-sm max-w-none min-h-[208px] focus:outline-none p-4',
      },
    },
    immediatelyRender: false,
  });

  useEffect(() => {
    if (editor && activeTab === 'html' && !sync) {
      if (editor.getHTML() !== htmlText) {
        editor.commands.setContent(htmlText);
      }
    }
  }, [htmlText, sync, activeTab, editor]);


  function toggleHtmlMode() {
    setCode(!code)
    if (!code) {
      editor?.commands.setContent(htmlText)
    }
  }

  return (
    <div className="w-full mx-auto bg-white border border-gray-200 rounded-lg shadow-sm flex flex-col">
      <div className="flex border-b border-gray-200 text-sm font-medium justify-between">
        <div>
          <button
            onClick={() => setActiveTab('html')}
            className={`px-4 py-2 transition-all border-b-2 ${activeTab === 'html' ? 'text-blue-500 border-blue-500' : 'border-transparent text-gray-500'}`}
          >
            HTML
          </button>
          <button
            onClick={() => setActiveTab('plain')}
            className={`px-4 py-2 transition-all border-b-2 ${activeTab === 'plain' ? 'text-blue-500 border-blue-500' : 'border-transparent text-gray-500'}`}
          >
            Plain Text
          </button>
        </div>
        <div className='px-4 flex items-center gap-3'>
          <span className='text-gray-500'>Sync</span>
          <Switch
            id={id}
            value={sync}
            onChange={setSync}
          />
        </div>
      </div>

      <div className="flex-1 min-h-[200px] bg-gray-50">
        {activeTab === 'html' && editor && (
          !code ?
            <EditorContent editor={editor} /> : <TextArea
              value={htmlText}
              onChange={(e) => setHtmlText(e.target.value)}
            />
        )}
        {activeTab === 'plain' && (
          <TextArea
            value={plainText}
            onChange={(e) => setPlainText(e.target.value)}
          />
        )}
      </div>

      {editor && (
        <div className={`flex flex-col no-scrollbar gap-1 relative bg-gray-50 border-gray-200 p-1 text-sm transition`}>
          <AnimatedMenu more={more}>
            <TextType editor={editor} title='Text Type' />
            <ToolButton
              title='Increase Size'
              active={false}
              onClick={() => {
                const currentSize = parseInt(editor.getAttributes('textStyle').fontSize || '16', 10)
                editor.chain().focus().setFontSize(`${currentSize + 2}px`).run()
              }}
            >
              <RiSortAlphabetDesc className='w-4' />
            </ToolButton>
            <ToolButton
              title='Increase Size'
              active={false}
              onClick={() => {
                const currentSize = parseInt(editor.getAttributes('textStyle').fontSize || '16', 10)
                editor.chain().focus().setFontSize(`${Math.max(currentSize - 2, 8)}px`).run()
              }}
            >
              <RiSortAlphabetAsc className='w-4' />
            </ToolButton>
            <ToolButton
              title='Bullet List'
              active={editor.isActive('bulletList')}
              onClick={() => editor.chain().focus().toggleBulletList().run()}
            >
              <RiListUnordered className='w-4' />
            </ToolButton>
            <ToolButton
              title='Ordered List'
              active={editor.isActive('orderedList')}
              onClick={() => editor.chain().focus().toggleOrderedList().run()}
            >
              <RiListOrdered2 className='w-4' />
            </ToolButton>
            <ToolButton
              title='Strikethrough'
              active={editor.isActive('strike')}
              onClick={() => editor.chain().focus().toggleStrike().run()}
            >
              <RiStrikethrough className='w-4' />
            </ToolButton>
            <ToolButton
              title='Subscript'
              active={editor.isActive('subscript')}
              onClick={() => editor.chain().focus().toggleSubscript().run()}
            >
              <RiSubscript className='w-4' />
            </ToolButton>
            <ToolButton
              title='Superscript'
              active={editor.isActive('superscript')}
              onClick={() => editor.chain().focus().toggleSuperscript().run()}
            >
              <RiSuperscript className='w-4' />
            </ToolButton>
          </AnimatedMenu>
          <div className='flex gap-1 h-8'>
            <ToolButton
              title='Bold'
              active={editor.isActive('bold')}
              onClick={() => editor.chain().focus().toggleBold().run()}
            >
              <RiBold className='w-4' />
            </ToolButton>
            <ToolButton
              title='Italic'
              onClick={() => editor.chain().focus().toggleItalic().run()}
              active={editor.isActive('italic')}
            >
              <RiItalic className='w-4' />
            </ToolButton>
            <ToolButton
              title='Underline'
              onClick={() => editor.chain().focus().toggleUnderline().run()}
              active={editor.isActive('underline')}
            >
              <RiUnderline className='w-4' />
            </ToolButton>
            <ColorInput title='Text Color' color={editor.getAttributes('textStyle').color || '#000000'} onSubmit={(c) => {
              if (c === "#000000") {
                editor.chain().focus().unsetColor().run();
              } else {
                editor.chain().focus().setColor(c).run();
              }
            }}>
              <RiFontColor className='w-4' />
            </ColorInput>
            <ColorInput title='Text Highlight' color={editor.getAttributes('highlight').color || '#000000'} onSubmit={(c) => {
              if (c === "#000000") {
                editor.chain().focus().unsetHighlight().run();
              } else {
                editor.chain().focus().setHighlight({ color: c }).run();
              }
            }}>
              <RiPaletteLine className='w-4' />
            </ColorInput>
            <ToolButton
              title='Insert Link'
              onClick={() => {
                const selectedText = editor.state.doc.textBetween(
                  editor.view.state.selection.from,
                  editor.view.state.selection.to,
                  ""
                );
                link?.show(selectedText, (displayName, url) => {
                  if (!url?.trim()) return;

                  const ed = editor.chain().focus();

                  if (displayName?.trim()) {
                    ed
                      .deleteSelection()
                      .insertContentAt(
                        editor.state.selection.from,
                        [
                          {
                            type: "text",
                            text: displayName.trim(),
                            marks: [
                              { type: "link", attrs: { href: url.trim() } }
                            ],
                          }
                        ]
                      )
                      .run();
                  } else {
                    ed.setLink({ href: url.trim() }).run();
                  }
                });
              }}
              active={editor.isActive('link')}
            >
              <RiLink className='w-4' />
            </ToolButton>
            <ToolButton
              title='Text Menu'
              onClick={() => setMore(bef => !bef)}
              active={more}
            >
              <RiFontFamily className='w-4' />
            </ToolButton>
            <ImageImport editor={editor} />
            <div className={clsx("ml-auto", activeTab === "html" && "z-2")}>
              <ToolButton
                title='Code View'
                onClick={() => toggleHtmlMode()}
                active={code}
              >
                <RiCodeLine className='w-4' />
              </ToolButton>
            </div>
          </div>
          <div className={`absolute inset-0 bg-gray-50/60 transition ${(activeTab !== "html" || code) ? "visible opacity-100" : "invisible opacity-30"}`} />
        </div>
      )}
    </div>
  );
}

function ColorInput({ onSubmit, title, children, color }: { onSubmit: (color: string) => void, color: string, title: string, children: React.ReactNode }) {
  const [show, setShow] = React.useState<boolean>(false)
  const popupRef = React.useRef<HTMLDivElement>(null);

  useEffect(() => {
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

  const submit = (c: string) => {
    onSubmit(c);
    setShow(false);
  }

  return <div className='relative' ref={popupRef}>
    <ToolButton
      onClick={() => setShow(!show)}
      title={title}
      active={show}
    >
      {children}
    </ToolButton>
    <ColorBox show={show} top>
      <ColorPanel color={color} submitColor={submit} />
    </ColorBox>
  </div>
}

function TextType({ editor, title }: { editor: Editor, title?: string }) {
  const [showTooltip, setShowTooltip] = useState(false);
  const [show, setShow] = React.useState<boolean>(false)
  const popupRef = React.useRef<HTMLDivElement>(null);

  useEffect(() => {
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

  let currentType = "Default";
  if (editor.isActive("heading", { level: 1 })) currentType = "Heading 1";
  else if (editor.isActive("heading", { level: 2 })) currentType = "Heading 2";
  else if (editor.isActive("heading", { level: 3 })) currentType = "Heading 3";
  else if (editor.isActive("heading", { level: 4 })) currentType = "Heading 4";
  else if (editor.isActive("heading", { level: 5 })) currentType = "Heading 5";
  else if (editor.isActive("heading", { level: 6 })) currentType = "Heading 6";
  else if (editor.isActive("paragraph")) currentType = "Paragraph";

  function GetIcon(name: string) {
    switch (name) {
      case "Heading 1":
        return <RiH1 className='w-4' />
      case "Heading 2":
        return <RiH2 className='w-4' />
      case "Heading 3":
        return <RiH3 className='w-4' />
      case "Heading 4":
        return <RiH4 className='w-4' />
      case "Heading 5":
        return <RiH5 className='w-4' />
      case "Heading 6":
        return <RiH6 className='w-4' />
      case "Paragraph":
        return <RiParagraph className='w-4' />
      default:
        return <RiText className='w-4' />
    }
  }

  const setType = (type: string) => {
    setShow(false);
    if (type === "Default") {
      editor.chain().focus().unsetAllMarks().clearNodes().run();
    } else if (type === "Paragraph") {
      editor.chain().focus().setParagraph().run();
    } else if (type.startsWith("Heading")) {
      const level = parseInt(type.slice(-1), 10) as Level;
      if (!isNaN(level)) {
        editor.chain().focus().toggleHeading({ level }).run();
      }
    }
  };

  return <div className='relative w-32' ref={popupRef}>
    <button className={`ripple border-r border-gray-200 px-3 py-1 w-full flex gap-2 items-center text-md cursor-pointer transition rounded-md ${show ? "bg-gray-200" : "hover:bg-gray-200/60"
      }`}
      onClick={() => {
        setShow(!show);
        setShowTooltip(false);
      }}
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
    >
      {GetIcon(currentType)}
      {currentType}
    </button>
    <AnimatePresence>
      {show && (
        <motion.div
          className="absolute overflow-x-scroll w-full max-h-30 max-w-4xl p-0 shadow-sm border border-gray-200/60 bg-white rounded-md bottom-full mb-2"
          transition={{ type: "spring", duration: 0.3, bounce: 0.3 }}
          initial={{ opacity: 0, scale: 0.6 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, scale: 0.6 }}
        >

          {["Default", "Paragraph", "Heading 1", "Heading 2", "Heading 3", "Heading 4", "Heading 5", "Heading 6"].map((type) => (
            <TextTypeButton
              key={type}
              onClick={() => setType(type)}
            >
              {GetIcon(type)}
              {type}
            </TextTypeButton>
          ))}
        </motion.div>
      )}
    </AnimatePresence>
    {title && <ToolTip show={showTooltip}>{title}</ToolTip>}
  </div>
}

function TextTypeButton({ children, onClick }: { children: React.ReactNode, onClick: () => void }) {
  return <button
    className='ripple hover:bg-gray-100 px-3 py-1 flex gap-3 w-full cursor-pointer'
    onClick={onClick}
  >
    {children}
  </button>
}

function AnimatedMenu({ more, children }: { more: boolean, children: React.ReactNode }) {
  const [hideOverflow, setHideOverflow] = React.useState(true);
  const containerRef = React.useRef<HTMLDivElement>(null);

  React.useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    function handleTransitionEnd(e: TransitionEvent) {
      if (e.propertyName === "height" && more) {
        setHideOverflow(false);
      }
    }

    el.addEventListener("transitionend", handleTransitionEnd);
    return () => el.removeEventListener("transitionend", handleTransitionEnd);
  }, [more]);

  React.useEffect(() => {
    if (!more) {
      setHideOverflow(true);
    }
  }, [more]);

  return (
    <div
      ref={containerRef}
      className={`flex gap-1 transition-all ${more ? "h-8" : "h-0"}${hideOverflow ? " overflow-y-hidden" : ""
        }`}
    >
      {children}
    </div>
  );
}


function TextArea({ value, onChange }: { value: string, onChange: (e: React.ChangeEvent<HTMLTextAreaElement>) => void }) {
  const textareaRef = React.useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
      const minHeight = 201;
      textareaRef.current.style.minHeight = minHeight + "px";
      textareaRef.current.style.height =
        Math.max(textareaRef.current.scrollHeight, minHeight) + "px";
    }
  }, [value]);

  return (
    <textarea
      ref={textareaRef}
      value={value}
      onChange={onChange}
      rows={1}
      className='p-4 font-mono text-sm outline-none resize-none min-h-full '
      style={{
        resize: "none",
        overflow: "hidden",
        width: "100%",
      }}
    />
  );
}


function ImageImport({ editor }: { editor: Editor | null }) {
  const [image, setImage] = React.useState<boolean>(false);
  const [url, setUrl] = React.useState<string>("");
  const popupRef = React.useRef<HTMLDivElement>(null);

  const submit = () => {
    editor?.chain().focus().setImage({ src: url }).run()
    setImage(false)
  }

  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (
        image &&
        popupRef.current &&
        !popupRef.current.contains(event.target as Node)
      ) {
        setImage(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);

    return () => {
      document.removeEventListener("mousedown", handleClickOutside);
    };
  }, [image]);

  return (<div className='relative' ref={popupRef}>
    <ToolButton
      title={image ? undefined : 'Image'}
      onClick={() => setImage(true)}
      active={image}
    >
      <RiImageAddLine className='w-4' />
    </ToolButton>
    <AnimatePresence>
      {image && (
        <motion.div
          className="absolute overflow-x-scroll max-w-4xl p-2 shadow-sm border border-gray-200/60 bg-white rounded-md bottom-full mb-2"
          transition={{ type: "spring", duration: 0.3, bounce: 0.3 }}
          initial={{ opacity: 0, scale: 0.6 }}
          animate={{ opacity: 1, scale: 1 }}
          exit={{ opacity: 0, scale: 0.6 }}
        >
          <MiniInput placeholder='https://example.com' value={url} onChange={(e) => setUrl(e.target.value)} />
          <div className="flex justify-end mt-2 gap-2 w-70">
            <button onClick={submit} className="ripple bg-blue-100 transition hover:bg-blue-200/80 text-blue-500 py-2.5 p-8 rounded-md cursor-pointer">
              Submit
            </button>
          </div>
        </motion.div>
      )}
    </AnimatePresence>
  </div>)
}

function ToolButton({
  active,
  onClick,
  children,
  title,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
  title?: string;
}) {
  const [showTooltip, setShowTooltip] = useState(false);

  return (
    <div
      className="relative inline-block"
    >
      <button
        onMouseEnter={() => setShowTooltip(true)}
        onMouseLeave={() => setShowTooltip(false)}
        onClick={() => {
          onClick();
          setShowTooltip(false);
        }}
        title={title}
        className={`ripple px-3 py-1 cursor-pointer transition rounded-md ${active ? "bg-gray-200" : "hover:bg-gray-200/60"
          }`}
      >
        {children}
      </button>
      {title && <ToolTip show={showTooltip}>{title}</ToolTip>}
    </div>
  );
}

function ToolTip({ show, children }: { show: boolean, children: React.ReactNode }) {
  return <AnimatePresence>
    {show &&
      <motion.div
        className="absolute bottom-full mb-2 left-1/2 -translate-x-1/2 whitespace-nowrap rounded bg-gray-800 py-1.5 px-2 text-gray-200 shadow-lg z-50 select-none"
        transition={{ type: "spring", duration: 0.3, bounce: 0.3 }}
        initial={{ opacity: 0, scale: 0.6 }}
        animate={{ opacity: 1, scale: 1 }}
        exit={{ opacity: 0, scale: 0.6 }}
      >
        {children}
        <div className="absolute top-full left-1/2 -translate-y-1 -translate-x-1/2 w-2 h-2 bg-gray-800 rotate-45"></div>
      </motion.div>}
  </AnimatePresence>
}
