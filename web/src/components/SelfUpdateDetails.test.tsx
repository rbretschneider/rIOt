import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import SelfUpdateDetails from './SelfUpdateDetails'
import ActivityLog from './ActivityLog'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { Command, CommandEventsResponse, Event } from '../types/models'

vi.mock('../api/client', () => ({
  api: {
    getCommandEvents: vi.fn(),
    getDeviceCommands: vi.fn(),
    getCommandOutput: vi.fn(),
    getCommand: vi.fn(),
    cancelCommand: vi.fn(),
  },
}))

vi.mock('../hooks/useWebSocket', () => ({
  useWebSocket: () => ({ connected: false }),
}))

const COMMAND_ID = '1a2b3c4d-1111-2222-3333-444444444444'
const timeZone = { now: () => Date.parse('2026-10-26T18:00:00Z') }

function command(over: Partial<Command> = {}): Command {
  return {
    id: COMMAND_ID,
    device_id: 'dev-1',
    action: 'agent_update',
    origin: 'agent_self_update',
    params: { from_version: '2.78.0', to_version: '2.80.0' },
    status: 'timed_out',
    result_msg: 'Agent update 2.78.0 → 2.80.0 by agent auto-update on hawaii was not confirmed within 10m (agent reports 2.78.0)',
    duration_ms: null,
    exit_code: null,
    created_at: '2026-10-26T17:51:00Z',
    updated_at: '2026-10-26T18:01:00Z',
    started_at: '2026-10-26T17:51:00Z',
    ...over,
  }
}

function event(id: number, over: Partial<Event> = {}): Event {
  return {
    id,
    device_id: 'dev-1',
    type: 'agent_update_started',
    severity: 'info',
    message: `message ${id}`,
    created_at: '2026-10-26T17:51:00Z',
    command_id: COMMAND_ID,
    command_attr: 'inferred',
    ...over,
  }
}

function serveEvents(body: Partial<CommandEventsResponse> = {}) {
  vi.mocked(api.getCommandEvents).mockResolvedValue({
    server_events: [],
    server_events_omitted: 0,
    agent_events: [],
    agent_events_omitted: 0,
    ...body,
  })
}

function renderDetails(cmd: Command = command()) {
  return renderWithProviders(<SelfUpdateDetails deviceId="dev-1" command={cmd} />, { timeZone })
}

beforeEach(() => {
  vi.clearAllMocks()
  serveEvents()
})

