import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders } from '../../test/renderWithProviders'
import { ApiError } from '../../api/errors'
import AutoUpdateToggle from './AutoUpdateToggle'

const mock = vi.hoisted(() => ({ getAutoUpdates: vi.fn(), setAutoUpdate: vi.fn() }))
vi.mock('../../api/client', () => ({ api: mock }))

const policy = (target: string, is_stack: boolean, enabled = true) => ({
  id: 1, device_id: 'host-1', target, is_stack, compose_work_dir: is_stack ? '/srv/web' : '', enabled, created_at: '',
})

beforeEach(() => {
  vi.clearAllMocks()
  mock.setAutoUpdate.mockResolvedValue({ status: 'ok' })
})

describe('[AC-011] detail toggle reads only its own kind', () => {
  it('a stack policy does not tick the container toggle of the same name', async () => {
    mock.getAutoUpdates.mockResolvedValue([policy('web', true)])
    renderWithProviders(<AutoUpdateToggle deviceId="host-1" target="web" workingDir="" />)
    const box = screen.getByRole('checkbox', { name: 'Auto update' })
    await waitFor(() => expect(box).toBeEnabled())
    expect(box).not.toBeChecked()
  })
  it('a container policy does not tick the stack toggle of the same name', async () => {
    mock.getAutoUpdates.mockResolvedValue([policy('web', false)])
    renderWithProviders(<AutoUpdateToggle deviceId="host-1" target="web" workingDir="/srv/web" stack />)
    const box = screen.getByRole('checkbox', { name: 'Auto update' })
    await waitFor(() => expect(box).toBeEnabled())
    expect(box).not.toBeChecked()
  })
  it('shows its own kind and saves with that kind', async () => {
    const user = userEvent.setup()
    mock.getAutoUpdates.mockResolvedValue([policy('web', false), policy('web', true, false)])
    renderWithProviders(<AutoUpdateToggle deviceId="host-1" target="web" workingDir="/srv/web" stack />)
    const box = screen.getByRole('checkbox', { name: 'Auto update' })
    await waitFor(() => expect(box).toBeEnabled())
    expect(box).not.toBeChecked()
    await user.click(box)
    expect(mock.setAutoUpdate).toHaveBeenCalledExactlyOnceWith('host-1', 'web', true, '/srv/web', true)
  })
})

describe('[AC-003] detail toggle errors', () => {
  it('raises a toast instead of an inline error panel and reverts', async () => {
    const user = userEvent.setup()
    mock.getAutoUpdates.mockResolvedValue([])
    mock.setAutoUpdate.mockRejectedValue(new ApiError(409, 'clash', 'auto_update_name_clash', { existing_kind: 'stack' }))
    renderWithProviders(<AutoUpdateToggle deviceId="host-1" target="web" workingDir="" />)
    const box = screen.getByRole('checkbox', { name: 'Auto update' })
    await waitFor(() => expect(box).toBeEnabled())
    await user.click(box)
    expect(await screen.findByText('Auto-update not changed for container web')).toBeInTheDocument()
    expect(screen.getByText('A stack with the same name on this host has auto-update on. Turn that off first.')).toBeInTheDocument()
    expect(box).not.toBeChecked()
    expect(screen.queryByRole('button', { name: 'Retry' })).not.toBeInTheDocument()
  })
})
