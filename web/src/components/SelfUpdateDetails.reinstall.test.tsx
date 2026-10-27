import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import SelfUpdateDetails from './SelfUpdateDetails'
import ActivityLog from './ActivityLog'
import CommandProgress from './docker/CommandProgress'
import { api } from '../api/client'
import { renderWithProviders } from '../test/renderWithProviders'
import type { Command, CommandEventsResponse, Event } from '../types/models'

// EXPECT-008 (FR-026, DEC-6): the server's record of an installer re-run opens in the
// same read-only view as an agent self-update, with its own wording and no controls.
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

const COMMAND_ID = '9f8e7d6c-1111-2222-3333-444444444444'
const timeZone = { now: () => Date.parse('2026-10-27T13:30:00Z') }

function record(over: Partial<Command> = {}): Command {
  return {
    id: COMMAND_ID,
    device_id: 'dev-1',
    action: 'agent_update',
    origin: 'installer',
    params: {},
    status: 'succeeded',
    step: 'confirmed',
    result_msg: 'confirmed: the agent registered again',
    duration_ms: null,
    exit_code: null,
    created_at: '2026-10-27T13:15:00Z',
    updated_at: '2026-10-27T13:16:10Z',
    started_at: '2026-10-27T13:15:00Z',
    ...over,
  }
}