describe('EXPECT-002 [AC-023] details show versions, outcome, server events, the omitted count, and no controls', () => {
  const serverEvents = [
    event(11, { type: 'device_offline', message: 'Device hawaii went offline during Agent update 2.78.0 → 2.80.0 by agent auto-update (expected, back by 18:01)', command_attr: 'expected' }),
    event(12, { type: 'device_online', message: 'Device hawaii came online after Agent update 2.78.0 → 2.80.0 by agent auto-update (offline 42s)', command_attr: 'expected' }),
    event(13, { type: 'agent_self_update_unconfirmed', severity: 'warning', message: 'not confirmed within 10m', command_attr: 'outcome' }),
  ]
  const agentEvents = Array.from({ length: 50 }, (_, i) => event(100 + i, { message: `agent flood ${i}` }))

  it('names both versions and the actor phrase', async () => {
    serveEvents({ server_events: serverEvents })
    renderDetails()

    expect(await screen.findByText('Agent update 2.78.0 → 2.80.0')).toBeInTheDocument()
    expect(screen.getByText('by agent auto-update')).toBeInTheDocument()
  })

  it('shows the start time and the server-rendered outcome message', async () => {
    renderDetails()

    expect(await screen.findByText(/was not confirmed within 10m \(agent reports 2\.78\.0\)/)).toBeInTheDocument()
    expect(screen.getByText('2026-10-26 17:51:00 UTC'), 'the start time must be shown').toBeInTheDocument()
  })

  it('shows every server-authored event in full, even when the agent flooded the record', async () => {
    serveEvents({ server_events: serverEvents, agent_events: agentEvents, agent_events_omitted: 150 })
    renderDetails()

    const list = await screen.findByTestId('self-update-server-events')
    expect(within(list).getAllByRole('listitem'), 'all three server events must be listed').toHaveLength(3)
    expect(within(list).getByText(/went offline during Agent update 2\.78\.0 → 2\.80\.0 by agent auto-update/)).toBeInTheDocument()
    expect(within(list).getByText(/came online after Agent update/)).toBeInTheDocument()
    expect(within(list).getByText('not confirmed within 10m')).toBeInTheDocument()
    expect(within(list).getByText('Agent self-update not confirmed')).toBeInTheDocument()
  })

  it('shows the agent events returned and how many more were not shown', async () => {
    serveEvents({ server_events: serverEvents, agent_events: agentEvents, agent_events_omitted: 150 })
    renderDetails()

    const list = await screen.findByTestId('self-update-agent-events')
    expect(within(list).getAllByRole('listitem')).toHaveLength(50)
    expect(screen.getByText('150 more agent events not shown')).toBeInTheDocument()
  })

  it('states no omitted count when nothing was omitted', async () => {
    serveEvents({ server_events: serverEvents, agent_events: agentEvents.slice(0, 2) })
    renderDetails()

    await screen.findByTestId('self-update-agent-events')
    expect(screen.queryByText(/more agent events not shown/)).toBeNull()
    expect(screen.queryByText(/more server events not shown/)).toBeNull()
  })

  it('says so when server events were omitted too', async () => {
    serveEvents({ server_events: serverEvents, server_events_omitted: 7 })
    renderDetails()

    expect(await screen.findByText('7 more server events not shown')).toBeInTheDocument()
  })

  it('offers no action: no button, no link, no cancel or retry', async () => {
    serveEvents({ server_events: serverEvents, agent_events: agentEvents.slice(0, 3) })
    renderDetails(command({ status: 'running' }))

    await screen.findByTestId('self-update-server-events')
    expect(screen.queryAllByRole('button'), 'a self-update record must have no controls').toHaveLength(0)
    expect(screen.queryAllByRole('link')).toHaveLength(0)
    expect(screen.queryByText(/cancel/i)).toBeNull()
    expect(screen.queryByText(/retry/i)).toBeNull()
  })

  it('asks the server for the events of this device and this record', async () => {
    renderDetails()

    await screen.findByTestId('self-update-details')
    expect(api.getCommandEvents).toHaveBeenCalledWith('dev-1', COMMAND_ID)
  })

  it('puts scrollbar-thin on both scrolling lists', async () => {
    serveEvents({ server_events: serverEvents, agent_events: agentEvents.slice(0, 2) })
    renderDetails()

    expect((await screen.findByTestId('self-update-server-events')).className).toContain('scrollbar-thin')
    expect(screen.getByTestId('self-update-agent-events').className).toContain('scrollbar-thin')
  })
})

describe('EXPECT-002 [AC-023] outcome wording', () => {
  it.each([
    ['running', 'In progress'],
    ['verifying', 'Returned, confirming'],
    ['succeeded', 'Succeeded'],
    ['failed', 'Failed'],
    ['timed_out', 'Not confirmed'],
  ])('shows %s as "%s"', async (status, text) => {
    renderDetails(command({ status: status as Command['status'], result_msg: '' }))

    expect((await screen.findByTestId('self-update-outcome')).textContent).toBe(text)
  })
})

