import type { HTMLAttributes, ReactNode } from 'react'

export interface CardProps extends Omit<HTMLAttributes<HTMLElement>, 'title'> {
  title?: ReactNode
  titleAs?: 'h2' | 'h3'
  /** Right-aligned slot in the header, for buttons or counts. */
  actions?: ReactNode
  padding?: 'none' | 'md'
}

/** A raised panel with a hairline border. With a title it gets a ruled header. */
export function Card({
  title,
  titleAs: Heading = 'h2',
  actions,
  padding = 'md',
  className = '',
  children,
  ...rest
}: CardProps) {
  const hasHeader = title != null || actions != null
  return (
    <section
      {...rest}
      className={`rounded-lg border border-border bg-raised ${className}`.trim()}
    >
      {hasHeader && (
        <header className="flex items-center justify-between gap-3 border-b border-border px-4 py-3">
          {title != null && <Heading className="min-w-0 text-sm font-semibold text-text">{title}</Heading>}
          {actions != null && <div className="ml-auto flex shrink-0 items-center gap-2">{actions}</div>}
        </header>
      )}
      <div className={padding === 'md' ? 'p-4' : undefined}>{children}</div>
    </section>
  )
}
