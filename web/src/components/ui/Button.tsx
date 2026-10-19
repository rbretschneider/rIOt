import type { ButtonHTMLAttributes, Ref } from 'react'
import { focusRing } from './focusRing'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md'

export interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: ButtonVariant
  size?: ButtonSize
  /** Shows a spinner and `pendingLabel`, disables the button and sets aria-busy. */
  pending?: boolean
  /** Text shown while pending. Defaults to the normal children. */
  pendingLabel?: string
  ref?: Ref<HTMLButtonElement>
}

const BASE =
  'inline-flex items-center justify-center gap-2 rounded-md font-medium whitespace-nowrap transition-colors ' +
  'disabled:cursor-not-allowed disabled:opacity-50'

const VARIANTS: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-white hover:bg-accent-hover disabled:hover:bg-accent',
  secondary:
    'border border-border-strong bg-raised-2 text-text hover:bg-border-strong disabled:hover:bg-raised-2',
  ghost: 'bg-transparent text-text-muted hover:bg-raised-2 hover:text-text disabled:hover:bg-transparent disabled:hover:text-text-muted',
  danger: 'bg-danger text-white hover:bg-danger-hover disabled:hover:bg-danger',
}

const SIZES: Record<ButtonSize, string> = {
  sm: 'px-3 py-1.5 text-xs',
  md: 'px-4 py-2 text-sm',
}

/** Inline loading spinner. Inherits the surrounding text color. */
export function Spinner({ className = '' }: { className?: string }) {
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      fill="none"
      className={`h-4 w-4 animate-spin ${className}`}
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeWidth="3" className="opacity-25" />
      <path d="M21 12a9 9 0 0 0-9-9" stroke="currentColor" strokeWidth="3" strokeLinecap="round" />
    </svg>
  )
}

export function Button({
  variant = 'secondary',
  size = 'md',
  pending = false,
  pendingLabel,
  type = 'button',
  disabled,
  className = '',
  children,
  ref,
  ...rest
}: ButtonProps) {
  // type defaults to "button" so a Button inside a <form> never submits it by accident.
  // Pending also disables the button: the disabled attribute is what stops a second
  // click while the first action is in flight.
  return (
    <button
      {...rest}
      ref={ref}
      type={type}
      disabled={disabled || pending}
      aria-busy={pending ? true : undefined}
      data-variant={variant}
      data-size={size}
      className={`${BASE} ${VARIANTS[variant]} ${SIZES[size]} ${focusRing} ${className}`.trim()}
    >
      {pending && <Spinner />}
      {pending ? (pendingLabel ?? children) : children}
    </button>
  )
}