describe('EXPECT-002 [AC-016] untrusted text', () => {
  it('shows invalid versions as the plain action label', async () => {
    renderDetails(command({ params: { from_version: '<img src=x onerror=alert(1)>', to_version: '2.80.0' } }))

    expect(await screen.findByText('Agent update')).toBeInTheDocument()
    expect(document.querySelector('img'), 'no markup from params may be rendered').toBeNull()
  })

  it('renders an event message as text, never as markup', async () => {
    serveEvents({ agent_events: [event(1, { message: '<img src=x onerror=alert(1)><b>bold</b>' })], server_events: [] })
    renderDetails()

    const list = await screen.findByTestId('self-update-agent-events')
    expect(within(list).getByText('<img src=x onerror=alert(1)><b>bold</b>')).toBeInTheDocument()
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelector('b')).toBeNull()
  })

  it('renders the result message as text, never as markup', async () => {
    renderDetails(command({ result_msg: '<script>alert(1)</script>' }))

    expect(await screen.findByText('<script>alert(1)</script>')).toBeInTheDocument()
    expect(document.querySelector('script[src], script:not([type])')).toBeNull()
  })
})

describe('EXPECT-002 [AC-020] the record still shows when its events cannot be loaded', () => {
  it('shows the record and an honest error for the events', async () => {
    vi.mocked(api.getCommandEvents).mockRejectedValue(new Error('HTTP 500'))
    renderDetails()

    expect(await screen.findByText('Could not load the linked events.')).toBeInTheDocument()
    expect(screen.getByText('Agent update 2.78.0 → 2.80.0')).toBeInTheDocument()
    expect(screen.getByTestId('self-update-outcome').textContent).toBe('Not confirmed')
  })

  it('treats null event lists (a Go nil slice) as empty', async () => {
    vi.mocked(api.getCommandEvents).mockResolvedValue({
      server_events: null,
      server_events_omitted: 0,
      agent_events: null,
      agent_events_omitted: 0,
    })
    renderDetails()

    expect(await screen.findAllByText('None')).toHaveLength(2)
  })
})

describe('EXPECT-002 [AC-023] the command history shows a self-update read-only', () => {
  const commands: Command[] = [
    command(),
    command({ id: 'cmd-ryan', origin: 'user', params: {}, status: 'succeeded', result_msg: 'reboot initiated', action: 'reboot' }),
  ]

  beforeEach(() => {
    vi.mocked(api.getDeviceCommands).mockResolvedValue(commands)
    vi.mocked(api.getCommandOutput).mockResolvedValue([])
  })

  it('shows the versioned label, the actor phrase and the outcome in the row', async () => {
    renderWithProviders(<ActivityLog deviceId="dev-1" />, { timeZone })

    const row = (await screen.findByText('Agent update 2.78.0 → 2.80.0')).closest('tr') as HTMLElement
    expect(within(row).getByText('by agent auto-update')).toBeInTheDocument()
    expect(within(row).getByText('Not confirmed')).toBeInTheDocument()
  })

  it('leaves an ordinary command row as it was', async () => {
    renderWithProviders(<ActivityLog deviceId="dev-1" />, { timeZone })

    const row = (await screen.findByText('reboot initiated')).closest('tr') as HTMLElement
    expect(within(row).getByText('Reboot')).toBeInTheDocument()
    expect(within(row).getByText('Succeeded')).toBeInTheDocument()
    expect(within(row).queryByText('by agent auto-update')).toBeNull()
  })

  it('opens the record in a modal that has the details and no controls beyond closing', async () => {
    serveEvents({ server_events: [event(11, { type: 'device_offline', message: 'went offline', command_attr: 'expected' })] })
    renderWithProviders(<ActivityLog deviceId="dev-1" />, { timeZone })

    await userEvent.click(await screen.findByText('Agent update 2.78.0 → 2.80.0'))

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('went offline')).toBeInTheDocument()
    expect(within(dialog).getByTestId('self-update-outcome').textContent).toBe('Not confirmed')
    expect(api.getCommandOutput, 'a self-update has no output to fetch').not.toHaveBeenCalled()
    expect(within(dialog).queryByText(/cancel/i)).toBeNull()
    expect(within(dialog).queryByText(/retry/i)).toBeNull()
    expect(within(dialog).queryByText('Exit Code')).toBeNull()
  })
})
