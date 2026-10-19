import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Button, type ButtonSize, type ButtonVariant } from './Button'

const VARIANTS: ButtonVariant[] = ['primary', 'secondary', 'ghost', 'danger']
const SIZES: ButtonSize[] = ['sm', 'md']

describe('[UIF AC-008] variants and sizes are distinct', () => {
  const combos = VARIANTS.flatMap(variant => SIZES.map(size => ({ variant, size })))

  it.each(combos)('exposes data-variant=$variant and data-size=$size', ({ variant, size }) => {
    render(<Button variant={variant} size={size}>Go</Button>)
    const button = screen.getByRole('button', { name: 'Go' })
    expect(button).toHaveAttribute('data-variant', variant)
    expect(button).toHaveAttribute('data-size', size)
  })

  it('gives all eight combinations a different class set', () => {
    const classSets = combos.map(({ variant, size }) => {
      const { unmount } = render(<Button variant={variant} size={size}>Go</Button>)
      const classes = screen.getByRole('button', { name: 'Go' }).className
      unmount()
      return classes
    })
    expect(new Set(classSets).size, 'every variant/size pair must render different classes').toBe(8)
  })

  it('defaults to the secondary variant and md size', () => {
    render(<Button>Go</Button>)
    const button = screen.getByRole('button', { name: 'Go' })
    expect(button).toHaveAttribute('data-variant', 'secondary')
    expect(button).toHaveAttribute('data-size', 'md')
  })

  it('merges a caller className without dropping the variant classes', () => {
    render(<Button variant="primary" className="w-full">Go</Button>)
    const button = screen.getByRole('button', { name: 'Go' })
    expect(button.className).toContain('w-full')
    expect(button.className).toContain('bg-accent')
  })
})

describe('[UIF AC-009] default type does not submit forms', () => {
  it('renders type="button" when no type is given', () => {
    render(<Button>Go</Button>)
    expect(screen.getByRole('button', { name: 'Go' })).toHaveAttribute('type', 'button')
  })

  it('does not submit an enclosing form when clicked', async () => {
    const onSubmit = vi.fn((e: React.FormEvent) => e.preventDefault())
    render(
      <form onSubmit={onSubmit}>
        <Button>Go</Button>
      </form>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'Go' }))
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('submits when the caller opts in with type="submit"', async () => {
    const onSubmit = vi.fn((e: React.FormEvent) => e.preventDefault())
    render(
      <form onSubmit={onSubmit}>
        <Button type="submit">Save</Button>
      </form>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSubmit).toHaveBeenCalledOnce()
  })
})

describe('[UIF AC-010] disabled blocks clicks', () => {
  it('does not call onClick when disabled', async () => {
    const onClick = vi.fn()
    render(<Button disabled onClick={onClick}>Go</Button>)
    await userEvent.click(screen.getByRole('button', { name: 'Go' }))
    expect(onClick).not.toHaveBeenCalled()
  })

  it('is natively disabled and styled as disabled', () => {
    render(<Button disabled>Go</Button>)
    const button = screen.getByRole('button', { name: 'Go' })
    expect(button).toBeDisabled()
    expect(button.className).toContain('disabled:opacity-50')
    expect(button.className).toContain('disabled:cursor-not-allowed')
  })

  it('calls onClick when enabled', async () => {
    const onClick = vi.fn()
    render(<Button onClick={onClick}>Go</Button>)
    await userEvent.click(screen.getByRole('button', { name: 'Go' }))
    expect(onClick).toHaveBeenCalledOnce()
  })
})

describe('[UIF AC-011] pending state', () => {
  it('shows a spinner and the pending label, is disabled and busy', () => {
    const { container } = render(<Button pending pendingLabel="Deleting…">Delete</Button>)
    const button = screen.getByRole('button', { name: 'Deleting…' })
    expect(button).toBeDisabled()
    expect(button).toHaveAttribute('aria-busy', 'true')
    expect(container.querySelector('svg.animate-spin')).not.toBeNull()
    expect(screen.queryByText('Delete')).toBeNull()
  })

  it('falls back to the normal children when no pending label is given', () => {
    render(<Button pending>Save</Button>)
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
  })

  it('does not call onClick while pending', async () => {
    const onClick = vi.fn()
    render(<Button pending onClick={onClick}>Save</Button>)
    await userEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onClick).not.toHaveBeenCalled()
  })

  it('has no aria-busy and no spinner when not pending', () => {
    const { container } = render(<Button>Save</Button>)
    expect(screen.getByRole('button', { name: 'Save' })).not.toHaveAttribute('aria-busy')
    expect(container.querySelector('svg')).toBeNull()
  })

  it('hides the spinner from assistive technology', () => {
    const { container } = render(<Button pending>Save</Button>)
    expect(container.querySelector('svg')).toHaveAttribute('aria-hidden', 'true')
  })
})

describe('[UIF AC-012] focus-visible ring', () => {
  it.each(VARIANTS)('the %s button carries the shared focus-visible ring', variant => {
    render(<Button variant={variant}>Go</Button>)
    const classes = screen.getByRole('button', { name: 'Go' }).className
    expect(classes).toContain('focus-visible:ring-2')
    expect(classes).toContain('focus-visible:ring-accent')
  })
})
