"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { HardDrive, Files, UploadCloud, Server, Activity, CheckCircle2, AlertCircle, LogIn, LogOut, User as UserIcon } from "lucide-react";
import { useHealth } from "@/hooks/use-health";
import { useAuth } from "@/hooks/use-auth";
import { Button } from "@/components/ui/button";
import { AuthModal } from "@/components/auth-modal";
import { cn } from "@/lib/utils";

export function Navbar() {
  const pathname = usePathname();
  const { data: health, isLoading, isError } = useHealth();
  const { user, isAuthenticated, logout, isLoggingOut } = useAuth();
  const [authModalOpen, setAuthModalOpen] = React.useState(false);
  const [authModalMode, setAuthModalMode] = React.useState<"login" | "register">("login");

  const navLinks = [
    { href: "/", label: "Dashboard", icon: HardDrive },
    { href: "/files", label: "File Manager", icon: Files },
    { href: "/upload", label: "Upload", icon: UploadCloud },
    { href: "/nodes", label: "Storage Nodes", icon: Server },
  ];

  return (
    <header className="sticky top-0 z-50 border-b border-slate-800 bg-slate-950/80 backdrop-blur-md">
      <div className="mx-auto flex h-16 max-w-7xl items-center justify-between px-4 sm:px-6 lg:px-8">
        {/* Left: Brand */}
        <div className="flex items-center gap-6">
          <Link href="/" className="flex items-center gap-2 group">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-indigo-600/20 border border-indigo-500/30 text-indigo-400 group-hover:bg-indigo-600/30 transition-colors">
              <HardDrive className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <span className="font-bold text-lg text-white tracking-tight">CloudDump</span>
                <span className="rounded bg-indigo-950/90 px-1.5 py-0.5 text-[10px] font-mono text-indigo-300 border border-indigo-800">
                  Pool
                </span>
              </div>
            </div>
          </Link>

          {/* Navigation Links */}
          <nav className="hidden md:flex items-center gap-1">
            {navLinks.map((item) => {
              const Icon = item.icon;
              const isActive =
                item.href === "/"
                  ? pathname === "/"
                  : pathname.startsWith(item.href);

              return (
                <Link
                  key={item.href}
                  href={item.href}
                  className={cn(
                    "flex items-center gap-2 px-3 py-1.5 rounded-md text-sm font-medium transition-colors",
                    isActive
                      ? "bg-slate-800 text-white font-semibold"
                      : "text-slate-400 hover:text-slate-200 hover:bg-slate-900"
                  )}
                >
                  <Icon className={cn("h-4 w-4", isActive ? "text-indigo-400" : "text-slate-500")} />
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>

        {/* Right: User Auth & Backend Status */}
        <div className="flex items-center gap-3">
          {/* Backend Health Badge */}
          <div className="hidden sm:flex items-center gap-2 rounded-full border border-slate-800 bg-slate-900/90 px-3 py-1 text-xs">
            {isLoading ? (
              <>
                <Activity className="h-3.5 w-3.5 text-amber-400 animate-spin" />
                <span className="text-slate-400">Connecting...</span>
              </>
            ) : isError ? (
              <>
                <AlertCircle className="h-3.5 w-3.5 text-rose-400" />
                <span className="text-rose-400 font-medium">Backend Offline</span>
              </>
            ) : (
              <>
                <span className="relative flex h-2 w-2">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                  <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                </span>
                <span className="text-emerald-400 font-medium flex items-center gap-1">
                  <CheckCircle2 className="h-3.5 w-3.5" />
                  API Online
                </span>
              </>
            )}
          </div>

          {/* Auth Controls */}
          {isAuthenticated && user ? (
            <div className="flex items-center gap-2.5">
              <div className="flex items-center gap-1.5 rounded-lg border border-slate-800 bg-slate-900 px-2.5 py-1 text-xs">
                <UserIcon className="h-3.5 w-3.5 text-indigo-400" />
                <span className="font-medium text-slate-200 truncate max-w-[140px]" title={user.email}>
                  {user.name || user.email}
                </span>
              </div>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => logout()}
                disabled={isLoggingOut}
                className="h-8 px-2.5 text-xs text-slate-400 hover:text-rose-400 flex items-center gap-1"
                title="Log out"
              >
                <LogOut className="h-3.5 w-3.5" />
                <span className="hidden sm:inline">Logout</span>
              </Button>
            </div>
          ) : (
            <div className="flex items-center gap-1.5">
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  setAuthModalMode("login");
                  setAuthModalOpen(true);
                }}
                className="h-8 px-3 text-xs flex items-center gap-1.5 text-slate-200 border-slate-700"
              >
                <LogIn className="h-3.5 w-3.5 text-indigo-400" />
                <span>Log In</span>
              </Button>
              <Button
                size="sm"
                onClick={() => {
                  setAuthModalMode("register");
                  setAuthModalOpen(true);
                }}
                className="h-8 px-3 text-xs hidden sm:inline-flex bg-indigo-600 hover:bg-indigo-500"
              >
                Sign Up
              </Button>
            </div>
          )}
        </div>
      </div>

      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
        defaultMode={authModalMode}
      />
    </header>
  );
}
