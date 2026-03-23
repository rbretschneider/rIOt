import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import Security from './Security'

// --- mock api/client ---

const mockGetSecurityOverview = vi.fn()
const mockGetSecurityDevices = vi.fn()
const mockGetSecurityScore = vi.fn()

vi.mock('../api/client', () => ({
  api: {
    getSecurityOverview: (...args: unknown[]) => mockGetSecurityOverview(...args),
    getSecurityDevices: (...args: unknown[]) => mockGetSecurityDevices(...args),
    getSecurityScore: (...args: unknown[]) => mockGetSecurityScore(...args),
  },
}))

// Default overview with no cert data (AC-009 base state)
const baseOverview = {
  total_devices: 2,
  devices_reporting: 2,
  total_failed_logins: 0,
  total_logged_in: 1,
  firewall_active: 2,
  firewall_inactive: 0,
  selinux_enforcing: 0,
  apparmor_enabled: 0,
  certs_expiring_soon: 0,
  total_certs: 0,
}

const device1 = {
  device_id: 'dev-1',
  hostname: 'alpha',
  status: 'online',
  selinux: '',
  apparmor: 'enabled',
  firewall_status: 'active',
  failed_logins_24h: 3,
  logged_in_users: 1,
  open_ports: [22, 80],
  pending_security_count: 2,
  unattended_upgrades: false,
  certs_expiring_soon: 0,
}

const device2 = {
  device_id: 'dev-2',
  hostname: 'beta',
  status: 'online',
  selinux: 'enforcing',
  apparmor: '',
  firewall_status: 'inactive',
  failed_logins_24h: 0,
  logged_in_users: 0,
  open_ports: [],
  pending_security_count: 0,
  unattended_upgrades: true,
  certs_expiring_soon: 1,
}

const deviceNoUpdateData = {
  device_id: 'dev-3',
  hostname: 'gamma',
  status: 'online',
  selinux: '',
  apparmor: '',
  firewall_status: 'active',
  failed_logins_24h: 0,
  logged_in_users: 0,
  open_ports: [],
  pending_security_count: 0,
  unattended_upgrades: null,
  certs_expiring_soon: 0,
}

const scoreA: import('../types/models').SecurityScoreResult = {
  overall_score: 92,
  max_score: 100,
  grade: 'A',
  categories: [],
  evaluated_at: new Date().toISOString(),
}

const scoreF: import('../types/models').SecurityScoreResult = {
  overall_score: 25,
  max_score: 100,
  grade: 'F',
  categories: [],
  evaluated_at: new Date().toISOString(),
}

function renderWithProviders() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <Security />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

beforeEach(() => {
  vi.clearAllMocks()
  mockGetSecurityOverview.mockResolvedValue(baseOverview)
  mockGetSecurityDevices.mockResolvedValue([device1, device2])
  mockGetSecurityScore.mockResolvedValue(scoreA)
})

// --- AC-001: Score column removed from Fleet Overview ---
// AC-001 is covered by the FleetOverview.test.tsx assertions below.

describe('[AC-002] Score column displayed on Security page', () => {
  it('renders a Score column header as the first data column after Device', async () => {
    renderWithProviders()
    const headers = await screen.findAllByRole('columnheader')
    const headerTexts = headers.map(h => h.textContent?.trim() ?? '')
    const deviceIdx = headerTexts.findIndex(t => t === 'Device')
    const scoreIdx = headerTexts.findIndex(t => t.startsWith('Score'))
    expect(scoreIdx).toBeGreaterThan(-1)
    expect(scoreIdx).toBe(deviceIdx + 1)
  })

  it('displays scores with emerald colour for grade-A devices', async () => {
    mockGetSecurityScore.mockResolvedValue(scoreA)
    renderWithProviders()
    await waitFor(() => {
      // Score value 92 should be rendered
      expect(screen.getAllByText('92').length).toBeGreaterThan(0)
    })
    const scoreEl = screen.getAllByText('92')[0]
    expect(scoreEl.className).toMatch(/text-emerald-400/)
  })

  it('displays scores with red colour for grade-F devices', async () => {
    mockGetSecurityScore.mockResolvedValue(scoreF)
    renderWithProviders()
    await waitFor(() => {
      expect(screen.getAllByText('25').length).toBeGreaterThan(0)
    })
    const scoreEl = screen.getAllByText('25')[0]
    expect(scoreEl.className).toMatch(/text-red-400/)
  })
})

describe('[AC-003] Score modal opens from Security page', () => {
  it('opens SecurityScoreModal when clicking a device score', async () => {
    mockGetSecurityScore.mockResolvedValue(scoreA)
    renderWithProviders()
    // Wait for score to render
    await waitFor(() => {
      expect(screen.getAllByText('92').length).toBeGreaterThan(0)
    })
    // Click the first score button
    const scoreButtons = screen.getAllByTitle(/Security: 92\/100/)
    fireEvent.click(scoreButtons[0])
    // Modal should be visible — it shows "Security Score" text
    expect(await screen.findByText('Security Score')).toBeInTheDocument()
  })
})

