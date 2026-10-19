import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import { onlineManager } from '@tanstack/react-query'
import userEvent from '@testing-library/user-event'
import DeviceDetail from './DeviceDetail'
import { api } from '../api/client'
import { ApiError } from '../api/errors'
import { renderWithProviders } from '../test/renderWithProviders'

vi.mock('../api/client', () => ({
  api: {
    getDevice: vi.fn(),
    getEvents: vi.fn(),
    getServerUpdate: vi.fn(),
    getPatchStatus: vi.fn(),
    getDeviceAlertRules: vi.fn(),
    getDeviceLogs: vi.fn(),
    getHeartbeatHistory: vi.fn(),
    getSecurityScore: vi.fn(),
    sendCommand: vi.fn(),
    deleteDevice: vi.fn(),
  },
}))

vi.mock('../hooks/useDevices', () => ({
  useDevices: () => ({ wsConnected: false }),
}))

vi.mock('../hooks/useFeatures', () => ({
  useFeatures: () => ({ isEnabled: () => true }),
}))

// The remediation flow is driven through the real prop contract; the modal body is not under test here.
vi.mock('../components/SecurityScoreModal', () => ({
  default: ({ onRunCommand }: { onRunCommand?: (action: string, params?: Record<string, unknown>) => void }) => (
    <div role="dialog" aria-label="Security score details">
      <button onClick={() => onRunCommand?.('enable_auto_updates', {})}>Run remediation</button>
      <button onClick={() => onRunCommand?.('reboot', {})}>Run failing remediation</button>
    </div>
  ),
}))

type DeviceStatus = 'online' | 'offline'

function deviceResponse(over: { status?: DeviceStatus; hostname?: string; pending?: number } = {}) {
  return {
    device: {
      id: 'dev-1',
      hostname: over.hostname ?? 'web-01',
      short_id: 'abc123',
      arch: 'amd64',
      status: over.status ?? 'online',
      agent_version: '2.0.4',
      primary_ip: '192.168.1.1',
      tags: [],
      docker_available: false,
      auto_patch: false,
      last_heartbeat: '2026-10-17T12:00:00Z',
      created_at: '2026-10-01T00:00:00Z',
      updated_at: '2026-10-17T00:00:00Z',
    },
    latest_telemetry: {
      id: 1,
      device_id: 'dev-1',
      timestamp: '2026-10-17T12:00:00Z',
      data: { updates: { pending_updates: over.pending ?? 3 } },
    },
    agent_connected: true,
  }
}

const SCORE = {
  overall_score: 72,
  max_score: 100,
  grade: 'C',
  categories: [],
  evaluated_at: '2026-10-17T12:00:00Z',
}

function renderPage() {
  return renderWithProviders(<DeviceDetail />, {
    route: '/devices/dev-1',
    path: '/devices/:id',
    routes: [{ path: '/', element: <div>Fleet page</div> }],
    showLocation: true,
  })
}

const toastRegion = () => screen.getByRole('region', { name: 'Notifications' })
const dialog = () => screen.getByRole('dialog')

async function openPowerAction(name: 'Reboot' | 'Shutdown') {
  await userEvent.click(await screen.findByRole('button', { name: 'Device power actions' }))
  await userEvent.click(screen.getByRole('button', { name }))
}

async function openDelete() {
  await userEvent.click(await screen.findByRole('button', { name: 'Delete' }))
}

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(api.getDevice).mockResolvedValue(deviceResponse() as never)
  vi.mocked(api.getEvents).mockResolvedValue([])
  vi.mocked(api.getServerUpdate).mockResolvedValue({ latest_version: '2.0.5' } as never)
  vi.mocked(api.getPatchStatus).mockResolvedValue([])
  vi.mocked(api.getDeviceAlertRules).mockResolvedValue([])
  vi.mocked(api.getDeviceLogs).mockResolvedValue([])
  vi.mocked(api.getHeartbeatHistory).mockResolvedValue([])
  vi.mocked(api.getSecurityScore).mockResolvedValue(SCORE as never)
  vi.mocked(api.sendCommand).mockResolvedValue({ id: 'c1', action: 'reboot', status: 'pending' } as never)
  vi.mocked(api.deleteDevice).mockResolvedValue(undefined as never)
})

