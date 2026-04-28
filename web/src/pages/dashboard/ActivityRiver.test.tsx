import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import ActivityRiver from './ActivityRiver'
import type { Event } from '../../types/models'

function evt(id: number, type: string, severity: Event['severity'] = 'info', acknowledged?: boolean): Event {
  return {
    id,
    device_id: 'dev-1',
    type,
    severity,
    message: `message-${id}`,
    created_at: new Date(2026, 3, 27, 12, 0, id).toISOString(),
    acknowledged_at: acknowledged ? new Date().toISOString() : undefined,
  }
}

function makeEvents(count: number): Event[] {
  return Array.from({ length: count }, (_, i) => evt(i + 1, 'service_info'))
}

describe('[AC-023] ActivityRiver — severity-colored items (FR-050)', () => {
  it('renders info events with gray bg class', () => {
    const events = [evt(1, 'service_info', 'info')]
    render(<ActivityRiver events={events} />)
    const item = screen.getByTestId('river-event-1')
    expect(item.className).toContain('bg-gray-700')
  })

  it('renders warning events with amber bg class', () => {
    const events = [evt(2, 'alert_high', 'warning')]
    render(<ActivityRiver events={events} />)
    const item = screen.getByTestId('river-event-2')
    expect(item.className).toContain('bg-amber-900')
  })

  it('renders critical events with red bg class', () => {
    const events = [evt(3, 'alert_critical', 'critical')]
    render(<ActivityRiver events={events} />)
    const item = screen.getByTestId('river-event-3')
    expect(item.className).toContain('bg-red-900')
  })

  it('renders severity text badge for each item (non-color signal — AC-034)', () => {
    const events = [
      evt(1, 'a', 'info'),
      evt(2, 'b', 'warning'),
      evt(3, 'c', 'critical'),
    ]
    render(<ActivityRiver events={events} />)
    expect(screen.getByTestId('river-event-1').textContent).toContain('Info')
    expect(screen.getByTestId('river-event-2').textContent).toContain('Warning')
    expect(screen.getByTestId('river-event-3').textContent).toContain('Critical')
  })

  it('renders the event message text', () => {
    const events = [evt(42, 'probe_failed', 'warning')]
    render(<ActivityRiver events={events} />)
    expect(screen.getByTestId('river-event-42').textContent).toContain('message-42')
  })
})

describe('[AC-024] ActivityRiver — new-event enter animation class (FR-052)', () => {
  it('applies animate-river-enter class only to the first (newest) item', () => {
    const events = [evt(10, 'event_new'), evt(9, 'event_old')]
    render(<ActivityRiver events={events} />)
    const first = screen.getByTestId('river-event-10')
    const second = screen.getByTestId('river-event-9')
    expect(first.className).toContain('animate-river-enter')
    expect(second.className).not.toContain('animate-river-enter')
  })

  it('applies enter class to the single event when there is only one', () => {
    const events = [evt(1, 'event')]
    render(<ActivityRiver events={events} />)
    expect(screen.getByTestId('river-event-1').className).toContain('animate-river-enter')
  })
})

describe('[AC-025] ActivityRiver — cap at 20 events (FR-053)', () => {
  it('renders exactly 20 events when given 25', () => {
    const events = makeEvents(25)
    render(<ActivityRiver events={events} />)
    // events 1..20 are visible, 21..25 are hidden
    expect(screen.queryByTestId('river-event-20')).toBeInTheDocument()
    expect(screen.queryByTestId('river-event-21')).not.toBeInTheDocument()
  })

  it('renders all events when given fewer than 20', () => {
    const events = makeEvents(10)
    render(<ActivityRiver events={events} />)
    expect(screen.queryByTestId('river-event-10')).toBeInTheDocument()
  })

  it('renders exactly 20 events when given exactly 20', () => {
    const events = makeEvents(20)
    render(<ActivityRiver events={events} />)
    expect(screen.queryByTestId('river-event-20')).toBeInTheDocument()
    expect(screen.queryByTestId('river-event-21')).not.toBeInTheDocument()
  })

  it('shows empty state message when no events match after filter', () => {
    // Activate alerts chip but only info events exist
    render(<ActivityRiver events={[evt(1, 'service_info', 'info')]} />)
    fireEvent.click(screen.getByTestId('river-chip-alerts'))
    expect(screen.getByText(/no events match/i)).toBeInTheDocument()
  })

  it('shows empty state when events array is empty and no chips active', () => {
    render(<ActivityRiver events={[]} />)
    expect(screen.getByText(/no events match/i)).toBeInTheDocument()
  })
})

