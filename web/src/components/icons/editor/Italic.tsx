const Italic = ({ size = 16, color = "currentColor" }) => (
    <svg
        xmlns="http://www.w3.org/2000/svg"
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke={color}
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
    >
        <path d="M19 4h-9M14 20H5M14.7 4.7L9.2 19.4" />
    </svg>
);
export default Italic;