describe('[UIF AC-073] delete failure inline, no alert', () => {
  it('keeps the dialog open with the error inline and never calls window.alert', async () => {
    // happy-dom has no window.alert; define a no-op first so there is a function to spy on.
    Object.defineProperty(window, 'alert', { configurable: true, writable: true, value: () => {} })
    const alertSpy = vi.spyOn(window, 'alert')
    vi.mocked(api.deleteDevice).mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    renderPage()
    await openDelete()

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))

    expect(await within(dialog()).findByRole('alert')).toHaveTextContent('HTTP 500: Internal Server Error')
    expect(alertSpy).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog', { name: 'Delete Device' })).toBeInTheDocument()
    alertSpy.mockRestore()
    Reflect.deleteProperty(window, 'alert')
  })

  it('does not log to the console', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    vi.mocked(api.deleteDevice).mockRejectedValue(new Error('boom'))
    renderPage()
    await openDelete()
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))
    await within(dialog()).findByRole('alert')
    expect(errorSpy).not.toHaveBeenCalledWith('Delete device failed:', expect.anything())
    errorSpy.mockRestore()
  })
})

describe('[UIF AC-074] delete success navigates and toasts hostname', () => {
  it('lands on the Fleet route and shows a success toast naming the hostname', async () => {
    vi.mocked(api.getDevice).mockResolvedValue(deviceResponse({ hostname: 'web-01' }) as never)
    renderPage()
    await openDelete()
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))

    await waitFor(() => expect(screen.getByTestId('location')).toHaveTextContent(/^\/$/))
    expect(within(toastRegion()).getByText('Device "web-01" deleted')).toBeInTheDocument()
    expect(api.deleteDevice).toHaveBeenCalledTimes(1)
  })
})

describe('[UIF SEC-002] the delete scope is fixed when the dialog opens', () => {
  it('sends uninstall=true and says so for an online device, even if it goes offline before confirming', async () => {
    const { queryClient } = renderPage()
    await openDelete()
    expect(dialog()).toHaveTextContent('The agent will be uninstalled from the device.')

    act(() => {
      queryClient.setQueryData(['device', 'dev-1'], deviceResponse({ status: 'offline' }))
    })
    expect(dialog()).toHaveTextContent('The agent will be uninstalled from the device.')

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.deleteDevice).toHaveBeenCalledTimes(1))
    expect(api.deleteDevice).toHaveBeenCalledWith('dev-1', true)
  })

  it('sends uninstall=false and says so for an offline device, even if it comes online before confirming', async () => {
    vi.mocked(api.getDevice).mockResolvedValue(deviceResponse({ status: 'offline' }) as never)
    const { queryClient } = renderPage()
    await openDelete()
    expect(dialog()).toHaveTextContent('you may need to manually uninstall the agent')

    act(() => {
      queryClient.setQueryData(['device', 'dev-1'], deviceResponse({ status: 'online' }))
    })
    expect(dialog()).toHaveTextContent('you may need to manually uninstall the agent')

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.deleteDevice).toHaveBeenCalledTimes(1))
    expect(api.deleteDevice).toHaveBeenCalledWith('dev-1', false)
  })

  it('keeps the same scope through a failed attempt and the retry, even if status flips in between', async () => {
    vi.mocked(api.getDevice).mockResolvedValue(deviceResponse({ status: 'offline' }) as never)
    vi.mocked(api.deleteDevice)
      .mockRejectedValueOnce(new Error('HTTP 500'))
      .mockResolvedValueOnce(undefined as never)
    const { queryClient } = renderPage()
    await openDelete()

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))
    await within(dialog()).findByRole('alert')

    act(() => {
      queryClient.setQueryData(['device', 'dev-1'], deviceResponse({ status: 'online' }))
    })
    expect(dialog()).toHaveTextContent('you may need to manually uninstall the agent')

    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(api.deleteDevice).toHaveBeenCalledTimes(2))
    expect(vi.mocked(api.deleteDevice).mock.calls.map(c => c[1])).toEqual([false, false])
  })

  it('reads the scope again from the device the next time the dialog is opened', async () => {
    const { queryClient } = renderPage()
    await openDelete()
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Cancel' }))

    act(() => {
      queryClient.setQueryData(['device', 'dev-1'], deviceResponse({ status: 'offline' }))
    })
    await openDelete()
    expect(dialog()).toHaveTextContent('you may need to manually uninstall the agent')
  })
})

