"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { HardDrive, Files, UploadCloud, Server, LogIn, LogOut } from "lucide-react";
import { useAuth } from "@/hooks/use-auth";
import { AuthModal } from "@/components/auth-modal";

export function Navbar() {
  const pathname = usePathname();
  const { user, isAuthenticated, logout } = useAuth();
  const [authModalOpen, setAuthModalOpen] = React.useState(false);

  const navLinks = [
    { href: "/", label: "Dashboard", icon: HardDrive },
    { href: "/files", label: "My files", icon: Files },
    { href: "/upload", label: "Upload", icon: UploadCloud },
    { href: "/nodes", label: "Nodes & Storage", icon: Server },
  ];

  return (
    <>
      <header className="sticky top-0 z-40 border-b border-gray-200/80 bg-white/95 backdrop-blur-xs">
        <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
          <div className="flex items-center gap-6">
            <Link href="/" className="flex items-center gap-2.5">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-indigo-600 text-white shadow-xs">
                <HardDrive className="h-4 w-4" />
              </div>
              <span className="font-bold text-lg text-slate-900 tracking-tight">CloudDump</span>
            </Link>

            <nav className="hidden md:flex items-center gap-1">
              {navLinks.map((item) => {
                const Icon = item.icon;
                const isActive =
                  item.href === "/"
                    ? pathname === "/"
                    : pathname === item.href || pathname.startsWith(`${item.href}/`);
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    className={`flex items-center gap-2 rounded-lg px-3 py-1.5 text-xs font-medium transition-colors ${
                      isActive
                        ? "bg-gray-100 text-slate-900 font-semibold"
                        : "text-gray-600 hover:bg-gray-50 hover:text-slate-900"
                    }`}
                  >
                    <Icon className="h-3.5 w-3.5" />
                    <span>{item.label}</span>
                  </Link>
                );
              })}
            </nav>
          </div>

          <div className="flex items-center gap-3">
            {isAuthenticated && user ? (
              <div className="flex items-center gap-2 text-xs">
                <span className="text-gray-600">{user.email}</span>
                <button
                  type="button"
                  onClick={() => logout()}
                  className="rounded-lg p-1.5 text-gray-400 hover:text-slate-900"
                >
                  <LogOut className="h-4 w-4" />
                </button>
              </div>
            ) : (
              <button
                type="button"
                onClick={() => setAuthModalOpen(true)}
                className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-800 shadow-xs"
              >
                Sign In
              </button>
            )}
          </div>
        </div>
      </header>

      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
        defaultMode="login"
      />
    </>
  );
}
