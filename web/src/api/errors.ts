// Typed API error. Lives in its own module on purpose: the demo build aliases
// api/client and api/settings to stand-ins, and many tests mock '../api/client'
// with partial factories, so anything imported from those modules at runtime
// could be swapped out or be undefined. Pages import from here instead.

export class ApiError extends Error {
  readonly status: number
  /** Machine-readable error code from the response body, when the server sent one. */
  readonly code?: string
  /** The parsed error body, for fields beyond `error` and `code`. */
  readonly detail?: Record<string, unknown>

  constructor(status: number, message: string, code?: string, detail?: Record<string, unknown>) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.detail = detail
  }
}

export function isApiError(e: unknown): e is ApiError {
  return e instanceof ApiError
}

export function isNotFound(e: unknown): boolean {
  return isApiError(e) && e.status === 404
}

export const DEFAULT_ERROR_MESSAGE = 'Something went wrong. Please try again.'

/** Text safe to show a user for any thrown value. Never throws. */
export function errorMessage(e: unknown, fallback: string = DEFAULT_ERROR_MESSAGE): string {
  if (e instanceof Error && e.message) return e.message
  if (typeof e === 'string' && e) return e
  return fallback
}

/** Matches the app-wide `retry: 1` but never retries a 404. */
export function retryUnlessNotFound(failureCount: number, error: unknown): boolean {
  return failureCount < 1 && !isNotFound(error)
}
