// TypeScript mirror of the design tokens declared in index.css. Tailwind owns
// the real values; this file exists so tests can assert that index.css still
// aliases the intended palette and that text tokens meet WCAG contrast.

/** Token name (without the --color- prefix) to the Tailwind palette step it aliases. */
export const TOKEN_PALETTE = {
  surface: 'gray-950',
  raised: 'gray-900',
  'raised-2': 'gray-800',
  border: 'gray-800',
  'border-strong': 'gray-700',
  text: 'gray-100',
  'text-muted': 'gray-400',
  accent: 'blue-600',
  'accent-hover': 'blue-500',
  link: 'blue-400',
  success: 'emerald-500',
  'success-fg': 'emerald-400',
  warn: 'amber-500',
  'warn-fg': 'amber-400',
  danger: 'red-600',
  'danger-hover': 'red-500',
  'danger-fg': 'red-400',
  info: 'blue-500',
  'info-fg': 'blue-400',
} as const

export type TokenName = keyof typeof TOKEN_PALETTE

/** sRGB hex of each palette step. Used only by the contrast test. */
export const TOKEN_HEX: Record<TokenName, string> = {
  surface: '#030712',
  raised: '#111827',
  'raised-2': '#1f2937',
  border: '#1f2937',
  'border-strong': '#374151',
  text: '#f3f4f6',
  'text-muted': '#9ca3af',
  accent: '#2563eb',
  'accent-hover': '#3b82f6',
  link: '#60a5fa',
  success: '#10b981',
  'success-fg': '#34d399',
  warn: '#f59e0b',
  'warn-fg': '#fbbf24',
  danger: '#dc2626',
  'danger-hover': '#ef4444',
  'danger-fg': '#f87171',
  info: '#3b82f6',
  'info-fg': '#60a5fa',
}

/** The layering scale, lowest to highest. Matches the --z-* variables in index.css. */
export const Z_INDEX = {
  base: 0,
  dropdown: 40,
  modal: 50,
  tooltip: 70,
  toast: 80,
} as const
