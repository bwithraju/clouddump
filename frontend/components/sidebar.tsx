"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import {
  LayoutDashboard,
  FolderOpen,
  UploadCloud,
  HardDrive,
  Activity,
  ShieldCheck,
  Sparkles,
  LogIn,
  LogOut,
  X,
  ExternalLink,
} from "lucide-react";
import { useAuth } from "@/hooks/use-auth";
import { usePoolStats } from "@/hooks/use-nodes";
import { formatBytes } from "@/lib/utils";
import { AuthModal } from "@/components/auth-modal";

interface SidebarProps {
  mobileOpen?: boolean;
  onCloseMobile?: () => void;
}

export function Sidebar({ mobileOpen = false, onCloseMobile }: SidebarProps) {
  const pathname = usePathname();
  const { user, isAuthenticated, logout } = useAuth();
  const { data: stats } = usePoolStats();
  const [authModalOpen, setAuthModalOpen] = React.useState(false);

  const totalCapacity = stats?.total_capacity || 0;
  const usedCapacity = stats?.used_capacity || 0;
  const usedPercent = totalCapacity > 0 ? Math.min(100, Math.round((usedCapacity / totalCapacity) * 100)) : 0;

  const navItems = [
    { name: "Dashboard", href: "/", icon: LayoutDashboard },
    { name: "My files", href: "/files", icon: FolderOpen },
    { name: "Upload", href: "/upload", icon: UploadCloud },
    { name: "Nodes & Storage", href: "/nodes", icon: HardDrive },
  ];

  const secondaryItems = [
    { name: "Health Telemetry", href: "/api/health", external: true, icon: ShieldCheck },
  ];

  const sidebarContent = (
    <div className="flex h-full flex-col justify-between bg-white border-r border-gray-200/80 px-4 py-5 select-none">
      <div className="space-y-6">
        {/* Brand / Logo Header */}
        <div className="flex items-center justify-between px-2">
          <Link href="/" className="flex items-center gap-2.5 group">
            {/* BacCloud-style dual square brand mark */}
            <div className="relative flex h-8 w-8 items-center justify-center rounded-lg bg-indigo-600 shadow-sm transition-transform group-hover:scale-105">
              <span className="absolute -top-1 -right-1 h-3 w-3 rounded-sm bg-purple-500 opacity-90" />
              <div className="h-3 w-3 rounded-xs bg-white" />
            </div>
            <span className="text-lg font-bold tracking-tight text-slate-900">
              CloudDump
            </span>
          </Link>

          {/* Close button for mobile drawer */}
          {onCloseMobile && (
            <button
              type="button"
              onClick={onCloseMobile}
              className="lg:hidden rounded-md p-1.5 text-gray-500 hover:bg-gray-100 hover:text-slate-900"
              aria-label="Close menu"
            >
              <X className="h-5 w-5" />
            </button>
          )}
        </div>

        {/* Primary Navigation */}
        <nav className="space-y-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive =
              item.href === "/"
                ? pathname === "/"
                : pathname === item.href || pathname.startsWith(`${item.href}/`);

            return (
              <Link
                key={item.href}
                href={item.href}
                onClick={onCloseMobile}
                className={`flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-all ${
                  isActive
                    ? "bg-gray-100 text-slate-900 font-semibold"
                    : "text-slate-600 hover:bg-gray-50 hover:text-slate-900"
                }`}
              >
                <Icon
                  className={`h-4 w-4 ${
                    isActive ? "text-indigo-600" : "text-slate-400 group-hover:text-slate-600"
                  }`}
                />
                <span>{item.name}</span>
              </Link>
            );
          })}
        </nav>

        {/* System Sub-Section */}
        <div className="pt-2">
          <div className="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-gray-400">
            System
          </div>
          <div className="space-y-1">
            {secondaryItems.map((item) => {
              const Icon = item.icon;
              return (
                <a
                  key={item.name}
                  href={item.href}
                  target="_blank"
                  rel="noreferrer"
                  className="flex items-center justify-between rounded-lg px-3 py-2 text-sm font-medium text-slate-600 hover:bg-gray-50 hover:text-slate-900 transition-colors"
                >
                  <div className="flex items-center gap-3">
                    <Icon className="h-4 w-4 text-slate-400" />
                    <span>{item.name}</span>
                  </div>
                  <ExternalLink className="h-3 w-3 text-slate-400" />
                </a>
              );
            })}
          </div>
        </div>
      </div>

      {/* Bottom Section: Storage Meter & User Profile */}
      <div className="space-y-4 pt-4">
        {/* BacCloud-Style Storage Widget Card */}
        <div className="rounded-xl border border-gray-200/90 bg-white p-3.5 shadow-xs space-y-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="rounded-lg bg-indigo-50 p-1.5 text-indigo-600">
                <HardDrive className="h-4 w-4" />
              </div>
              <span className="text-xs font-semibold text-slate-900">Storage</span>
            </div>
            <Link
              href="/nodes"
              onClick={onCloseMobile}
              className="text-[11px] font-medium text-indigo-600 hover:text-indigo-700 hover:underline"
            >
              See details
            </Link>
          </div>

          <div className="space-y-1.5">
            <div className="flex items-baseline justify-between text-xs">
              <span className="font-semibold text-slate-900">
                {formatBytes(usedCapacity)}
              </span>
              <span className="text-gray-400 text-[11px]">
                of {formatBytes(totalCapacity)}
              </span>
            </div>

            {/* Segmented mini progress bar */}
            <div className="h-2 w-full overflow-hidden rounded-full bg-gray-100 flex">
              <div
                style={{ width: `${usedPercent}%` }}
                className="h-full bg-indigo-600 transition-all duration-300"
              />
            </div>
          </div>

          <Link
            href="/nodes"
            onClick={onCloseMobile}
            className="flex w-full items-center justify-center gap-1.5 rounded-lg bg-slate-900 py-2 text-xs font-medium text-white hover:bg-slate-800 transition-colors shadow-xs"
          >
            <Sparkles className="h-3.5 w-3.5 text-indigo-300" />
            <span>Manage Nodes</span>
          </Link>
        </div>

        {/* User Profile / Auth Area */}
        <div className="border-t border-gray-100 pt-3">
          {isAuthenticated && user ? (
            <div className="flex items-center justify-between rounded-lg p-2 hover:bg-gray-50 transition-colors">
              <div className="flex items-center gap-2.5 min-w-0">
                <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-indigo-100 font-semibold text-xs text-indigo-700">
                  {user.name ? user.name.slice(0, 2).toUpperCase() : user.email.slice(0, 2).toUpperCase()}
                </div>
                <div className="min-w-0">
                  <div className="truncate text-xs font-medium text-slate-900">
                    {user.name || "Cloud User"}
                  </div>
                  <div className="truncate text-[11px] text-gray-500">
                    {user.email}
                  </div>
                </div>
              </div>
              <button
                type="button"
                onClick={() => logout()}
                title="Sign out"
                className="rounded-md p-1.5 text-gray-400 hover:bg-gray-200/60 hover:text-rose-600 transition-colors"
                aria-label="Sign out"
              >
                <LogOut className="h-4 w-4" />
              </button>
            </div>
          ) : (
            <button
              type="button"
              onClick={() => setAuthModalOpen(true)}
              className="flex w-full items-center justify-center gap-2 rounded-lg border border-gray-200 bg-white py-2 text-xs font-medium text-slate-700 hover:bg-gray-50 hover:text-slate-900 transition-colors shadow-xs"
            >
              <LogIn className="h-3.5 w-3.5 text-slate-500" />
              <span>Sign In / Register</span>
            </button>
          )}
        </div>
      </div>

      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
        defaultMode="login"
      />
    </div>
  );

  return (
    <>
      {/* Desktop Fixed Sidebar */}
      <aside className="hidden lg:flex lg:w-60 lg:flex-col lg:fixed lg:inset-y-0 z-30">
        {sidebarContent}
      </aside>

      {/* Mobile Drawer Backdrop & Overlay */}
      {mobileOpen && (
        <div
          className="fixed inset-0 z-50 bg-slate-950/40 backdrop-blur-xs lg:hidden animate-in fade-in duration-200"
          onClick={onCloseMobile}
        >
          <div
            className="fixed inset-y-0 left-0 w-72 max-w-[85vw] bg-white shadow-2xl animate-in slide-in-from-left duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            {sidebarContent}
          </div>
        </div>
      )}
    </>
  );
}