describe('[UIF AC-081] no inline command banners', () => {
  it('shows no "Command sent:" banner after a successful command and no error banner after a failed one', async () => {
    renderPage()
    await openPowerAction('Reboot')
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Reboot' }))
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.queryByText(/Command sent:/)).toBeNull()

    vi.mocked(api.sendCommand).mockRejectedValue(new Error('HTTP 409: Conflict'))
    await openPowerAction('Reboot')
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Reboot' }))
    await within(dialog()).findByRole('alert')

    const main = document.querySelector('.space-y-6') as HTMLElement
    const outside = Array.from(main.querySelectorAll('div')).filter(
      el => /bg-red-900\/30|bg-emerald-900\/30/.test(el.className) && !el.closest('[role="dialog"]') && !toastRegion().contains(el),
    )
    expect(outside).toHaveLength(0)
  })
})

describe('[UIF AC-082] command dialog variants', () => {
  it('Reboot is a danger dialog', async () => {
    renderPage()
    await openPowerAction('Reboot')
    expect(within(dialog()).getByRole('button', { name: 'Reboot' })).toHaveAttribute('data-variant', 'danger')
  })

  it('Shutdown is a danger dialog', async () => {
    renderPage()
    await openPowerAction('Shutdown')
    expect(within(dialog()).getByRole('button', { name: 'Shut Down' })).toHaveAttribute('data-variant', 'danger')
  })

  it('Patch is a primary dialog', async () => {
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /^Patch \(3\)/ }))
    expect(screen.getByRole('dialog', { name: 'Patch Device' })).toBeInTheDocument()
    expect(within(dialog()).getByRole('button', { name: 'Patch' })).toHaveAttribute('data-variant', 'primary')
  })

  it('Update Agent is a primary dialog', async () => {
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Update Agent' }))
    expect(screen.getByRole('dialog', { name: 'Update Agent' })).toBeInTheDocument()
    expect(within(dialog()).getByRole('button', { name: 'Update' })).toHaveAttribute('data-variant', 'primary')
  })

  it('the Delete dialog is a danger dialog', async () => {
    renderPage()
    await openDelete()
    expect(within(dialog()).getByRole('button', { name: 'Delete' })).toHaveAttribute('data-variant', 'danger')
  })
})

describe('[UIF SEC-001] the opening gesture can never also confirm a device command', () => {
  it('focuses Cancel, not Confirm, in the Patch dialog', async () => {
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /^Patch \(3\)/ }))
    expect(within(dialog()).getByRole('button', { name: 'Cancel' })).toHaveFocus()
    expect(within(dialog()).getByRole('button', { name: 'Patch' })).not.toHaveFocus()
  })

  it('focuses Cancel, not Confirm, in the Update Agent dialog', async () => {
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Update Agent' }))
    expect(within(dialog()).getByRole('button', { name: 'Cancel' })).toHaveFocus()
  })

  it('a held Enter on the Patch button never sends the command', async () => {
    renderPage()
    const patch = await screen.findByRole('button', { name: /^Patch \(3\)/ })
    patch.focus()
    const user = userEvent.setup()
    await user.keyboard('{Enter>6/}')
    expect(api.sendCommand).not.toHaveBeenCalled()
  })
})