describe('[AC-026] ActivityRiver — chip filter multiselect with eventCategory mapping (FR-054)', () => {
  const events = [
    evt(1, 'container_start', 'info'),
    evt(2, 'agent_connected', 'info'),
    evt(3, 'auth_failure', 'info'),
    evt(4, 'service_info', 'warning'),
    evt(5, 'probe_failed', 'info'),
  ]

  it('renders all events when no chips are active', () => {
    render(<ActivityRiver events={events.slice(0, 5)} />)
    // All 5 events visible (capped at 20, we have 5)
    expect(screen.getByTestId('river-event-1')).toBeInTheDocument()
    expect(screen.getByTestId('river-event-2')).toBeInTheDocument()
    expect(screen.getByTestId('river-event-5')).toBeInTheDocument()
  })

  it('filters to docker events when docker chip is active', () => {
    render(<ActivityRiver events={events} />)
    fireEvent.click(screen.getByTestId('river-chip-docker'))
    expect(screen.getByTestId('river-event-1')).toBeInTheDocument()
    expect(screen.queryByTestId('river-event-2')).not.toBeInTheDocument()
    expect(screen.queryByTestId('river-event-5')).not.toBeInTheDocument()
  })

  it('filters to agent events when agent chip is active', () => {
    render(<ActivityRiver events={events} />)
    fireEvent.click(screen.getByTestId('river-chip-agent'))
    expect(screen.getByTestId('river-event-2')).toBeInTheDocument()
    expect(screen.queryByTestId('river-event-1')).not.toBeInTheDocument()
  })

  it('filters to security events when security chip is active', () => {
    render(<ActivityRiver events={events} />)
    fireEvent.click(screen.getByTestId('river-chip-security'))
    expect(screen.getByTestId('river-event-3')).toBeInTheDocument()
    expect(screen.queryByTestId('river-event-1')).not.toBeInTheDocument()
  })

  it('filters to warning/critical events when alerts chip is active', () => {
    render(<ActivityRiver events={events} />)
    fireEvent.click(screen.getByTestId('river-chip-alerts'))
    // event 4 has severity=warning
    expect(screen.getByTestId('river-event-4')).toBeInTheDocument()
    // event 5 has severity=info → not shown
    expect(screen.queryByTestId('river-event-5')).not.toBeInTheDocument()
  })

  it('multiselect: docker + agent chips show events from both categories', () => {
    render(<ActivityRiver events={events} />)
    fireEvent.click(screen.getByTestId('river-chip-docker'))
    fireEvent.click(screen.getByTestId('river-chip-agent'))
    expect(screen.getByTestId('river-event-1')).toBeInTheDocument()
    expect(screen.getByTestId('river-event-2')).toBeInTheDocument()
    expect(screen.queryByTestId('river-event-3')).not.toBeInTheDocument()
  })

  it('deactivating a chip restores previous visible set', () => {
    render(<ActivityRiver events={events} />)
    // Activate docker
    fireEvent.click(screen.getByTestId('river-chip-docker'))
    expect(screen.queryByTestId('river-event-2')).not.toBeInTheDocument()
    // Deactivate docker
    fireEvent.click(screen.getByTestId('river-chip-docker'))
    expect(screen.getByTestId('river-event-2')).toBeInTheDocument()
  })

  it('chip buttons have aria-pressed=true when active', () => {
    render(<ActivityRiver events={events} />)
    const chip = screen.getByTestId('river-chip-docker')
    expect(chip).toHaveAttribute('aria-pressed', 'false')
    fireEvent.click(chip)
    expect(chip).toHaveAttribute('aria-pressed', 'true')
  })

  it('chip buttons show visible filter labels (AC-034 — non-color signal)', () => {
    render(<ActivityRiver events={[]} />)
    expect(screen.getByTestId('river-chip-alerts').textContent).toBe('Alerts')
    expect(screen.getByTestId('river-chip-docker').textContent).toBe('Docker')
    expect(screen.getByTestId('river-chip-agent').textContent).toBe('Agent')
    expect(screen.getByTestId('river-chip-security').textContent).toBe('Security')
  })
})
