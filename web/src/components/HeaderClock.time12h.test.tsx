import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HOUR_CYCLE_STORAGE_KEY, TIME_ZONE_STORAGE_KEY, TimePreferenceProvider } from '../contexts/TimePreferenceProvider'
import { Timestamp } from './ui/Timestamp'
import HeaderClock from './HeaderClock'

// TIME-12H-001. Instant 2026-10-27T17:05:09Z, server zone America/New_York, Local zone America/Los_Angeles.
const NOW = Date.parse('2026-10-27T17:05:09Z')
const INSTANT = '2026-10-27T17:05:09Z'

function memoryStorage() {
  const data = new Map<string, string>()
  return {
    data,
    getItem: (key: string) => data.get(key) ?? null,
    setItem: (key: string, value: string) => {
      data.set(key, value)
    },
  }
}

function mount(options: { serverZone?: unknown; storage?: ReturnType<typeof memoryStorage> } = {}) {
  const serverZone = 'serverZone' in options ? options.serverZone : 'America/New_York'
  return render(
    <TimePreferenceProvider
      storage={options.storage ?? memoryStorage()}
      now={() => NOW}
      localTimeZone="America/Los_Angeles"
      serverZone={serverZone}
    >
      <HeaderClock />
      <Timestamp value={INSTANT} />
      <button>outside</button>
    </TimePreferenceProvider>,
  )
}

const clock = () => screen.getByRole('button', { name: /Time display/ })
const open = async () => userEvent.click(clock())
const zoneGroup = () => screen.getByRole('radiogroup', { name: 'Time zone' })
const clockGroup = () => screen.getByRole('radiogroup', { name: 'Clock' })

describe('TIME-12H-001 [AC-021] defaults to Server 12-hour', () => {
  it('shows the server-zone 12-hour time with empty storage', () => {
    mount()
    expect(clock()).toHaveTextContent('1:05:09 PM EDT')
    expect(clock().textContent).toBe('1:05:09 PM EDT')
  })
})

describe('TIME-12H-001 [AC-022] the clock\'s accessible name states zone and cycle', () => {
  it('names the default Server zone and the 12-hour cycle', () => {
    mount()
    expect(clock()).toHaveAccessibleName('Time display: Server (America/New_York), 12-hour. Change time display')
    expect(clock()).toHaveAttribute('title', 'Time display: Server (America/New_York), 12-hour. Change time display')
  })

  it('names UTC and 24-hour after the user selects them', async () => {
    mount()
    await open()
    await userEvent.click(screen.getByRole('radio', { name: 'UTC' }))
    await userEvent.click(screen.getByRole('radio', { name: '24-hour' }))
    expect(clock()).toHaveAccessibleName('Time display: UTC, 24-hour. Change time display')
  })

  it('names Local with the browser zone', async () => {
    mount()
    await open()
    await userEvent.click(screen.getByRole('radio', { name: 'Local (America/Los_Angeles)' }))
    expect(clock()).toHaveAccessibleName('Time display: Local (America/Los_Angeles), 12-hour. Change time display')
  })
})

describe('TIME-12H-001 [AC-017] the Server option is named with the zone', () => {
  it('reads Server (America/New_York) when the API zone was accepted', async () => {
    mount()
    await open()
    expect(screen.getByRole('radio', { name: 'Server (America/New_York)' })).toBeChecked()
  })

  it('reads Server (UTC) while the zone is unknown', async () => {
    mount({ serverZone: undefined })
    await open()
    expect(screen.getByRole('radio', { name: 'Server (UTC)' })).toBeChecked()
    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
  })
})

describe('TIME-12H-001 [AC-023] the panel holds the Time zone and Clock radio groups', () => {
  it('is closed until the clock is activated', () => {
    mount()
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('radiogroup')).toBeNull()
  })

  it('opens with aria-expanded and both groups with the defaults checked', async () => {
    mount()
    await open()
    expect(clock()).toHaveAttribute('aria-expanded', 'true')

    const zones = within(zoneGroup()).getAllByRole('radio')
    expect(zones.map(r => r.closest('label')?.textContent)).toEqual([
      'Server (America/New_York)',
      'Local (America/Los_Angeles)',
      'UTC',
    ])
    expect(within(zoneGroup()).getByRole('radio', { name: 'Server (America/New_York)' })).toBeChecked()
    expect(within(zoneGroup()).getByRole('radio', { name: 'Local (America/Los_Angeles)' })).not.toBeChecked()
    expect(within(zoneGroup()).getByRole('radio', { name: 'UTC' })).not.toBeChecked()

    expect(within(clockGroup()).getAllByRole('radio').map(r => r.closest('label')?.textContent)).toEqual(['12-hour', '24-hour'])
    expect(within(clockGroup()).getByRole('radio', { name: '12-hour' })).toBeChecked()
    expect(within(clockGroup()).getByRole('radio', { name: '24-hour' })).not.toBeChecked()
  })

  it('points aria-controls at the open panel', async () => {
    mount()
    await open()
    const panelId = clock().getAttribute('aria-controls')
    expect(panelId).toBeTruthy()
    expect(document.getElementById(panelId!)).toContainElement(zoneGroup())
  })

  it('moves focus to the checked Time zone radio when it opens', async () => {
    mount()
    await open()
    expect(screen.getByRole('radio', { name: 'Server (America/New_York)' })).toHaveFocus()
  })

  it('stays inside the viewport: fixed, width-capped and scrollable with the thin scrollbar', async () => {
    mount()
    await open()
    const panel = zoneGroup().closest('[role="group"]') as HTMLElement
    expect(panel.style.position, 'floating-ui fixed strategy').toBe('fixed')
    expect(panel.className).toContain('max-w-[calc(100vw-16px)]')
    expect(panel.className).toContain('overflow-y-auto')
    expect(panel.className).toContain('scrollbar-thin')
  })
})

