import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import CommandProgress from './CommandProgress'
import { api } from '../../api/client'
import { renderWithProviders } from '../../test/renderWithProviders'

vi.mock('../../api/client', () => ({
  api: {
    getCommand: vi.fn(),
    cancelCommand: vi.fn(),
  },
}))

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getCommand).mockResolvedValue({
    id: 'cmd-1',
    device_id: 'dev 1',
    action: 'reboot',
    status: 'succeeded',
    result_msg: 'reboot initiated',
    step: '',
  } as never)
})

describe('[AC-042] CommandProgress activity link (X-3)', () => {
  it('shows the Docker activity link by default, with host and command URL-encoded', async () => {
    renderWithProviders(<CommandProgress deviceId="dev 1" commandId="cmd/1" />)

    const link = await screen.findByRole('link', { name: 'View steps and output' })
    expect(link).toHaveAttribute('href', '/docker/activity?host=dev%201&command=cmd%2F1')
  })

  it('hides the link when showActivityLink is false, as in the host-command modal', async () => {
    renderWithProviders(<CommandProgress deviceId="dev 1" commandId="cmd/1" showActivityLink={false} />)

    await screen.findByText('reboot initiated')
    expect(screen.queryByRole('link', { name: 'View steps and output' })).toBeNull()
  })
})
