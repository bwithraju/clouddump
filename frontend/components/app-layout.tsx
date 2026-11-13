"use client";

import * as React from "react";
import { Sidebar } from "@/components/sidebar";
import { Header } from "@/components/header";

export function AppLayout({ children }: { children: React.ReactNode }) {
  const [mobileMenuOpen, setMobileMenuOpen] = React.useState(false);

  return (
    <div className="min-h-screen bg-[#f8f9fb] text-slate-900 antialiased font-sans flex">
      {/* Sidebar - fixed left on desktop, drawer on mobile */}
      <Sidebar
        mobileOpen={mobileMenuOpen}
        onCloseMobile={() => setMobileMenuOpen(false)}
      />

      {/* Main Content Shell (offset by 240px / w-60 on lg screens) */}
      <div className="flex flex-1 flex-col lg:pl-60 min-w-0 transition-all">
        {/* Top Header */}
        <Header onToggleMobileMenu={() => setMobileMenuOpen(true)} />

        {/* Page Content Canvas */}
        <main className="flex-1 px-4 sm:px-6 lg:px-8 py-6 max-w-7xl w-full mx-auto">
          {children}
        </main>

        {/* Minimal Clean Footer */}
        <footer className="border-t border-gray-200/80 bg-white py-4 text-center text-xs text-gray-400 select-none">
          CloudDump · Distributed Heterogeneous Cloud Storage Pool
        </footer>
      </div>
    </div>
  );
}
