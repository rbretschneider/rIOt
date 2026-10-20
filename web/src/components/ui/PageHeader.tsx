import type { ReactNode } from 'react'

export interface PageHeaderProps {
  title: ReactNode
  description?: ReactNode
  eyebrow?: ReactNode
  actions?: ReactNode
  children?: ReactNode
}

export function PageHeader({
  title,
  description,
  eyebrow,
  actions,
  children,
}: PageHeaderProps): React.JSX.Element {
  return (
    <header className="mb-5 min-w-0 border-b border-border pb-4">
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1 basis-64">
          {eyebrow && (
            <div className="mb-1 text-xs font-medium text-text-muted">
              {eyebrow}
            </div>
          )}
          <h1 className="break-words text-2xl font-semibold tracking-tight text-text [overflow-wrap:anywhere]">
            {title}
          </h1>
          {description && (
            <div className="mt-1 text-sm text-text-muted">{description}</div>
          )}
        </div>
        {actions && (
          <div className="flex max-w-full flex-wrap items-center gap-2">
            {actions}
          </div>
        )}
      </div>
      {children && (
        <div className="mt-3 flex min-w-0 flex-wrap items-center gap-3 text-sm text-text-muted">
          {children}
        </div>
      )}
    </header>
  )
}
