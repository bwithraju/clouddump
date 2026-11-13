import type { Metadata } from "next";
import "./globals.css";
import { Providers } from "@/components/providers";
import { AppLayout } from "@/components/app-layout";

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
    <html lang="en" suppressHydrationWarning>
      <body className="min-h-screen bg-[#f8f9fb] text-slate-900 antialiased font-sans selection:bg-indigo-500/20 selection:text-indigo-900">
        <Providers>
          <AppLayout>{children}</AppLayout>
        </Providers>
      </body>
    </html>
  );
}
