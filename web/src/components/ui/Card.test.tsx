import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Card } from './Card'

describe('[UIF AC-013] card title is a heading on raised surface', () => {
  it('exposes the title as a level-2 heading by default', () => {
    render(<Card title="Hardware">body</Card>)
    expect(screen.getByRole('heading', { name: 'Hardware', level: 2 })).toBeInTheDocument()
  })

  it('renders a level-3 heading when titleAs is h3', () => {
    render(<Card title="Hardware" titleAs="h3">body</Card>)
    expect(screen.getByRole('heading', { name: 'Hardware', level: 3 })).toBeInTheDocument()
  })

  it('uses the raised and border tokens on the container', () => {
    const { container } = render(<Card title="Hardware">body</Card>)
    const card = container.firstElementChild as HTMLElement
    expect(card.className).toContain('bg-raised')
    expect(card.className).toContain('border-border')
  })

  it('renders children and the actions slot', () => {
    render(<Card title="Hardware" actions={<button>Edit</button>}>inner content</Card>)
    expect(screen.getByText('inner content')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Edit' })).toBeInTheDocument()
  })

  it('renders no header and no heading when there is neither title nor actions', () => {
    render(<Card>just body</Card>)
    expect(screen.queryByRole('heading')).toBeNull()
  })

  it('removes body padding with padding="none"', () => {
    render(<Card padding="none"><span data-testid="inner">x</span></Card>)
    expect(screen.getByTestId('inner').parentElement?.className ?? '').not.toContain('p-4')
  })

  it('passes through extra attributes and className', () => {
    render(<Card data-testid="card" className="mt-4">x</Card>)
    const card = screen.getByTestId('card')
    expect(card.className).toContain('mt-4')
    expect(card.tagName).toBe('SECTION')
  })
})
