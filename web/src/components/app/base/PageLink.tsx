import { useLocation } from "react-router-dom";
import { Link } from "react-router-dom";

export default function PageLink({ children, href }: { children: React.ReactNode, href: string }) {
    const { pathname } = useLocation();

    return <Link to={href} className={`ripple w-[calc(100%+30px)] -translate-x-[15px] px-[15px] py-2 my-0.5 font-sans text-lg flex items-center gap-7 rounded-lg text-gray-600 transition-all duration-500 ${pathname.startsWith(href) ? "text-indigo-500 bg-indigo-100" : "hover:text-gray-900 hover:bg-gray-50"}`}>
        {children}
    </Link>
}
