import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import NotificationSettings from './NotificationSettings'
import { settingsApi } from '../../api/settings'
import { renderWithProviders } from '../../test/renderWithProviders'

vi.mock('../../api/settings', () => ({
  settingsApi: {
    getNotificationChannels: vi.fn(),
    deleteNotificationChannel: vi.fn(),
    createNotificationChannel: vi.fn(),
    updateNotificationChannel: vi.fn(),
    testNotificationChannel: vi.fn(),
  },
}))

const channel = {
  id: 5,
  name: 'Ops ntfy',
  type: 'ntfy',
  enabled: true,
  config: { topic: 'riot-alerts' },
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsApi.getNotificationChannels).mockResolvedValue([channel] as never)
  vi.mocked(settingsApi.deleteNotificationChannel).mockResolvedValue(undefined as never)
})

async function clickDelete() {
  renderWithProviders(<NotificationSettings />)
  await screen.findByText('Ops ntfy')
  await userEvent.click(screen.getByRole('button', { name: 'Delete' }))
}

describe('[UIF AC-072] delete channel toasts Channel deleted', () => {
  it('opens a danger dialog naming the channel', async () => {
    await clickDelete()
    const dialog = screen.getByRole('dialog', { name: 'Delete channel' })
    expect(dialog).toHaveTextContent('"Ops ntfy"')
    expect(within(dialog).getByRole('button', { name: 'Delete' })).toHaveAttribute('data-variant', 'danger')
  })

  it('does not call the API when cancelled', async () => {
    await clickDelete()
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cancel' }))
    expect(settingsApi.deleteNotificationChannel).not.toHaveBeenCalled()
  })

  it('deletes the channel by id and raises a "Channel deleted" success toast on confirm', async () => {
    await clickDelete()
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(settingsApi.deleteNotificationChannel).toHaveBeenCalledWith(5))
    const region = screen.getByRole('region', { name: 'Notifications' })
    expect(await within(region).findByText('Channel deleted')).toBeInTheDocument()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('keeps the dialog open with an inline error when the delete fails', async () => {
    vi.mocked(settingsApi.deleteNotificationChannel).mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    await clickDelete()
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent(
      'HTTP 500: Internal Server Error',
    )
  })
})

describe('[UIF AC-030] channel edit modal is a named dialog', () => {
  it('opens "Add Notification Channel" as a dialog and closes it on Escape', async () => {
    renderWithProviders(<NotificationSettings />)
    await userEvent.click(await screen.findByRole('button', { name: 'Add Channel' }))
    expect(screen.getByRole('dialog', { name: 'Add Notification Channel' })).toHaveAttribute('aria-modal', 'true')
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})
