import * as React from "react";
import { cn } from "@/lib/utils";

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "default" | "secondary" | "destructive" | "outline" | "ghost";
  size?: "default" | "sm" | "lg" | "icon";
}

export function getButtonClassName(
  variant: ButtonProps["variant"] = "default",
  size: ButtonProps["size"] = "default",
  className = ""
) {
  const variantStyles = {
    default: "bg-indigo-600 text-white hover:bg-indigo-500 shadow-sm",
    secondary: "bg-slate-800 text-slate-200 hover:bg-slate-700 border border-slate-700",
    destructive: "bg-rose-600 text-white hover:bg-rose-500 shadow-sm",
    outline: "border border-slate-700 bg-transparent text-slate-200 hover:bg-slate-800",
    ghost: "text-slate-300 hover:bg-slate-800 hover:text-white",
  }[variant];

  const sizeStyles = {
    default: "h-9 px-4 py-2 text-sm",
    sm: "h-8 rounded-md px-3 text-xs",
    lg: "h-10 rounded-md px-8 text-base",
    icon: "h-9 w-9",
  }[size];

  return cn(
    "inline-flex items-center justify-center rounded-lg font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-indigo-400 disabled:pointer-events-none disabled:opacity-50 cursor-pointer",
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
