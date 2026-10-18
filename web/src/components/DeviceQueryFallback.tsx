import { Link } from 'react-router-dom'
import { isNotFound } from '../api/errors'
import { EmptyState, ErrorPanel, Skeleton } from './ui'

interface Props {
  isLoading: boolean
  error: unknown
  onRetry: () => void
}

/**
 * What a device page shows when it has no device data. A 404 is "not found";
 * any other failure (including a network error with no status) is a load
 * failure with Retry; otherwise the first fetch is still in flight.
 */
export default function DeviceQueryFallback({ isLoading, error, onRetry }: Props) {
  // A 404, or a settled query that produced neither data nor an error (for example a
  // missing route parameter), both mean there is no such device.
  if (isNotFound(error) || (!isLoading && !error)) {
    return (
      <EmptyState
        title="Device not found"
        description="This device no longer exists, or the link is wrong."
      >
        <Link to="/" className="mt-3 text-sm font-medium text-link hover:text-info-fg">
          Back to Fleet
        </Link>
      </EmptyState>
    )
  }
  if (error) {
    return <ErrorPanel title="Couldn't load device" error={error} onRetry={onRetry} />
  }
  return (
    <div role="status" aria-busy="true" aria-label="Loading" className="space-y-4">
      <Skeleton className="h-8 w-64" />
      <Skeleton lines={4} />
    </div>
  )
}
