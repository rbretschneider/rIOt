import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import HeatmapGrid from './HeatmapGrid'
import type { Device, Event, FleetHeartbeat } from '../../types/models'

vi.mock('../../components/Sparkline', () => ({
  default: ({ data }: { data: number[] }) => (
    <svg data-testid="sparkline" data-points={data.join(',')} />
  ),
}))

function device(
  id: string,
  hostname: string,
  status: Device['status'] = 'online',
  containerCount = 0,
): Device {
  return {
    id,
    hostname,
    short_id: id.slice(0, 6),
    arch: 'amd64',
    status,
    location: '',
    tags: [],
    docker_available: containerCount > 0,
    docker_container_count: containerCount,
    docker_auto_update_container_count: 0,
    auto_patch: false,
    has_auto_update: false,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  }
}

function hbEntry(deviceId: string, cpu: number, mem: number, disk: number, load = 0): FleetHeartbeat {
  return {
    device_id: deviceId,
    timestamp: new Date().toISOString(),
    data: {
      uptime: 0,
      cpu_percent: cpu,
      mem_percent: mem,
      disk_root_percent: disk,
      load_avg_1m: load,
    },
  }
}

function unackedWarning(deviceId: string, id: number): Event {
  return {
    id,
    device_id: deviceId,
    type: 'alert_high',
    severity: 'warning',
    message: 'high cpu',
    created_at: new Date().toISOString(),
  }
}

function renderGrid(props: Parameters<typeof HeatmapGrid>[0]) {
  return render(
    <MemoryRouter>
      <HeatmapGrid {...props} />
    </MemoryRouter>,
  )
}

describe('[AC-018] HeatmapGrid — empty state when no devices', () => {
  it('renders empty state when device list is empty', () => {
    renderGrid({ devices: [], deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByTestId('heatmap-empty-state')).toBeInTheDocument()
  })

  it('does not render heatmap-grid when empty', () => {
    renderGrid({ devices: [], deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.queryByTestId('heatmap-grid')).not.toBeInTheDocument()
  })

  it('empty state contains descriptive text', () => {
    renderGrid({ devices: [], deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByText(/no devices registered/i)).toBeInTheDocument()
  })
})

describe('[AC-013] HeatmapGrid — renders device cards with required pieces (FR-031)', () => {
  it('renders a card for each device', () => {
    const devices = [device('d1', 'node-1'), device('d2', 'node-2')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByTestId('device-card-d1')).toBeInTheDocument()
    expect(screen.getByTestId('device-card-d2')).toBeInTheDocument()
  })

  it('renders status dot for each device', () => {
    const devices = [device('d1', 'node-1')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByTestId('status-dot-d1')).toBeInTheDocument()
  })

  it('renders hostname text on each card', () => {
    const devices = [device('d1', 'myserver')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByText('myserver')).toBeInTheDocument()
  })

  it('renders container badge when containerCount > 0', () => {
    const devices = [device('d1', 'node-1', 'online', 3)]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByTestId('container-badge-d1')).toBeInTheDocument()
    expect(screen.getByTestId('container-badge-d1').textContent).toBe('3')
  })

  it('does not render container badge when containerCount is 0', () => {
    const devices = [device('d1', 'node-1', 'online', 0)]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.queryByTestId('container-badge-d1')).not.toBeInTheDocument()
  })

  it('renders mini-bars section', () => {
    const devices = [device('d1', 'node-1')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByTestId('mini-bars-d1')).toBeInTheDocument()
  })
})

describe('[AC-015] HeatmapGrid — alert hairline red border (FR-033)', () => {
  it('applies red border class to device with active unacknowledged alert', () => {
    const devices = [device('d1', 'alert-node')]
    const events = [unackedWarning('d1', 1)]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events })
    const card = screen.getByTestId('device-card-d1')
    expect(card.className).toContain('border-red-500')
  })

  it('does not apply red border when no alert events for device', () => {
    const devices = [device('d1', 'quiet-node')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    const card = screen.getByTestId('device-card-d1')
    expect(card.className).not.toContain('border-red-500')
  })

  it('does not apply red border for acknowledged events', () => {
    const devices = [device('d1', 'acked-node')]
    const ackedEvent: Event = {
      id: 99,
      device_id: 'd1',
      type: 'alert_high',
      severity: 'warning',
      message: 'old warning',
      created_at: new Date().toISOString(),
      acknowledged_at: new Date().toISOString(),
    }
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [ackedEvent] })
    const card = screen.getByTestId('device-card-d1')
    expect(card.className).not.toContain('border-red-500')
  })

  it('does not apply red border for info-severity events', () => {
    const devices = [device('d1', 'info-node')]
    const infoEvent: Event = {
      id: 5,
      device_id: 'd1',
      type: 'probe_ok',
      severity: 'info',
      message: 'all good',
      created_at: new Date().toISOString(),
    }
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [infoEvent] })
    const card = screen.getByTestId('device-card-d1')
    expect(card.className).not.toContain('border-red-500')
  })
})