describe('[UIF AC-083] command toast is humanized', () => {
  it('toasts "OS update command sent to web-01 — Pending" with no raw tokens', async () => {
    vi.mocked(api.sendCommand).mockResolvedValue({ id: 'c1', action: 'os_update', status: 'pending' } as never)
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /^Patch \(3\)/ }))
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Patch' }))

    const toast = await within(toastRegion()).findByText(/OS update command sent to web-01/)
    expect(toast).toHaveTextContent('OS update command sent to web-01 — Pending')
    expect(document.body.textContent).not.toContain('os_update')
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'os_update', { mode: 'full' })
  })

  it('uses the requested action label even if the server echoes another', async () => {
    vi.mocked(api.sendCommand).mockResolvedValue({ id: 'c1', action: 'something_else', status: 'queued' } as never)
    renderPage()
    await openPowerAction('Reboot')
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Reboot' }))
    expect(await within(toastRegion()).findByText('Reboot command sent to web-01 — Queued')).toBeInTheDocument()
  })
})

describe('[UIF AC-084] command failure inline', () => {
  it('keeps the dialog open with the error and raises no toast', async () => {
    vi.mocked(api.sendCommand).mockRejectedValue(new Error('HTTP 409: Conflict'))
    renderPage()
    await openPowerAction('Reboot')
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Reboot' }))

    expect(await within(dialog()).findByRole('alert')).toHaveTextContent('HTTP 409: Conflict')
    expect(within(toastRegion()).queryByRole('alert')).toBeNull()
  })
})

describe('[UIF AC-085] remediation commands use toasts', () => {
  async function openRemediation() {
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: /Security score 72, grade C/ }))
  }

  it('toasts success when a remediation resolves', async () => {
    vi.mocked(api.sendCommand).mockResolvedValue({ id: 'c2', action: 'enable_auto_updates', status: 'sent' } as never)
    await openRemediation()
    await userEvent.click(screen.getByRole('button', { name: 'Run remediation' }))
    expect(
      await within(toastRegion()).findByText('Enable auto-updates command sent to web-01 — Sent'),
    ).toBeInTheDocument()
  })

  it('raises a persistent error toast with the message when a remediation rejects', async () => {
    vi.mocked(api.sendCommand).mockRejectedValue(new Error('HTTP 503: Service Unavailable'))
    await openRemediation()
    await userEvent.click(screen.getByRole('button', { name: 'Run failing remediation' }))
    const alert = await within(toastRegion()).findByRole('alert')
    expect(alert).toHaveTextContent('Command failed')
    expect(alert).toHaveTextContent('HTTP 503: Service Unavailable')
  })
})

describe('[UIF AC-086] fetch logs success toast', () => {
  it('toasts "Log fetch requested" when the request is accepted', async () => {
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Fetch Last 24h' }))
    expect(await within(toastRegion()).findByText('Log fetch requested')).toBeInTheDocument()
    expect(api.sendCommand).toHaveBeenCalledWith('dev-1', 'fetch_logs', { hours: 24, priority: 6 })
  })

  it('leaves no inline "Logs fetched" paragraph behind', async () => {
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Fetch Last 24h' }))
    await within(toastRegion()).findByText('Log fetch requested')
    expect(screen.queryByText(/Logs fetched/)).toBeNull()
  })
})

describe('[UIF AC-087] fetch logs failure toast', () => {
  it('raises an error toast containing the message', async () => {
    vi.mocked(api.sendCommand).mockRejectedValue(new Error('HTTP 504: Gateway Timeout'))
    renderPage()
    await userEvent.click(await screen.findByRole('button', { name: 'Fetch Last 24h' }))
    const alert = await within(toastRegion()).findByRole('alert')
    expect(alert).toHaveTextContent("Couldn't fetch logs")
    expect(alert).toHaveTextContent('HTTP 504: Gateway Timeout')
    expect(screen.queryByText(/Failed to fetch logs/)).toBeNull()
  })
})

