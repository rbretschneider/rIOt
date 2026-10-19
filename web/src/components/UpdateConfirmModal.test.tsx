import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import UpdateConfirmModal from './UpdateConfirmModal'
import type { ContainerInfo } from '../types/models'

const containers = [
  { id: 'c1', name: 'web', image: 'nginx:1.27', riot: undefined },
  { id: 'c2', name: 'db', image: 'postgres:16', riot: undefined },
] as unknown as ContainerInfo[]

describe('[UIF AC-030] UpdateConfirmModal is a named dialog', () => {
  it('exposes role=dialog named by its title', () => {
    render(<UpdateConfirmModal title="Update 2 containers" containers={containers} onConfirm={vi.fn()} onCancel={vi.fn()} />)
    const dialog = screen.getByRole('dialog', { name: 'Update 2 containers' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
  })

  it('lists every container and the interruption warning', () => {
    render(<UpdateConfirmModal title="Update" containers={containers} onConfirm={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByText('web')).toBeInTheDocument()
    expect(screen.getByText('postgres:16')).toBeInTheDocument()
    expect(screen.getByText('Running services will be briefly interrupted.')).toBeInTheDocument()
  })

  it('shows the network execution order when given', () => {
    render(
      <UpdateConfirmModal
        title="Update"
        containers={containers}
        networkOrder={{ parent: 'vpn', dependents: ['web', 'db'] }}
        onConfirm={vi.fn()}
        onCancel={vi.fn()}
      />,
    )
    expect(screen.getByText('Execution order:')).toBeInTheDocument()
    expect(screen.getByText('Update parent: vpn')).toBeInTheDocument()
    expect(screen.getByText('Stop dependents: web, db')).toBeInTheDocument()
  })

  it('focuses Cancel, not Confirm Update, when it opens (SEC-001)', () => {
    render(<UpdateConfirmModal title="Update" containers={containers} onConfirm={vi.fn()} onCancel={vi.fn()} />)
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
  })

  it('calls onConfirm only when Confirm Update is clicked', async () => {
    const onConfirm = vi.fn()
    render(<UpdateConfirmModal title="Update" containers={containers} onConfirm={onConfirm} onCancel={vi.fn()} />)
    await userEvent.click(screen.getByRole('button', { name: 'Confirm Update' }))
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('calls onCancel on Cancel, Escape and backdrop, never onConfirm', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    render(<UpdateConfirmModal title="Update" containers={containers} onConfirm={onConfirm} onCancel={onCancel} />)
    await userEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    await userEvent.keyboard('{Escape}')
    await userEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onCancel).toHaveBeenCalledTimes(3)
    expect(onConfirm).not.toHaveBeenCalled()
  })
})