describe('[AC-016] HeatmapGrid — stress-score sort order (FR-034)', () => {
  it('sorts devices by stress score descending (highest CPU first)', () => {
    const devices = [device('low', 'low-stress'), device('high', 'high-stress')]
    const heartbeats: Record<string, FleetHeartbeat[]> = {
      low: [hbEntry('low', 10, 20, 15)],
      high: [hbEntry('high', 90, 80, 70)],
    }
    renderGrid({ devices, deviceHeartbeats: heartbeats, pulseMap: {}, events: [] })
    const cards = screen.getAllByRole('link')
    // high-stress device should appear before low-stress in DOM order
    const highIdx = cards.findIndex(c => c.getAttribute('data-testid') === 'device-card-high')
    const lowIdx = cards.findIndex(c => c.getAttribute('data-testid') === 'device-card-low')
    expect(highIdx).toBeLessThan(lowIdx)
  })

  it('forces offline devices to the end regardless of last-known values', () => {
    const devices = [
      device('offline-high', 'offline-high', 'offline'),
      device('online-low', 'online-low', 'online'),
    ]
    const heartbeats: Record<string, FleetHeartbeat[]> = {
      'offline-high': [hbEntry('offline-high', 99, 99, 99)],
      'online-low': [hbEntry('online-low', 1, 1, 1)],
    }
    renderGrid({ devices, deviceHeartbeats: heartbeats, pulseMap: {}, events: [] })
    const cards = screen.getAllByRole('link')
    const onlineIdx = cards.findIndex(c => c.getAttribute('data-testid') === 'device-card-online-low')
    const offlineIdx = cards.findIndex(c => c.getAttribute('data-testid') === 'device-card-offline-high')
    expect(onlineIdx).toBeLessThan(offlineIdx)
  })

  it('all offline devices sort after all online devices', () => {
    const devices = [
      device('off1', 'offline-1', 'offline'),
      device('off2', 'offline-2', 'offline'),
      device('on1', 'online-1', 'online'),
    ]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    const cards = screen.getAllByRole('link')
    const onlineIdx = cards.findIndex(c => c.getAttribute('data-testid') === 'device-card-on1')
    const off1Idx = cards.findIndex(c => c.getAttribute('data-testid') === 'device-card-off1')
    const off2Idx = cards.findIndex(c => c.getAttribute('data-testid') === 'device-card-off2')
    expect(onlineIdx).toBeLessThan(off1Idx)
    expect(onlineIdx).toBeLessThan(off2Idx)
  })
})

describe('[AC-017] HeatmapGrid — device card navigates to /devices/:id (FR-035)', () => {
  it('DeviceCard wraps content in a link to /devices/:id', () => {
    const devices = [device('abc-uuid', 'mynode')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    const card = screen.getByTestId('device-card-abc-uuid')
    expect(card.tagName).toBe('A')
    expect(card.getAttribute('href')).toBe('/devices/abc-uuid')
  })
})

describe('[AC-014] HeatmapGrid — status dot pulse keyed on lastPulseAt (FR-032)', () => {
  it('online device has emerald status dot', () => {
    const devices = [device('d1', 'node-1', 'online')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    const dot = screen.getByTestId('status-dot-d1')
    expect(dot.className).toContain('bg-emerald-400')
  })

  it('offline device has gray status dot', () => {
    const devices = [device('d1', 'node-1', 'offline')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    const dot = screen.getByTestId('status-dot-d1')
    expect(dot.className).toContain('bg-gray-600')
  })

  it('status dot aria-label says Online when online', () => {
    const devices = [device('d1', 'node-1', 'online')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByTestId('status-dot-d1').getAttribute('aria-label')).toBe('Online')
  })

  it('status dot aria-label says Offline when offline', () => {
    const devices = [device('d1', 'node-1', 'offline')]
    renderGrid({ devices, deviceHeartbeats: {}, pulseMap: {}, events: [] })
    expect(screen.getByTestId('status-dot-d1').getAttribute('aria-label')).toBe('Offline')
  })
})
