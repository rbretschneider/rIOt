// Typed API error. Lives in its own module on purpose: the demo build aliases
// api/client and api/settings to stand-ins, and many tests mock '../api/client'
// with partial factories, so anything imported from those modules at runtime
// could be swapped out or be undefined. Pages import from here instead.

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
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
