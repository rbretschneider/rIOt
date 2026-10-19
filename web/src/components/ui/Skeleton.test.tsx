import { describe, it, expect } from 'vitest'
import { render } from '@testing-library/react'
import { Skeleton } from './Skeleton'

describe('[UIF AC-044] hidden from assistive technology', () => {
  it('sets aria-hidden on a single block', () => {
    const { container } = render(<Skeleton />)
    expect(container.firstElementChild).toHaveAttribute('aria-hidden', 'true')
  })

  it('sets aria-hidden on the wrapper of a multi-line skeleton', () => {
    const { container } = render(<Skeleton lines={3} />)
    expect(container.firstElementChild).toHaveAttribute('aria-hidden', 'true')
    expect(container.firstElementChild?.children).toHaveLength(3)
  })

  it('shortens the last line of a multi-line skeleton', () => {
    const { container } = render(<Skeleton lines={3} />)
    const bars = Array.from(container.firstElementChild?.children ?? [])
    expect(bars[0].className).toContain('w-full')
    expect(bars[2].className).toContain('w-2/3')
  })

  it('passes a caller className through to the bar', () => {
    const { container } = render(<Skeleton className="h-10 w-32" />)
    expect(container.firstElementChild?.className).toContain('h-10 w-32')
  })
})

describe('[UIF AC-142] reduced motion', () => {
  it('turns the pulse off under prefers-reduced-motion', () => {
    const { container } = render(<Skeleton />)
    const classes = container.firstElementChild?.className ?? ''
    expect(classes).toContain('animate-pulse')
    expect(classes).toContain('motion-reduce:animate-none')
  })

  it('applies the same reduced-motion rule to every line', () => {
    const { container } = render(<Skeleton lines={2} />)
    for (const bar of Array.from(container.firstElementChild?.children ?? [])) {
      expect(bar.className).toContain('motion-reduce:animate-none')
    }
  })
})
