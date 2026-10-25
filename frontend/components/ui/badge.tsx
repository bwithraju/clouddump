import * as React from "react";
import { cn } from "@/lib/utils";

export interface BadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: "default" | "secondary" | "success" | "destructive" | "warning" | "outline";
}

export function Badge({ className, variant = "default", ...props }: BadgeProps) {
  const variantStyles = {
    default: "bg-indigo-950 text-indigo-300 border-indigo-800",
    secondary: "bg-slate-800 text-slate-300 border-slate-700",
    success: "bg-emerald-950/80 text-emerald-400 border-emerald-800",
    destructive: "bg-rose-950/80 text-rose-400 border-rose-800",
    warning: "bg-amber-950/80 text-amber-400 border-amber-800",
    outline: "text-slate-300 border-slate-700",
  }[variant];

  return (
    <div
      className={cn(
        "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-medium transition-colors focus:outline-none",
        variantStyles,
        className
      )}
      {...props}
    />
  );
}
