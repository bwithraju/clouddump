"use client";

import * as React from "react";
import { createPortal } from "react-dom";
import { X, Lock, Mail, User as UserIcon, Loader2, AlertCircle } from "lucide-react";
import { useAuth } from "@/hooks/use-auth";
import { Button } from "@/components/ui/button";

interface AuthModalProps {
  isOpen: boolean;
  onClose: () => void;
  defaultMode?: "login" | "register";
}

function AuthModalContent({
  onClose,
  defaultMode,
}: {
  onClose: () => void;
  defaultMode: "login" | "register";
}) {
  const [mode, setMode] = React.useState<"login" | "register">(defaultMode);
  const [email, setEmail] = React.useState("");
  const [password, setPassword] = React.useState("");
  const [name, setName] = React.useState("");
  const [errorMsg, setErrorMsg] = React.useState<string | null>(null);

  const { login, register, isLoggingIn, isRegistering } = useAuth();
  const isSubmitting = isLoggingIn || isRegistering;

  // Handle escape key
  React.useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose();
      }
    };
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onClose]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErrorMsg(null);

    if (!email.trim() || !password.trim()) {
      setErrorMsg("Email and password are required.");
      return;
    }

    try {
      if (mode === "login") {
        await login({ email: email.trim(), password });
      } else {
        await register({ email: email.trim(), password, name: name.trim() || undefined });
      }
      setEmail("");
      setPassword("");
      setName("");
      onClose();
    } catch (err: any) {
      setErrorMsg(err.message || "Authentication failed. Please check credentials.");
    }
  };

  return (
    <div
      className="fixed inset-0 z-50 overflow-y-auto bg-slate-950/40 backdrop-blur-xs animate-in fade-in duration-150"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="flex min-h-full items-center justify-center p-4">
        <div
          className="relative w-full max-w-md rounded-2xl border border-gray-200 bg-white p-6 shadow-2xl space-y-5 text-slate-900"
          role="dialog"
          aria-modal="true"
          aria-labelledby="auth-modal-title"
        >
          {/* Close Button */}
          <button
            type="button"
            onClick={onClose}
            className="absolute right-4 top-4 rounded-lg p-1.5 text-gray-400 hover:text-slate-700 hover:bg-gray-100 transition-colors"
            aria-label="Close"
          >
            <X className="h-5 w-5" />
          </button>

          {/* Header */}
          <div className="space-y-1">
            <h2 id="auth-modal-title" className="text-xl font-bold tracking-tight text-slate-900 flex items-center gap-2">
              <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-indigo-50 text-indigo-600">
                <Lock className="h-4 w-4" />
              </div>
              {mode === "login" ? "Sign in to CloudDump" : "Create CloudDump Account"}
            </h2>
            <p className="text-xs text-gray-500">
              {mode === "login"
                ? "Access your files, storage nodes, and resumable uploads."
                : "Set up an account to manage distributed storage pools."}
            </p>
          </div>

          {/* Tab switch */}
          <div className="flex rounded-xl bg-gray-100 p-1 border border-gray-200 text-xs font-medium">
            <button
              type="button"
              onClick={() => {
                setMode("login");
                setErrorMsg(null);
              }}
              className={`flex-1 py-1.5 rounded-lg transition-all ${
                mode === "login"
                  ? "bg-white text-slate-900 font-semibold shadow-xs"
                  : "text-gray-500 hover:text-slate-900"
              }`}
            >
              Log In
            </button>
            <button
              type="button"
              onClick={() => {
                setMode("register");
                setErrorMsg(null);
              }}
              className={`flex-1 py-1.5 rounded-lg transition-all ${
                mode === "register"
                  ? "bg-white text-slate-900 font-semibold shadow-xs"
                  : "text-gray-500 hover:text-slate-900"
              }`}
            >
              Create Account
            </button>
          </div>

          {/* Error message alert */}
          {errorMsg && (
            <div className="rounded-xl border border-rose-200 bg-rose-50 p-3 text-xs text-rose-800 flex items-start gap-2 shadow-xs">
              <AlertCircle className="h-4 w-4 text-rose-600 shrink-0 mt-0.5" />
              <span>{errorMsg}</span>
            </div>
          )}

          {/* Form */}
          <form onSubmit={handleSubmit} className="space-y-3.5">
            {mode === "register" && (
              <div className="space-y-1">
                <label className="text-xs font-medium text-slate-700">Name (optional)</label>
                <div className="relative">
                  <UserIcon className="absolute left-3 top-2.5 h-4 w-4 text-gray-400" />
                  <input
                    type="text"
                    placeholder="Your Name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className="w-full rounded-lg border border-gray-200 bg-gray-50/70 pl-9 pr-3 py-2 text-sm text-slate-900 placeholder:text-gray-400 focus:bg-white focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/15 transition-all"
                  />
                </div>
              </div>
            )}

            <div className="space-y-1">
              <label className="text-xs font-medium text-slate-700">Email address</label>
              <div className="relative">
                <Mail className="absolute left-3 top-2.5 h-4 w-4 text-gray-400" />
                <input
                  type="email"
                  required
                  placeholder="user@example.com"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full rounded-lg border border-gray-200 bg-gray-50/70 pl-9 pr-3 py-2 text-sm text-slate-900 placeholder:text-gray-400 focus:bg-white focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/15 transition-all"
                />
              </div>
            </div>

            <div className="space-y-1">
              <label className="text-xs font-medium text-slate-700">Password</label>
              <div className="relative">
                <Lock className="absolute left-3 top-2.5 h-4 w-4 text-gray-400" />
                <input
                  type="password"
                  required
                  placeholder="••••••••"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  className="w-full rounded-lg border border-gray-200 bg-gray-50/70 pl-9 pr-3 py-2 text-sm text-slate-900 placeholder:text-gray-400 focus:bg-white focus:border-indigo-500 focus:outline-none focus:ring-2 focus:ring-indigo-500/15 transition-all"
                />
              </div>
              {mode === "register" && (
                <p className="text-[11px] text-gray-400">Minimum 6 characters required.</p>
              )}
            </div>

            <Button
              type="submit"
              disabled={isSubmitting}
              className="w-full flex items-center justify-center gap-2 mt-2 bg-slate-900 hover:bg-slate-800 text-white font-medium py-2.5 shadow-xs"
            >
              {isSubmitting ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  {mode === "login" ? "Signing in..." : "Creating account..."}
                </>
              ) : mode === "login" ? (
                "Sign In"
              ) : (
                "Register & Sign In"
              )}
            </Button>
          </form>
        </div>
      </div>
    </div>
  );
}

export function AuthModal({ isOpen, onClose, defaultMode = "login" }: AuthModalProps) {
  const [mounted, setMounted] = React.useState(false);

  React.useEffect(() => {
    setMounted(true);
  }, []);

  if (!isOpen || !mounted) return null;

  return createPortal(
    <AuthModalContent key={`${defaultMode}-${isOpen}`} onClose={onClose} defaultMode={defaultMode} />,
    document.body
  );
}
