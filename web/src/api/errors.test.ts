import { describe, it, expect } from 'vitest'
import { ApiError, DEFAULT_ERROR_MESSAGE, errorMessage, isApiError, isNotFound, retryUnlessNotFound } from './errors'

describe('[UIF AC-088] ApiError helpers', () => {
  it('ApiError is an Error with name, status and message', () => {
    const e = new ApiError(404, 'HTTP 404: Not Found')
    expect(e).toBeInstanceOf(Error)
    expect(e.name).toBe('ApiError')
    expect(e.status).toBe(404)
    expect(e.message).toBe('HTTP 404: Not Found')
  })

  it('isApiError is true only for ApiError instances', () => {
    expect(isApiError(new ApiError(500, 'x'))).toBe(true)
    expect(isApiError(new Error('x'))).toBe(false)
    expect(isApiError({ status: 404 })).toBe(false)
  })

  it('isNotFound is true only for an ApiError with status 404', () => {
    expect(isNotFound(new ApiError(404, 'x'))).toBe(true)
    expect(isNotFound(new ApiError(500, 'x'))).toBe(false)
    expect(isNotFound(new TypeError('Failed to fetch'))).toBe(false)
    expect(isNotFound(new Error('Unauthorized'))).toBe(false)
    expect(isNotFound(undefined)).toBe(false)
  })

  it('retryUnlessNotFound retries a non-404 once and never retries a 404', () => {
    expect(retryUnlessNotFound(0, new ApiError(500, 'x'))).toBe(true)
    expect(retryUnlessNotFound(1, new ApiError(500, 'x'))).toBe(false)
    expect(retryUnlessNotFound(0, new ApiError(404, 'x'))).toBe(false)
    expect(retryUnlessNotFound(0, new TypeError('Failed to fetch'))).toBe(true)
  })
})

describe('[UIF AC-038] errorMessage fallback', () => {
  it('returns the message of an Error', () => {
    expect(errorMessage(new Error('HTTP 500: Internal Server Error'))).toBe('HTTP 500: Internal Server Error')
  })

  it('returns a non-empty string rejection as-is', () => {
    expect(errorMessage('boom')).toBe('boom')
  })

  it('uses the documented default text', () => {
    expect(DEFAULT_ERROR_MESSAGE).toBe('Something went wrong. Please try again.')
  })

  it.each([
    ['an Error with an empty message', new Error('')],
    ['undefined', undefined],
    ['null', null],
    ['an empty string', ''],
    ['an object with no message', { code: 7 }],
  ])('falls back for %s', (_label, value) => {
    expect(errorMessage(value)).toBe(DEFAULT_ERROR_MESSAGE)
  })

  it('uses a caller supplied fallback', () => {
    expect(errorMessage(undefined, 'custom')).toBe('custom')
  })
})