describe('[AC-004] No-score device handling', () => {
  it('renders "-" in muted styling when device has no score data', async () => {
    // First device returns no score (rejected/error)
    mockGetSecurityScore.mockRejectedValue(new Error('no telemetry'))
    mockGetSecurityDevices.mockResolvedValue([device1])
    renderWithProviders()
    // After loading, score cell should show "-"
    await waitFor(() => {
      const dashes = screen.getAllByText('-')
      // At least one "-" in the score column
      expect(dashes.length).toBeGreaterThan(0)
    })
    // The dash must have muted styling
    const dashes = screen.getAllByText('-')
    const hasGrayDash = dashes.some(el => el.className.includes('text-gray-600'))
    expect(hasGrayDash).toBe(true)
  })
})

describe('[AC-005] Fleet average score card', () => {
  it('displays Fleet Score card with arithmetic mean of all resolved scores', async () => {
    // dev-1 and dev-2 both return scoreA (92)
    mockGetSecurityScore.mockResolvedValue(scoreA)
    renderWithProviders()
    await waitFor(() => {
      expect(screen.getByText('Fleet Score')).toBeInTheDocument()
    })
    // Average of 92 + 92 = 92
    const fleetScoreArea = screen.getByText('Fleet Score').closest('div')
    expect(fleetScoreArea?.textContent).toContain('92')
  })

  it('applies emerald colour to Fleet Score when average is grade A (>=90)', async () => {
    mockGetSecurityScore.mockResolvedValue(scoreA)
    renderWithProviders()
    await waitFor(() => {
      expect(screen.getByText('Fleet Score')).toBeInTheDocument()
    })
    // Grade A -> emerald
    const scoreEl = screen.getAllByText('92').find(el => el.closest('[class*="border-b"]'))
    expect(scoreEl?.className).toMatch(/text-emerald-400/)
  })
})

describe('[AC-006] Security updates column', () => {
  it('renders a "Sec. Updates" column header', async () => {
    renderWithProviders()
    expect(await screen.findByText(/Sec\. Updates/i)).toBeInTheDocument()
  })

  it('displays pending security count in red when > 0', async () => {
    mockGetSecurityDevices.mockResolvedValue([device1]) // pending_security_count: 2
    renderWithProviders()
    await waitFor(() => {
      const count = screen.getByText('2')
      expect(count.className).toMatch(/text-red-400/)
    })
  })

  it('displays "0" in muted styling when pending_security_count is 0', async () => {
    mockGetSecurityDevices.mockResolvedValue([device2]) // pending_security_count: 0
    renderWithProviders()
    await waitFor(() => {
      const zeros = screen.getAllByText('0')
      const mutedZero = zeros.find(el => el.className.includes('text-gray-500'))
      expect(mutedZero).toBeDefined()
    })
  })

  it('displays "-" in muted styling when no update telemetry (unattended_upgrades is null)', async () => {
    mockGetSecurityDevices.mockResolvedValue([deviceNoUpdateData])
    renderWithProviders()
    await waitFor(() => {
      const dashes = screen.getAllByText('-')
      const hasGrayDash = dashes.some(el => el.className.includes('text-gray-600'))
      expect(hasGrayDash).toBe(true)
    })
  })
})

describe('[AC-007] Auto-Updates column', () => {
  it('renders an "Auto-Updates" column header', async () => {
    renderWithProviders()
    expect(await screen.findByText('Auto-Updates')).toBeInTheDocument()
  })

  it('displays "Enabled" in green when unattended_upgrades is true', async () => {
    mockGetSecurityDevices.mockResolvedValue([device2]) // unattended_upgrades: true
    renderWithProviders()
    await waitFor(() => {
      const el = screen.getByText('Enabled')
      expect(el.className).toMatch(/text-emerald-400/)
    })
  })

  it('displays "Disabled" in amber when unattended_upgrades is false', async () => {
    mockGetSecurityDevices.mockResolvedValue([device1]) // unattended_upgrades: false
    renderWithProviders()
    await waitFor(() => {
      const el = screen.getByText('Disabled')
      expect(el.className).toMatch(/text-amber-400/)
    })
  })

  it('displays "-" when unattended_upgrades is null (no update telemetry)', async () => {
    mockGetSecurityDevices.mockResolvedValue([deviceNoUpdateData])
    renderWithProviders()
    await waitFor(() => {
      const dashes = screen.getAllByText('-')
      const hasGrayDash = dashes.some(el => el.className.includes('text-gray-600'))
      expect(hasGrayDash).toBe(true)
    })
  })
})