function event(id: number, over: Partial<Event> = {}): Event {
  return {
    id,
    device_id: 'dev-1',
    type: 'device_offline',
    severity: 'info',
    message: `message ${id}`,
    created_at: '2026-10-27T13:15:50Z',
    command_id: COMMAND_ID,
    command_attr: 'expected',
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

beforeEach(() => {
  vi.clearAllMocks()
  serveEvents()
})

describe('EXPECT-008 [AC-022] record view shows label, actor, start, outcome, events and no controls', () => {
  const offline = event(11, {
    message: 'Device PiVPN went offline during Agent reinstall by installer (expected, back by 13:25)',
  })
  const online = event(12, {
    type: 'device_online',
    message: 'Device PiVPN came online after Agent reinstall by installer (offline 20s)',
  })

  it('names the action "Agent reinstall" and the actor "by installer", with no versions', async () => {
    renderWithProviders(<SelfUpdateDetails deviceId="dev-1" command={record()} />, { timeZone })

    expect(await screen.findByText('Agent reinstall')).toBeInTheDocument()
    expect(screen.getByText('by installer')).toBeInTheDocument()
    expect(screen.queryByText(/→/), 'a reinstall has no from/to versions').toBeNull()
    expect(screen.queryByText(/auto-update/), 'a reinstall is not a self-update').toBeNull()
  })

  it('shows the start time and the server-rendered result message', async () => {
    renderWithProviders(<SelfUpdateDetails deviceId="dev-1" command={record()} />, { timeZone })

    expect(await screen.findByText('2026-10-27 13:15:00 UTC'), 'the start time must be shown').toBeInTheDocument()
    expect(screen.getByText('confirmed: the agent registered again')).toBeInTheDocument()
  })

  it.each([
    ['running', 'In progress'],
    ['succeeded', 'Succeeded'],
    ['timed_out', 'Not confirmed'],
  ])('words the outcome %s as "%s"', async (status, text) => {
    renderWithProviders(
      <SelfUpdateDetails deviceId="dev-1" command={record({ status: status as Command['status'], result_msg: '' })} />,
      { timeZone },
    )

    expect((await screen.findByTestId('self-update-outcome')).textContent).toBe(text)
  })

  it('lists the linked server events in full', async () => {
    serveEvents({ server_events: [offline, online] })
    renderWithProviders(<SelfUpdateDetails deviceId="dev-1" command={record()} />, { timeZone })

    const list = await screen.findByTestId('self-update-server-events')
    expect(within(list).getAllByRole('listitem')).toHaveLength(2)
    expect(within(list).getByText(/went offline during Agent reinstall by installer \(expected, back by 13:25\)/)).toBeInTheDocument()
    expect(within(list).getByText(/came online after Agent reinstall by installer \(offline 20s\)/)).toBeInTheDocument()
    expect(api.getCommandEvents).toHaveBeenCalledWith('dev-1', COMMAND_ID)
  })

  it('labels the not-confirmed warning event with its own type wording', async () => {
    serveEvents({
      server_events: [
        event(13, {
          type: 'agent_reinstall_unconfirmed',
          severity: 'warning',
          message: 'Agent reinstall by installer on PiCUPS was not confirmed within 10m (agent did not restart)',
          command_attr: 'outcome',
        }),
      ],
    })
    renderWithProviders(
      <SelfUpdateDetails deviceId="dev-1" command={record({ status: 'timed_out', result_msg: '' })} />,
      { timeZone },
    )

    const list = await screen.findByTestId('self-update-server-events')
    expect(within(list).getByText('Agent reinstall not confirmed')).toBeInTheDocument()
    expect(within(list).getByText(/agent did not restart/)).toBeInTheDocument()
  })

  it('offers no action: no button, no link, no cancel or retry', async () => {
    serveEvents({ server_events: [offline] })
    renderWithProviders(<SelfUpdateDetails deviceId="dev-1" command={record({ status: 'running' })} />, { timeZone })

    await screen.findByTestId('self-update-server-events')
    expect(screen.queryAllByRole('button'), 'a reinstall record must have no controls').toHaveLength(0)
    expect(screen.queryAllByRole('link')).toHaveLength(0)
    expect(screen.queryByText(/cancel/i)).toBeNull()
    expect(screen.queryByText(/retry/i)).toBeNull()
  })

  it('renders the result message and event text as text, never as markup', async () => {
    serveEvents({ server_events: [event(1, { message: '<img src=x onerror=alert(1)>' })] })
    renderWithProviders(
      <SelfUpdateDetails deviceId="dev-1" command={record({ result_msg: '<b>bold</b>' })} />,
      { timeZone },
    )

    expect(await screen.findByText('<b>bold</b>')).toBeInTheDocument()
    expect(await screen.findByText('<img src=x onerror=alert(1)>')).toBeInTheDocument()
    expect(document.querySelector('img')).toBeNull()
    expect(document.querySelector('b')).toBeNull()
  })

  it('keeps scrollbar-thin on the scrolling event list', async () => {
    serveEvents({ server_events: [offline] })
    renderWithProviders(<SelfUpdateDetails deviceId="dev-1" command={record()} />, { timeZone })

    expect((await screen.findByTestId('self-update-server-events')).className).toContain('scrollbar-thin')
  })
})

describe('EXPECT-008 [AC-022] the command history shows the reinstall read-only', () => {
  const commands: Command[] = [
    record(),
    record({ id: 'cmd-su', origin: 'agent_self_update', params: { from_version: '2.78.0', to_version: '2.80.0' }, result_msg: 'self' }),
    record({ id: 'cmd-ryan', origin: 'user', action: 'reboot', status: 'succeeded', result_msg: 'reboot initiated' }),
  ]

  beforeEach(() => {
    vi.mocked(api.getDeviceCommands).mockResolvedValue(commands)
    vi.mocked(api.getCommandOutput).mockResolvedValue([])
  })

  it('shows the action label, actor phrase and outcome in the row', async () => {
    renderWithProviders(<ActivityLog deviceId="dev-1" />, { timeZone })

    const row = (await screen.findByText('Agent reinstall')).closest('tr') as HTMLElement
    expect(within(row).getByText('by installer')).toBeInTheDocument()
    expect(within(row).getByText('Succeeded')).toBeInTheDocument()
  })

  it('leaves the self-update row and the ordinary command row as they were', async () => {
    renderWithProviders(<ActivityLog deviceId="dev-1" />, { timeZone })

    const selfRow = (await screen.findByText('Agent update 2.78.0 → 2.80.0')).closest('tr') as HTMLElement
    expect(within(selfRow).getByText('by agent auto-update')).toBeInTheDocument()
    expect(within(selfRow).queryByText('by installer')).toBeNull()
    const userRow = screen.getByText('reboot initiated').closest('tr') as HTMLElement
    expect(within(userRow).getByText('Reboot')).toBeInTheDocument()
  })

  it('opens the record in a read-only modal with its events and no output fetch', async () => {
    serveEvents({ server_events: [event(11, { message: 'went offline' })] })
    renderWithProviders(<ActivityLog deviceId="dev-1" />, { timeZone })

    await userEvent.click(await screen.findByText('Agent reinstall'))

    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('went offline')).toBeInTheDocument()
    expect(within(dialog).getByTestId('self-update-outcome').textContent).toBe('Succeeded')
    expect(api.getCommandOutput, 'a reinstall record has no output to fetch').not.toHaveBeenCalled()
    expect(within(dialog).queryByText(/cancel/i)).toBeNull()
    expect(within(dialog).queryByText(/retry/i)).toBeNull()
    expect(within(dialog).queryByText('Exit Code')).toBeNull()
  })
})

describe('EXPECT-008 [AC-022] the activity label link opens the record, never a command', () => {
  it('routes a reinstall record to the read-only view with no cancel control and no steps link', async () => {
    vi.mocked(api.getCommand).mockResolvedValue(record({ status: 'running', step: 'waiting_for_restart', result_msg: '' }))
    renderWithProviders(<CommandProgress deviceId="dev-1" commandId={COMMAND_ID} />, { timeZone })

    expect(await screen.findByTestId('self-update-details')).toBeInTheDocument()
    expect(screen.getByText('Agent reinstall')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /cancel/i })).toBeNull()
    expect(screen.queryByRole('link', { name: 'View steps and output' })).toBeNull()
    expect(api.cancelCommand).not.toHaveBeenCalled()
  })
})
