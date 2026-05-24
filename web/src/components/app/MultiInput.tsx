import { RiCloseLine } from '@remixicon/react';
import React, { useState, useRef } from 'react';

interface MultiInputProps {
  values: string[];
  onChange: (values: string[]) => void;
  placeholder?: string;
  className?: string;
}

const MultiInput: React.FC<MultiInputProps> = ({
  values,
  onChange,
  placeholder = "Type and press Tab...",
  className = ""
}) => {
  const [inputValue, setInputValue] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Tab' && inputValue.trim()) {
      e.preventDefault();

      const newValues = [...values, inputValue.trim()];
      onChange(newValues);

      setInputValue('');
    } else if (e.key === 'Backspace' && inputValue === '' && values.length > 0) {
      e.preventDefault();

      const newValues = values.slice(0, -1);
      onChange(newValues);
    }
  };

  const removeValue = (index: number) => {
    const newValues = values.filter((_, i) => i !== index);
    onChange(newValues);
  };

  return (
    <div className={`flex shadow-sm flex-wrap items-center gap-1 p-2.5 border transition border-gray-200 focus-within:border-gray-300 rounded-lg ${className}`}>
      {values.map((value, index) => (
        <span
          key={index}
          className="inline-flex max-w-[calc(100%-5rem)] items-center gap-1 px-2 py-0.5 bg-blue-100 text-blue-800 rounded-lg text-sm"
        >
          <span className='truncate'>{value}</span>
          <button
            type="button"
            onClick={() => removeValue(index)}
            className="hover:bg-blue-200 cursor-pointer rounded-full shrink-0 p-0.5 transition-colors"
          >
            <RiCloseLine className='w-3 h-3' />
          </button>
        </span>
      ))}

      <input
        ref={inputRef}
        type="text"
        value={inputValue}
        onChange={(e) => setInputValue(e.target.value)}
        onKeyDown={handleKeyDown}
        placeholder={values.length === 0 ? placeholder : ""}
        className="flex-1 text-slate-700 font-sans placeholder:text-slate-400 min-w-[100px] outline-none bg-transparent"
      />
    </div>
  );
};
export default MultiInput;
