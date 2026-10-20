import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import Alerts from './Alerts'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'

vi.mock('../api/client', () => ({
  api: {
    getEvents: vi.fn(),
    acknowledgeEvent: vi.fn(),
    acknowledgeAllEvents: vi.fn(),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({
    data: [{ id: 'dev-1', hostname: 'Web-01.lan' }],
    wsConnected: true,
  }),
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

const event = (over: Record<string, unknown> = {}) => ({
  id: 1,
  device_id: 'dev-1',
  type: 'disk_high',
  severity: 'warning',
  message: 'Disk / is 91% full',
  created_at: '2026-10-17T14:03:22Z',
  ...over,
})

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  vi.mocked(api.getEvents).mockResolvedValue([event()] as never)
})

describe('[APP-WORKSPACE AC-007] alert header preserves filtering and acknowledgement', () => {
  it('keeps one page title and usable filter and acknowledgement actions', async () => {
    const user = userEvent.setup()
    vi.mocked(api.acknowledgeAllEvents).mockResolvedValue(undefined)
    renderWithProviders(<Alerts />)
    await screen.findByText('Disk / is 91% full')
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(screen.getByRole('heading', { level: 1, name: 'Events & Alerts' })).toBeInTheDocument()
    const search = screen.getByRole('textbox', { name: 'Filter events' })
    await user.type(search, 'no matching message')
    expect(screen.getByText('No events found')).toBeInTheDocument()
    await user.clear(search)
    await user.click(screen.getByRole('button', { name: 'Acknowledge All' }))
    await waitFor(() => expect(api.acknowledgeAllEvents).toHaveBeenCalledOnce())
  })
})

describe('[APP-WORKSPACE AC-009] alerts link to the named Docker resource', () => {
  it('uses the canonical scoped name even when the old container ID is absent', async () => {
    vi.mocked(api.getEvents).mockResolvedValue([event({ container_name: '/web?blue', container_id: undefined })] as never)
    renderWithProviders(<Alerts />)
    expect(await screen.findByRole('link', { name: 'web?blue' })).toHaveAttribute('href', '/docker/containers/dev-1/web%3Fblue')
  })
})

describe('[UIF AC-021] alerts show mapped labels', () => {
  it('shows "Warning" and "Disk usage high" and neither raw value', async () => {
    renderWithProviders(<Alerts />)
    const row = (await screen.findByText('Disk / is 91% full')).closest('tr') as HTMLElement

    expect(within(row).getByText('Warning')).toHaveAttribute('data-tone', 'warn')
    expect(within(row).getByText('Disk usage high')).toBeInTheDocument()
    expect(within(row).queryByText('warning')).toBeNull()
    expect(within(row).queryByText('disk_high')).toBeNull()
    expect(within(row).queryByText('disk high')).toBeNull()
  })

  it('maps critical to a danger-tone "Critical" badge', async () => {
    vi.mocked(api.getEvents).mockResolvedValue([event({ severity: 'critical', type: 'container_oom_killed' })] as never)
    renderWithProviders(<Alerts />)
    const row = (await screen.findByText('Disk / is 91% full')).closest('tr') as HTMLElement
    expect(within(row).getByText('Critical')).toHaveAttribute('data-tone', 'danger')
    expect(within(row).getByText('Container OOM-killed')).toBeInTheDocument()
  })

  it('humanizes an event type the label map does not know', async () => {
    vi.mocked(api.getEvents).mockResolvedValue([event({ type: 'cpu_high' })] as never)
    renderWithProviders(<Alerts />)
    const row = (await screen.findByText('Disk / is 91% full')).closest('tr') as HTMLElement
    expect(within(row).getByText('Cpu high')).toBeInTheDocument()
  })

  it('marks unacknowledged warning and critical rows with token border classes instead of inline hex', async () => {
    vi.mocked(api.getEvents).mockResolvedValue([
      event({ id: 1, severity: 'critical', message: 'crit msg' }),
      event({ id: 2, severity: 'warning', message: 'warn msg' }),
      event({ id: 3, severity: 'info', message: 'info msg' }),
    ] as never)
    renderWithProviders(<Alerts />)
    const critical = (await screen.findByText('crit msg')).closest('tr') as HTMLElement
    const warning = screen.getByText('warn msg').closest('tr') as HTMLElement
    const info = screen.getByText('info msg').closest('tr') as HTMLElement
    expect(critical.className).toContain('border-l-danger-fg')
    expect(warning.className).toContain('border-l-warn-fg')
    expect(info.className).not.toContain('border-l-')
    expect(critical.getAttribute('style') ?? '').toBe('')
  })
})

describe('[UIF AC-109] timestamps carry UTC label', () => {
  it('renders the event time as YYYY-MM-DD HH:mm:ss UTC under the default preference', async () => {
    renderWithProviders(<Alerts />)
    const stamp = await screen.findByText('2026-10-17 14:03:22 UTC')
    expect(stamp.tagName).toBe('TIME')
  })
})

describe('[UIF AC-123] hostname not uppercased', () => {
  it('renders the device hostname link in its reported case with no uppercase class', async () => {
    renderWithProviders(<Alerts />)
    const link = await screen.findByRole('link', { name: 'Web-01.lan' })
    expect(link.textContent).toBe('Web-01.lan')
    expect(link.className).not.toContain('uppercase')
  })
})
