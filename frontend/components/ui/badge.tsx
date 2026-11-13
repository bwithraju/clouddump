import * as React from "react";
import { cn } from "@/lib/utils";

export interface BadgeProps extends React.HTMLAttributes<HTMLDivElement> {
  variant?: "default" | "secondary" | "success" | "destructive" | "warning" | "outline";
}

export function Badge({ className, variant = "default", ...props }: BadgeProps) {
  const variantStyles = {
    default: "bg-indigo-50 text-indigo-700 border-indigo-200/80",
    secondary: "bg-gray-100 text-gray-700 border-gray-200/80",
    success: "bg-emerald-50 text-emerald-700 border-emerald-200/80",
    destructive: "bg-rose-50 text-rose-700 border-rose-200/80",
    warning: "bg-amber-50 text-amber-700 border-amber-200/80",
    outline: "text-gray-600 border-gray-200 bg-white",
  }[variant];

  return (
    <div
      className={cn(
        "inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-medium transition-colors focus:outline-none",
        variantStyles,
        className
      )}
      {...props}
    />
  );
}