describe('[UIF AC-094] 404 shows Device not found', () => {
  it('renders "Device not found" with a link back to Fleet and does not retry', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new ApiError(404, 'HTTP 404: Not Found'))
    renderPage()
    expect(await screen.findByRole('heading', { name: 'Device not found' })).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Back to Fleet' })).toHaveAttribute('href', '/')
    expect(screen.queryByText("Couldn't load device")).toBeNull()
    expect(api.getDevice).toHaveBeenCalledTimes(1)
  })
})

describe('[UIF AC-095] non-404 shows Couldn\'t load device', () => {
  it('renders the title, the message and a Retry button, and not "Device not found"', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new ApiError(500, 'HTTP 500: Internal Server Error'))
    renderPage()
    expect(await screen.findByRole('heading', { name: "Couldn't load device" })).toBeInTheDocument()
    expect(screen.getByText('HTTP 500: Internal Server Error')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument()
    expect(screen.queryByText('Device not found')).toBeNull()
  })

  it('Retry refetches and recovers', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new ApiError(500, 'HTTP 500: Internal Server Error'))
    renderPage()
    const retry = await screen.findByRole('button', { name: 'Retry' })
    vi.mocked(api.getDevice).mockResolvedValue(deviceResponse() as never)
    await userEvent.click(retry)
    expect(await screen.findByRole('heading', { name: 'web-01' })).toBeInTheDocument()
  })
})

describe('[UIF AC-098] network error treated as non-404', () => {
  it('shows the load failure with "Failed to fetch"', async () => {
    vi.mocked(api.getDevice).mockRejectedValue(new TypeError('Failed to fetch'))
    renderPage()
    expect(await screen.findByRole('heading', { name: "Couldn't load device" })).toBeInTheDocument()
    expect(screen.getByText('Failed to fetch')).toBeInTheDocument()
    expect(screen.queryByText('Device not found')).toBeNull()
  })
})

describe('[UIF AC-100] loading skeleton, no Loading... text', () => {
  it('shows a busy status with skeleton blocks while the device is loading', async () => {
    vi.mocked(api.getDevice).mockReturnValue(new Promise(() => {}))
    const { container } = renderPage()
    const status = await screen.findByRole('status', { name: 'Loading' })
    expect(status).toHaveAttribute('aria-busy', 'true')
    expect(container.querySelector('[aria-hidden="true"]')).not.toBeNull()
    expect(screen.queryByText('Loading...')).toBeNull()
  })
})

describe('[UIF AC-136] gauge caption and accessible name', () => {
  it('shows the visible "Security score" caption and a named gauge button', async () => {
    renderPage()
    const gauge = await screen.findByRole('button', { name: /Security score 72, grade C/ })
    expect(gauge).toHaveAccessibleName('Security score 72, grade C. Open details')
    expect(screen.getByText('Security score')).toBeVisible()
  })
})

describe('[UIF AC-137] Delete is a separated danger button', () => {
  it('lists Probes and Terminal in the Device sections navigation', async () => {
    renderPage()
    const nav = await screen.findByRole('navigation', { name: 'Device sections' })
    expect(within(nav).getByRole('link', { name: 'Probes' })).toBeInTheDocument()
    expect(within(nav).getByRole('link', { name: 'Terminal' })).toBeInTheDocument()
  })

  it('renders Delete outside that navigation as a danger-variant Button', async () => {
    renderPage()
    const nav = await screen.findByRole('navigation', { name: 'Device sections' })
    const del = screen.getByRole('button', { name: 'Delete' })
    expect(nav.contains(del)).toBe(false)
    expect(del).toHaveAttribute('data-variant', 'danger')
  })

  it('renders Delete for an offline device too', async () => {
    vi.mocked(api.getDevice).mockResolvedValue(deviceResponse({ status: 'offline' }) as never)
    renderPage()
    expect(await screen.findByRole('button', { name: 'Delete' })).toHaveAttribute('data-variant', 'danger')
  })
})

