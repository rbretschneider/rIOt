import type { ReactNode } from 'react'
import { enumLabel, toneFor, type LabelCategory } from './labels'

export type BadgeTone = 'neutral' | 'accent' | 'success' | 'warn' | 'danger' | 'info'

const TONES: Record<BadgeTone, string> = {
  neutral: 'bg-text-muted/20 text-text-muted',
  accent: 'bg-accent/20 text-link',
  success: 'bg-success/20 text-success-fg',
  warn: 'bg-warn/20 text-warn-fg',
  danger: 'bg-danger-hover/20 text-danger-fg',
  info: 'bg-info/20 text-info-fg',
}

const BASE = 'inline-flex items-center rounded px-2 py-0.5 text-xs font-medium'

type BadgeProps =
  | { tone?: BadgeTone; children: ReactNode; className?: string }
  | { category: LabelCategory; value: string | null | undefined; tone?: BadgeTone; className?: string }

/**
 * Small status pill. Give it children and a tone, or give it an enum category and
 * value: the text then comes from the shared label map and the tone from toneFor().
 */
export function Badge(props: BadgeProps) {
  const className = props.className ?? ''
  if ('category' in props) {
    const tone = props.tone ?? toneFor(props.category, props.value)
    return (
      <span data-tone={tone} className={`${BASE} ${TONES[tone]} ${className}`.trim()}>
        {enumLabel(props.category, props.value)}
      </span>
    )
  }
  const tone = props.tone ?? 'neutral'
  return (
    <span data-tone={tone} className={`${BASE} ${TONES[tone]} ${className}`.trim()}>
      {props.children}
    </span>
  )
}
