import type { ReactNode } from 'react'
import { Button } from './Button'

export interface EmptyStateProps {
  title: string
  description?: ReactNode
  action?: { label: string; onClick: () => void }
  icon?: ReactNode
  /** Extra content under the action, for example a back link. */
  children?: ReactNode
}

/** Centered "nothing here" panel with an optional call to action. */
export function EmptyState({ title, description, action, icon, children }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-2 px-4 py-12 text-center">
      {icon != null && <div className="mb-1 text-text-muted">{icon}</div>}
      <h3 className="max-w-full text-base font-semibold text-text wrap-anywhere">{title}</h3>
      {description != null && (
        <div className="max-w-md text-sm text-text-muted break-words">{description}</div>
      )}
      {action && (
        <Button variant="secondary" className="mt-3" onClick={action.onClick}>
          {action.label}
        </Button>
      )}
      {children}
    </div>
  )
}