describe('TIME-12H-001 [AC-024] selecting UTC updates the clock and a Timestamp without remount and keeps the panel open', () => {
  it('applies the zone to the clock and to a timestamp on the same node', async () => {
    mount()
    const stamp = screen.getByText('2026-10-27 1:05:09 PM EDT')
    await open()
    await userEvent.click(screen.getByRole('radio', { name: 'UTC' }))

    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
    expect(stamp).toHaveTextContent('2026-10-27 5:05:09 PM UTC')
    expect(stamp.isConnected, 'the timestamp must not be remounted').toBe(true)
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('radio', { name: 'UTC' })).toBeChecked()
  })
})

describe('TIME-12H-001 [AC-025] selecting 24-hour updates the clock', () => {
  it('shows the 24-hour server-zone time', async () => {
    mount()
    await open()
    await userEvent.click(screen.getByRole('radio', { name: '24-hour' }))
    expect(clock()).toHaveTextContent('13:05:09 EDT')
    expect(screen.getByText('2026-10-27 13:05:09 EDT')).toBeInTheDocument()
  })

  it('persists both keys', async () => {
    const storage = memoryStorage()
    mount({ storage })
    await open()
    await userEvent.click(screen.getByRole('radio', { name: '24-hour' }))
    expect(storage.data.get(TIME_ZONE_STORAGE_KEY)).toBe('Server')
    expect(storage.data.get(HOUR_CYCLE_STORAGE_KEY)).toBe('24h')
  })
})

describe('TIME-12H-001 [AC-026] Escape returns focus to the clock and an outside click closes', () => {
  it('closes on Escape and focuses the clock button', async () => {
    mount()
    await open()
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
    await userEvent.keyboard('{Escape}')
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('radiogroup')).toBeNull()
    expect(clock()).toHaveFocus()
  })

  it('closes on a click outside the panel', async () => {
    mount()
    await open()
    await userEvent.click(screen.getByRole('button', { name: 'outside' }))
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByRole('radiogroup')).toBeNull()
  })

  it('closes again after reopening and clicking on the page body', async () => {
    mount()
    await open()
    await userEvent.keyboard('{Escape}')
    await open()
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
    await userEvent.click(document.body)
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
  })

  it('closes on a second activation of the clock', async () => {
    mount()
    await open()
    await open()
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
  })

  it('closes when focus leaves with Tab', async () => {
    mount()
    await open()
    await userEvent.tab()
    await userEvent.tab()
    await userEvent.tab()
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
  })

  it('stays open when an option is selected', async () => {
    mount()
    await open()
    await userEvent.click(screen.getByRole('radio', { name: '24-hour' }))
    await userEvent.click(screen.getByRole('radio', { name: 'Local (America/Los_Angeles)' }))
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
  })

  it('stays open when the panel background is clicked', async () => {
    mount()
    await open()
    await userEvent.click(screen.getByText('Time zone'))
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
  })
})

describe('TIME-12H-001 [AC-030] the panel is operable by keyboard alone', () => {
  it('tabs to the clock, opens with Enter, selects UTC with arrow keys and closes with Escape', async () => {
    mount()
    await userEvent.tab()
    expect(clock()).toHaveFocus()

    await userEvent.keyboard('{Enter}')
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByRole('radio', { name: 'Server (America/New_York)' })).toHaveFocus()

    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('radio', { name: 'Local (America/Los_Angeles)' })).toBeChecked()
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('radio', { name: 'UTC' })).toBeChecked()

    await userEvent.keyboard('{Escape}')
    expect(clock()).toHaveAttribute('aria-expanded', 'false')
    expect(clock()).toHaveFocus()
    expect(clock()).toHaveTextContent('5:05:09 PM UTC')
  })

  it('opens with Space too', async () => {
    mount()
    await userEvent.tab()
    await userEvent.keyboard(' ')
    expect(clock()).toHaveAttribute('aria-expanded', 'true')
  })

  it('moves from the Time zone group to the Clock group with Tab and changes the cycle with an arrow key', async () => {
    mount()
    await userEvent.tab()
    await userEvent.keyboard('{Enter}')
    await userEvent.tab()
    expect(screen.getByRole('radio', { name: '12-hour' })).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('radio', { name: '24-hour' })).toBeChecked()
    expect(clock()).toHaveTextContent('13:05:09 EDT')
  })
})

describe('TIME-12H-001 [NFR-008] every panel control carries the shared focus ring', () => {
  it('gives the clock button and every radio the focus-visible ring', async () => {
    mount()
    await open()
    expect(clock().className).toContain('focus-visible:ring-2')
    const radios = screen.getAllByRole('radio')
    expect(radios).toHaveLength(5)
    for (const radio of radios) {
      expect(radio.className, `radio ${radio.getAttribute('value')}`).toContain('focus-visible:ring-2')
    }
  })
})