describe('[AC-008] Certificate expiry summary card', () => {
  it('displays Certs Expiring card when total_certs > 0 and certs are expiring', async () => {
    mockGetSecurityOverview.mockResolvedValue({
      ...baseOverview,
      certs_expiring_soon: 3,
      total_certs: 5,
    })
    renderWithProviders()
    const certLabel = await screen.findByText('Certs Expiring')
    expect(certLabel).toBeInTheDocument()
    // The count is rendered in the same card
    const card = certLabel.closest('div')
    expect(card?.textContent).toContain('3')
  })

  it('applies amber colour to Certs Expiring card when certs are expiring', async () => {
    mockGetSecurityOverview.mockResolvedValue({
      ...baseOverview,
      certs_expiring_soon: 2,
      total_certs: 4,
    })
    renderWithProviders()
    await waitFor(() => {
      expect(screen.getByText('Certs Expiring')).toBeInTheDocument()
    })
    const card = screen.getByText('Certs Expiring').closest('div')
    const countEl = card?.querySelector('p:last-child')
    expect(countEl?.className).toMatch(/text-amber-400/)
  })
})

describe('[AC-009] Certificate card hidden when no cert data', () => {
  it('does not render Certs Expiring card when total_certs is 0', async () => {
    mockGetSecurityOverview.mockResolvedValue({
      ...baseOverview,
      certs_expiring_soon: 0,
      total_certs: 0,
    })
    renderWithProviders()
    // Wait for the page to render
    await screen.findByText('Reporting')
    expect(screen.queryByText('Certs Expiring')).not.toBeInTheDocument()
  })
})

describe('[AC-010] Table sorting', () => {
  it('defaults to Score ascending sort (lowest scores first, devices with scores appear sorted)', async () => {
    mockGetSecurityDevices.mockResolvedValue([device1, device2])
    renderWithProviders()
    // Verify Score column header has ascending indicator by default
    await waitFor(() => {
      const headers = screen.getAllByRole('columnheader')
      const scoreHeader = headers.find(h => h.textContent?.includes('Score') && h.textContent?.includes('↑'))
      expect(scoreHeader).toBeDefined()
    })
  })

  it('clicking Score column header toggles sort direction', async () => {
    renderWithProviders()
    await waitFor(() => {
      const headers = screen.getAllByRole('columnheader')
      const scoreHeader = headers.find(h => h.textContent?.includes('Score') && h.textContent?.includes('↑'))
      expect(scoreHeader).toBeDefined()
    })
    // Click the Score header to toggle to descending
    const headers = screen.getAllByRole('columnheader')
    const scoreHeader = headers.find(h => h.textContent?.includes('Score'))!
    fireEvent.click(scoreHeader)
    await waitFor(() => {
      const updatedHeaders = screen.getAllByRole('columnheader')
      const descHeader = updatedHeaders.find(h => h.textContent?.includes('Score') && h.textContent?.includes('↓'))
      expect(descHeader).toBeDefined()
    })
  })

  it('clicking Failed Logins header activates sort on that column', async () => {
    renderWithProviders()
    await screen.findByText('Device') // wait for render
    // Use role=columnheader to avoid matching data cells
    const headers = screen.getAllByRole('columnheader')
    const failedHeader = headers.find(h => h.textContent?.includes('Failed Logins'))!
    fireEvent.click(failedHeader)
    await waitFor(() => {
      const updatedHeaders = screen.getAllByRole('columnheader')
      const sortedHeader = updatedHeaders.find(h => h.textContent?.includes('Failed Logins') && h.textContent?.includes('↑'))
      expect(sortedHeader).toBeDefined()
    })
  })

  it('clicking Sec. Updates header activates sort on that column', async () => {
    renderWithProviders()
    await screen.findByText('Device')
    const headers = screen.getAllByRole('columnheader')
    const updHeader = headers.find(h => h.textContent?.includes('Sec. Updates'))!
    fireEvent.click(updHeader)
    await waitFor(() => {
      const updatedHeaders = screen.getAllByRole('columnheader')
      const sortedHeader = updatedHeaders.find(h => h.textContent?.includes('Sec. Updates') && h.textContent?.includes('↑'))
      expect(sortedHeader).toBeDefined()
    })
  })

  // Added by QA Engineer
  // Covers AC-010 / FR-021: default sort must place worst (lowest) scores at the top.
  // The existing tests only verify the arrow indicator; this verifies actual row order.
  it('default Score ascending sort places lower-scored device above higher-scored device', async () => {
    // dev-1 gets grade F (score 25), dev-2 gets grade A (score 92)
    // With ascending sort (worst first), alpha (25) must appear before beta (92).
    mockGetSecurityDevices.mockResolvedValue([device2, device1]) // reversed input order
    mockGetSecurityScore.mockImplementation((id: string) => {
      if (id === 'dev-1') return Promise.resolve(scoreF)
      if (id === 'dev-2') return Promise.resolve(scoreA)
      return Promise.reject(new Error('unknown'))
    })
    renderWithProviders()
    // Wait for both scores to resolve
    await waitFor(() => {
      expect(screen.queryByText('25')).toBeInTheDocument()
      expect(screen.queryByText('92')).toBeInTheDocument()
    })
    const rows = screen.getAllByRole('row')
    // rows[0] = thead, rows[1] = first data row, rows[2] = second data row
    const firstRowText = rows[1].textContent ?? ''
    const secondRowText = rows[2].textContent ?? ''
    // alpha (dev-1) has score 25 (F) — must be first
    expect(firstRowText).toContain('alpha')
    expect(secondRowText).toContain('beta')
  })
})
