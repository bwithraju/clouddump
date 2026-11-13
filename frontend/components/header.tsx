"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import {
  Search,
  Bell,
  Menu,
  ShieldCheck,
  AlertTriangle,
  CheckCircle2,
  X,
  UploadCloud,
} from "lucide-react";
import Link from "next/link";
import { useAuth } from "@/hooks/use-auth";
import { useSystemActivity } from "@/hooks/use-system";
import { AuthModal } from "@/components/auth-modal";
import { formatDate } from "@/lib/utils";

interface HeaderProps {
  onToggleMobileMenu: () => void;
}

export function Header({ onToggleMobileMenu }: HeaderProps) {
  const router = useRouter();
  const { user, isAuthenticated } = useAuth();
  const { data: activity } = useSystemActivity();
  const [searchVal, setSearchVal] = React.useState("");
  const [authModalOpen, setAuthModalOpen] = React.useState(false);
  const [notificationsOpen, setNotificationsOpen] = React.useState(false);

  const failedOps = activity?.failed_operations || [];
  const recentOps = activity?.recent_operations || [];
  const hasAlerts = failedOps.length > 0;

  const handleSearchSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (searchVal.trim()) {
      router.push(`/files?q=${encodeURIComponent(searchVal.trim())}`);
    } else {
      router.push("/files");
    }
  };

  return (
    <header className="sticky top-0 z-20 flex h-16 w-full items-center justify-between border-b border-gray-200/80 bg-white/95 px-4 sm:px-6 lg:px-8 backdrop-blur-xs select-none">
      {/* Left: Mobile Toggle & BacCloud Search Bar */}
      <div className="flex items-center gap-3 flex-1 max-w-xl">
        <button
          type="button"
          onClick={onToggleMobileMenu}
          className="lg:hidden rounded-lg p-2 text-slate-500 hover:bg-gray-100 hover:text-slate-900 transition-colors"
          aria-label="Open sidebar"
        >
          <Menu className="h-5 w-5" />
        </button>

        {/* BacCloud-Style Search Box */}
        <form onSubmit={handleSearchSubmit} className="relative w-full max-w-md">
          <Search className="absolute left-3.5 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <input
            type="text"
            placeholder="Search for document, files, nodes..."
            value={searchVal}
            onChange={(e) => setSearchVal(e.target.value)}
            className="w-full rounded-lg border border-gray-200 bg-gray-50/70 pl-9 pr-4 py-2 text-xs text-slate-900 placeholder:text-gray-400 focus:bg-white focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/15 transition-all"
          />
        </form>
      </div>

      {/* Right: Quick Upload, Status, Notifications, User */}
      <div className="flex items-center gap-2 sm:gap-3">
        {/* Pool Telemetry Status Pill */}
        <div className="hidden sm:flex items-center gap-1.5 rounded-full border border-gray-200/80 bg-gray-50/60 px-2.5 py-1 text-xs font-medium text-slate-700">
          <span
            className={`h-2 w-2 rounded-full ${
              hasAlerts ? "bg-amber-500 animate-pulse" : "bg-emerald-500"
            }`}
          />
          <span className="text-[11px]">
            {hasAlerts ? `${failedOps.length} degraded` : "Operational"}
          </span>
        </div>

        {/* Notifications Popover */}
        <div className="relative">
          <button
            type="button"
            onClick={() => setNotificationsOpen((prev) => !prev)}
            className="relative rounded-lg p-2 text-gray-500 hover:bg-gray-100 hover:text-slate-900 transition-colors"
            aria-label="Notifications"
          >
            <Bell className="h-4 w-4" />
            {hasAlerts && (
              <span className="absolute top-1.5 right-1.5 h-2 w-2 rounded-full bg-rose-500 ring-2 ring-white" />
            )}
          </button>

          {notificationsOpen && (
            <>
              <div
                className="fixed inset-0 z-40"
                onClick={() => setNotificationsOpen(false)}
              />
              <div className="absolute right-0 mt-2 w-80 rounded-xl border border-gray-200 bg-white shadow-xl z-50 p-4 space-y-3 animate-in fade-in zoom-in-95 duration-150">
                <div className="flex items-center justify-between border-b border-gray-100 pb-2">
                  <div className="flex items-center gap-2">
                    <span className="text-xs font-semibold text-slate-900">Notifications</span>
                    {hasAlerts && (
                      <span className="rounded-full bg-rose-100 text-rose-700 text-[10px] px-1.5 py-0.5 font-medium">
                        {failedOps.length} alerts
                      </span>
                    )}
                  </div>
                  <button
                    type="button"
                    onClick={() => setNotificationsOpen(false)}
                    className="text-gray-400 hover:text-slate-700"
                  >
                    <X className="h-4 w-4" />
                  </button>
                </div>

                <div className="space-y-2 max-h-64 overflow-y-auto pr-1">
                  {failedOps.length > 0 && (
                    <div className="space-y-1.5">
                      <div className="text-[10px] font-semibold text-rose-600 uppercase tracking-wider">
                        Alerts
                      </div>
                      {failedOps.slice(0, 3).map((f) => (
                        <div
                          key={f.id}
                          className="rounded-lg border border-rose-100 bg-rose-50/60 p-2 text-xs text-rose-900 space-y-0.5"
                        >
                          <div className="font-medium truncate flex items-center gap-1.5">
                            <AlertTriangle className="h-3 w-3 text-rose-500 shrink-0" />
                            <span>{f.entity}</span>
                          </div>
                          <div className="text-[11px] text-rose-700/80 truncate">{f.reason}</div>
                        </div>
                      ))}
                    </div>
                  )}

                  <div className="space-y-1.5 pt-1">
                    <div className="text-[10px] font-semibold text-gray-400 uppercase tracking-wider">
                      Recent Activity
                    </div>
                    {recentOps.length === 0 ? (
                      <div className="py-4 text-center text-xs text-gray-400">
                        No recent activity recorded
                      </div>
                    ) : (
                      recentOps.slice(0, 4).map((op) => (
                        <div
                          key={op.id}
                          className="rounded-lg p-2 hover:bg-gray-50 transition-colors text-xs border border-transparent hover:border-gray-100"
                        >
                          <div className="flex items-center justify-between text-slate-900 font-medium">
                            <span className="truncate">{op.entity}</span>
                            <span className="text-[10px] font-mono text-gray-400">
                              {formatDate(op.timestamp)}
                            </span>
                          </div>
                          <div className="text-[11px] text-gray-500 truncate">{op.details}</div>
                        </div>
                      ))
                    )}
                  </div>
                </div>

                <div className="border-t border-gray-100 pt-2 text-center">
                  <Link
                    href="/"
                    onClick={() => setNotificationsOpen(false)}
                    className="text-[11px] font-medium text-indigo-600 hover:underline"
                  >
                    View System Dashboard →
                  </Link>
                </div>
              </div>
            </>
          )}
        </div>

        {/* User Account / Trigger */}
        {isAuthenticated && user ? (
          <div className="flex items-center gap-2 pl-2 border-l border-gray-200">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-slate-900 text-xs font-semibold text-white shadow-xs">
              {user.name ? user.name.slice(0, 1).toUpperCase() : user.email.slice(0, 1).toUpperCase()}
            </div>
          </div>
        ) : (
          <button
            type="button"
            onClick={() => setAuthModalOpen(true)}
            className="rounded-lg bg-slate-900 px-3 py-1.5 text-xs font-medium text-white hover:bg-slate-800 transition-colors shadow-xs"
          >
            Sign In
          </button>
        )}
      </div>

      <AuthModal
        isOpen={authModalOpen}
        onClose={() => setAuthModalOpen(false)}
        defaultMode="login"
      />
    </header>
  );
}
