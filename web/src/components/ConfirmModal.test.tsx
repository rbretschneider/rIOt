import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import ConfirmModal from './ConfirmModal'

describe('ConfirmModal', () => {
  it('renders title and message', () => {
    render(
      <ConfirmModal
        title="Delete Device"
        message="Are you sure?"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    expect(screen.getByText('Delete Device')).toBeInTheDocument()
    expect(screen.getByText('Are you sure?')).toBeInTheDocument()
  })

  it('calls onConfirm when confirm button clicked', async () => {
    const onConfirm = vi.fn()
    render(
      <ConfirmModal
        title="Delete"
        message="Sure?"
        onConfirm={onConfirm}
        onCancel={vi.fn()}
      />
    )

    await userEvent.click(screen.getByText('Confirm'))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('calls onCancel when cancel button clicked', async () => {
    const onCancel = vi.fn()
    render(
      <ConfirmModal
        title="Delete"
        message="Sure?"
        onConfirm={vi.fn()}
        onCancel={onCancel}
      />
    )

    await userEvent.click(screen.getByText('Cancel'))
    expect(onCancel).toHaveBeenCalledOnce()
  })

  it('renders custom confirm label', () => {
    render(
      <ConfirmModal
        title="Delete"
        message="Sure?"
        confirmLabel="Yes, delete"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    expect(screen.getByText('Yes, delete')).toBeInTheDocument()
  })

  it('applies danger variant by default', () => {
    render(
      <ConfirmModal
        title="Delete"
        message="Sure?"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    const confirmBtn = screen.getByText('Confirm')
    expect(confirmBtn).toHaveAttribute('data-variant', 'danger')
  })

  it('applies primary variant when specified', () => {
    render(
      <ConfirmModal
        title="Save"
        message="Apply?"
        confirmVariant="primary"
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />
    )
    const confirmBtn = screen.getByText('Confirm')
    expect(confirmBtn).toHaveAttribute('data-variant', 'primary')
  })
})

describe('[UIF AC-030] ConfirmModal is a named dialog', () => {
  it('exposes role=dialog named by its title with aria-modal', () => {
    render(<ConfirmModal title="Delete Device" message="Are you sure?" onConfirm={vi.fn()} onCancel={vi.fn()} />)
    const dialog = screen.getByRole('dialog', { name: 'Delete Device' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(dialog).toHaveAccessibleDescription('Are you sure?')
  })

  it('focuses Cancel, not Confirm, when it opens (SEC-001)', () => {
    render(<ConfirmModal title="Update" message="Apply?" confirmVariant="primary" onConfirm={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
  })

  it('calls onCancel on Escape and on a backdrop click, never onConfirm', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    render(<ConfirmModal title="Delete" message="Sure?" onConfirm={onConfirm} onCancel={onCancel} />)
    await userEvent.keyboard('{Escape}')
    await userEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onCancel).toHaveBeenCalledTimes(2)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('has no ad-hoc z-index: it sits on the shared modal layer', () => {
    render(<ConfirmModal title="Delete" message="Sure?" onConfirm={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByTestId('modal-backdrop').className).toContain('z-(--z-modal)')
  })
})
