import * as React from "react";
import { cn } from "@/lib/utils";

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "default" | "primary" | "secondary" | "destructive" | "outline" | "ghost";
  size?: "default" | "sm" | "lg" | "icon";
}

export function getButtonClassName(
  variant: ButtonProps["variant"] = "default",
  size: ButtonProps["size"] = "default",
  className = ""
) {
  const variantStyles = {
    default: "bg-slate-900 text-white hover:bg-slate-800 shadow-xs border border-slate-900",
    primary: "bg-indigo-600 text-white hover:bg-indigo-700 shadow-xs border border-indigo-600",
    secondary: "bg-gray-100 text-slate-800 hover:bg-gray-200 border border-transparent",
    destructive: "bg-rose-600 text-white hover:bg-rose-700 shadow-xs border border-rose-600",
    outline: "border border-gray-200 bg-white text-slate-700 hover:bg-gray-50 hover:text-slate-900 shadow-xs",
    ghost: "text-slate-600 hover:bg-gray-100 hover:text-slate-900 border border-transparent",
  }[variant];

  const sizeStyles = {
    default: "h-9 px-4 py-2 text-sm",
    sm: "h-8 rounded-lg px-3 text-xs",
    lg: "h-10 rounded-lg px-6 text-sm",
    icon: "h-9 w-9",
  }[size];

  return cn(
    "inline-flex items-center justify-center rounded-lg font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-500/20 disabled:pointer-events-none disabled:opacity-50 cursor-pointer select-none",
    variantStyles,
    sizeStyles,
    className
  );
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant = "default", size = "default", type = "button", ...props }, ref) => {
    return (
      <button
        ref={ref}
        type={type}
        className={getButtonClassName(variant, size, className)}
        {...props}
      />
    );
  }
);
Button.displayName = "Button";
