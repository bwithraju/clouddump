# CloudDump Design System Guide

This document establishes the permanent visual language, layout structure, design tokens, and reusable component patterns for CloudDump, modeled on the clean, minimal **BacCloud** visual reference.

---

## 1. Core Principles

1. **Clean & Minimal**: Neutral soft backgrounds, crisp white content surfaces, and purposeful information density.
2. **Restraint over Fluff**: No glowing borders, glassmorphism, aggressive gradient backgrounds, or distracting animations.
3. **Data Hierarchy**: Clear visual distinction between primary metrics, status indicators, and tabular data.
4. **Preserved Functional Integrity**: Seamless real-time integration with distributed storage nodes, chunk maps, resumable upload sessions, and telemetry.

---

## 2. Color Palette & Tokens

| Token | Value / Tailwind Class | Usage |
| :--- | :--- | :--- |
| **App Background** | `#f8f9fb` / `bg-[#f8f9fb]` | Global canvas background |
| **Surface (Card)** | `#ffffff` / `bg-white` | Content cards, tables, modal dialogs |
| **Card Border** | `#e5e7eb` / `border-gray-200/80` | Subtle structural dividers |
| **Text Primary** | `#0f172a` / `text-slate-900` | Headings, titles, high-contrast labels |
| **Text Secondary** | `#64748b` / `text-slate-600` | Descriptions, secondary metadata |
| **Text Muted** | `#94a3b8` / `text-gray-400` | Timestamps, IDs, micro-labels |
| **Primary Button** | `#0f172a` / `bg-slate-900` | Primary actions ("Upload file", "Manage Nodes") |
| **Accent Primary** | `#4f46e5` / `text-indigo-600` | Active nav items, icons, progress bars |
| **Success Status** | `text-emerald-700 bg-emerald-50 border-emerald-200` | Online nodes, completed files, healthy state |
| **Warning Status** | `text-amber-700 bg-amber-50 border-amber-200` | Degraded files, upload pause, offline node alert |
| **Error Status** | `text-rose-700 bg-rose-50 border-rose-200` | Failed operations, corrupted chunks |

---

## 3. Layout Structure

CloudDump follows a persistent two-column architecture via `<AppLayout>`:

```
+-------------------------------------------------------------------------------+
| [Sidebar: w-60 fixed]  | [Header: h-16 sticky top-0]                         |
| - Brand Mark           | - Global Search Bar (files, nodes, documents)        |
| - Navigation Items     | - Pool Health Status Dot ("Operational")             |
|   * Dashboard (/)      | - Notification Bell Popover                          |
|   * My files (/files)  | - User Profile Avatar / Sign In                      |
|   * Upload (/upload)   +------------------------------------------------------+
|   * Nodes (/nodes)     | [Main Content Canvas: max-w-7xl px-8 py-6]           |
| - Storage Meter Card   | - Page Header & Primary Actions                      |
| - User Account Footer  | - Metric / Quick Access Cards                        |
|                        | - Content Tables (List/Grid toggles)                 |
+-------------------------------------------------------------------------------+
```

---

## 4. Reusable UI Components

### `Card` (`@/components/ui/card`)
- Clean white surface with `rounded-xl border border-gray-200/80 shadow-[0_1px_3px_rgba(0,0,0,0.03)]`.
- Always structure with `CardHeader`, `CardTitle`, `CardDescription`, and `CardContent`.

### `Button` (`@/components/ui/button`)
- **`default`**: `bg-slate-900 text-white hover:bg-slate-800` (Hero actions).
- **`primary`**: `bg-indigo-600 text-white hover:bg-indigo-700` (Accent actions).
- **`outline`**: `border border-gray-200 bg-white text-slate-700 hover:bg-gray-50`.
- **`ghost`**: `text-slate-600 hover:bg-gray-100 hover:text-slate-900`.
- **`destructive`**: `bg-rose-600 text-white hover:bg-rose-700`.

### `Badge` (`@/components/ui/badge`)
- Soft pastel pills with subtle borders:
  - `default`: Indigo tint
  - `success`: Emerald tint
  - `warning`: Amber tint
  - `destructive`: Rose tint
  - `outline`: Neutral gray border

### `Table` (`@/components/ui/table`)
- Minimalist white table surface.
- `TableHeader` has uppercase `text-[11px] font-semibold text-gray-500 bg-gray-50/50`.
- `TableRow` has `border-b border-gray-100 hover:bg-gray-50/60`.

---

## 5. Standard Page Guidelines for Future Features

1. **Header Row**: Title in `text-2xl font-bold tracking-tight text-slate-900` with subtle description below. Place primary action buttons on the right.
2. **Quick Access / Metrics**: Use 4-column responsive grids (`grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4`).
3. **Data Listings**: Offer both **List** (Table) and **Grid** views whenever presenting user files or nodes.
4. **Interactive Dialogs**: Always portal modals to `document.body` with `fixed inset-0 z-50 overflow-y-auto bg-slate-950/40 backdrop-blur-xs` and an inner `flex min-h-full items-center justify-center p-4` wrapper.
