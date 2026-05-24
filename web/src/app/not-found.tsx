import Link from "next/link";

export default function NotFound() {
  return (
    <div className="w-full py-30 flex items-center px-4 justify-center">
        <div className="shadow-lg p-7 max-w-lg w-full bg-white flex flex-col items-center">
            <h1 className="font-bold mb-4 text-lg">404 Error</h1>
            <p className="mb-6 text-md">Page not found.</p>
            <Link href="/" className="text-white px-3 py-1.5 bg-blue-500 rounded-lg hover:bg-blue-600 transition-all">Go back</Link>
        </div>
    </div>
  );
}