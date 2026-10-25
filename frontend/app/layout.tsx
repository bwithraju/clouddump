import type { Metadata } from "next";
import "./globals.css";
import { Providers } from "@/components/providers";
import { Navbar } from "@/components/navbar";

export const metadata: Metadata = {
  title: "CloudDump | Distributed Cloud Storage",
  description:
    "A distributed storage pool combining heterogeneous storage nodes into one logical drive.",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en" className="dark">
      <body className="min-h-screen bg-slate-950 text-slate-100 antialiased font-sans flex flex-col selection:bg-indigo-500/20 selection:text-indigo-300">
        <Providers>
          <Navbar />
          <main className="flex-1 max-w-7xl w-full mx-auto px-4 sm:px-6 lg:px-8 py-8">
            {children}
          </main>
          <footer className="border-t border-slate-900 bg-slate-950/50 py-6 text-center text-xs text-slate-500">
            CloudDump · Production-Grade Heterogeneous Cloud Storage Pool
          </footer>
        </Providers>
      </body>
    </html>
  );
}