// Added by QA Engineer
// Covers AC-073, AC-084, BR-002: a failed command is reported exactly once, and never in the page body.
describe('[UIF AC-084] QA: command and delete failures are reported exactly once', () => {
  it('a rejected Reboot shows its message once, inside the dialog, and nowhere else', async () => {
    vi.mocked(api.sendCommand).mockRejectedValue(new Error('HTTP 409: Conflict'))
    renderPage()
    await openPowerAction('Reboot')
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Reboot' }))
    await within(dialog()).findByRole('alert')

    const hits = screen.getAllByText(/HTTP 409: Conflict/)
    expect(hits).toHaveLength(1)
    expect(dialog().contains(hits[0])).toBe(true)
    expect(toastRegion().querySelectorAll('li')).toHaveLength(0)
  })

  it('a rejected Delete shows its message once, inside the dialog, and raises no toast', async () => {
    vi.mocked(api.deleteDevice).mockRejectedValue(new Error('HTTP 500: Internal Server Error'))
    renderPage()
    await openDelete()
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Delete' }))
    await within(dialog()).findByRole('alert')

    expect(screen.getAllByText(/HTTP 500: Internal Server Error/)).toHaveLength(1)
    expect(toastRegion().querySelectorAll('li')).toHaveLength(0)
  })
})

// Covers SEC-003 on the real page: the user leaves DeviceDetail while Reboot is still in flight and the
// request then fails. The dialog is gone, so the failure must surface once as an error toast.
describe('[UIF SEC-003] QA: a Reboot that fails after the page was left is still reported once', () => {
  it('raises exactly one persistent error toast naming the action and the reason', async () => {
    let rejectCommand: (reason: Error) => void = () => {}
    vi.mocked(api.sendCommand).mockReturnValue(new Promise((_res, rej) => { rejectCommand = rej }) as never)
    renderPage()
    await openPowerAction('Reboot')
    await userEvent.click(within(dialog()).getByRole('button', { name: 'Reboot' }))
    await within(dialog()).findByRole('button', { name: 'Rebooting…' })

    // Browser-back equivalent: the route changes under the pending dialog (the Probes route is not mounted in this harness).
    act(() => screen.getByRole('link', { name: 'Probes', hidden: true }).click())
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())

    await act(async () => rejectCommand(new Error('HTTP 502: Bad Gateway')))

    const alerts = within(toastRegion()).getAllByRole('alert')
    expect(alerts).toHaveLength(1)
    expect(alerts[0]).toHaveTextContent('HTTP 502: Bad Gateway')
  })
})

// Covers FR-084, FR-085, BR-004: "Device not found" may only be shown for a 404. When the browser is
// offline TanStack Query PAUSES the fetch: no request is made, no error exists, isLoading is false.
// Regression test for QA-D2: DeviceQueryFallback keys off isPending (true while paused), so an offline
// user sees the loading state, not "This device no longer exists". DeviceContainers and
// ContainerDetailPage share the component.
describe('[UIF AC-095] QA: an offline browser is not "Device not found"', () => {
  afterEach(() => {
    onlineManager.setOnline(true)
  })

  it('shows a loading or load-failure state, not "Device not found", while the fetch is paused offline (QA-D2)', async () => {
    onlineManager.setOnline(false)
    renderPage()
    await new Promise(resolve => setTimeout(resolve, 50))

    expect(api.getDevice).not.toHaveBeenCalled()
    expect(screen.queryByText('Device not found')).toBeNull()
  })
})

describe('[UIF AC-122] hostname case preserved in h1', () => {
  it('renders "Web-01.lan" exactly, with no uppercase class', async () => {
    vi.mocked(api.getDevice).mockResolvedValue(deviceResponse({ hostname: 'Web-01.lan' }) as never)
    renderPage()
    const h1 = await screen.findByRole('heading', { level: 1 })
    expect(h1.textContent).toBe('Web-01.lan')
    expect(h1.className).not.toContain('uppercase')
  })
})
