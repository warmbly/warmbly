import React from "react";

const MiniTextArea = ({
  onChange,
  placeholder,
  value,
  id,
  name,
  disabled,
}: {
  onChange: (e: React.ChangeEvent<HTMLTextAreaElement>) => void;
  placeholder?: string;
  value?: string;
  id?: string;
  name?: string;
  disabled?: boolean;
}) => {
  const textareaRef = React.useRef<HTMLTextAreaElement | null>(null);

  React.useEffect(() => {
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
      const minHeight = textareaRef.current.scrollHeight / textareaRef.current.rows * 3;
      textareaRef.current.style.height = Math.max(minHeight, textareaRef.current.scrollHeight) + "px";
    }
  }, [value]);

  return (
    <textarea
      ref={textareaRef}
      rows={3}
      className="w-full no-scrollbar font-sans text-[15px] bg-transparent placeholder:text-slate-400 text-slate-700 border border-slate-200 rounded-md px-3 py-2.5 transition duration-300 ease focus:outline-none focus:border-slate-300 shadow-sm focus:shadow resize-none"
      onChange={disabled ? undefined:onChange}
      placeholder={placeholder}
      value={disabled ? undefined:value}
      id={id}
      name={name}
      disabled={disabled}
    />
  );
};

export default MiniTextArea;