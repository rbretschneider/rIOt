import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import LogSettings from './LogSettings'
import { settingsApi } from '../../api/settings'
import { renderWithProviders } from '../../test/renderWithProviders'

vi.mock('../../api/settings', () => ({
  settingsApi: {
    getLogs: vi.fn(),
    clearLogs: vi.fn(),
    getServerErrorAlert: vi.fn(),
    saveServerErrorAlert: vi.fn(),
    getNotificationChannels: vi.fn(),
  },
}))

const log = {
  id: 1,
  timestamp: '2026-10-17T14:03:22Z',
  level: 'WARN',
  message: 'disk almost full',
  attrs: {},
  source: 'collector',
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsApi.getLogs).mockResolvedValue([log] as never)
  vi.mocked(settingsApi.clearLogs).mockResolvedValue(undefined as never)
  vi.mocked(settingsApi.getServerErrorAlert).mockResolvedValue(undefined as never)
  vi.mocked(settingsApi.getNotificationChannels).mockResolvedValue([])
})

async function clickClear() {
  renderWithProviders(<LogSettings />)
  await screen.findByText('disk almost full')
  await userEvent.click(screen.getByRole('button', { name: 'Clear' }))
}

describe('[UIF AC-071] clear logs danger dialog says cannot be undone', () => {
  it('opens a danger dialog titled "Delete all server logs" whose message says it cannot be undone', async () => {
    await clickClear()
    const dialog = screen.getByRole('dialog', { name: 'Delete all server logs' })
    expect(dialog).toHaveTextContent('cannot be undone')
    expect(within(dialog).getByRole('button', { name: 'Delete logs' })).toHaveAttribute('data-variant', 'danger')
  })

  it('does not call clearLogs when cancelled', async () => {
    await clickClear()
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(settingsApi.clearLogs).not.toHaveBeenCalled()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('clears the logs and raises a success toast on confirm', async () => {
    await clickClear()
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete logs' }))
    await waitFor(() => expect(settingsApi.clearLogs).toHaveBeenCalledTimes(1))
    const region = screen.getByRole('region', { name: 'Notifications' })
    expect(await within(region).findByText('Server logs deleted')).toBeInTheDocument()
  })

  it('shows a failure inline and keeps the dialog open', async () => {
    vi.mocked(settingsApi.clearLogs).mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    await clickClear()
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete logs' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent(
      'HTTP 500: Internal Server Error',
    )
  })

  it('keeps the trigger button label "Clear" and disables it when there are no logs', async () => {
    vi.mocked(settingsApi.getLogs).mockResolvedValue([])
    renderWithProviders(<LogSettings />)
    expect(await screen.findByRole('button', { name: 'Clear' })).toBeDisabled()
  })
})
