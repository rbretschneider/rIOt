import type { ReactNode } from 'react'
import { errorMessage } from '../../api/errors'
import { Button } from './Button'
import { Skeleton } from './Skeleton'

export interface ErrorPanelProps {
  title?: string
  error: unknown
  onRetry?: () => void
}

/** Honest failure view: what failed, why, and a Retry when the caller can refetch. */
export function ErrorPanel({ title = "Couldn't load data", error, onRetry }: ErrorPanelProps) {
  return (
    <div
      role="alert"
      className="flex flex-col items-start gap-2 rounded-lg border border-danger-hover/30 bg-danger-hover/10 p-4"
    >
      <h3 className="text-sm font-semibold text-danger-fg wrap-anywhere">{title}</h3>
      <p className="max-w-full text-sm text-text-muted break-words">{errorMessage(error)}</p>
      {onRetry && (
        <Button variant="secondary" size="sm" className="mt-1" onClick={onRetry}>
          Retry
        </Button>
      )}
    </div>
  )
}

export interface QueryStateProps<T> {
  query: {
    isLoading?: boolean
    error?: unknown
    data: T | undefined
    refetch?: () => unknown
  }
  /** Evaluated only when data exists, so an error can never be mistaken for empty. */
  isEmpty?: (data: T) => boolean
  empty?: ReactNode
  loading?: ReactNode
  errorTitle?: string
  renderError?: (error: unknown, retry: () => void) => ReactNode
  children: (data: T) => ReactNode
}

/**
 * Loading, error-with-retry, empty and data views for one query. Data wins over
 * everything: a failed background refetch keeps showing the last good data.
 */
export function QueryState<T>({
  query,
  isEmpty,
  empty,
  loading,
  errorTitle,
  renderError,
  children,
}: QueryStateProps<T>) {
  const { data, error, refetch } = query
  const retry = () => {
    void refetch?.()
  }

  if (data !== undefined) {
    if (isEmpty?.(data)) return <>{empty}</>
    return <>{children(data)}</>
  }
  if (error) {
    return renderError ? (
      <>{renderError(error, retry)}</>
    ) : (
      <ErrorPanel title={errorTitle} error={error} onRetry={refetch ? retry : undefined} />
    )
  }
  return (
    <div role="status" aria-busy="true" aria-label="Loading">
      {loading ?? <Skeleton lines={3} />}
    </div>
  )
}
