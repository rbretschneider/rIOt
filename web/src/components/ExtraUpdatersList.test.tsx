import { describe, expect, it } from 'vitest'
import { render, screen, within } from '@testing-library/react'
import ExtraUpdatersList from './ExtraUpdatersList'
import type { ExtraUpdater } from '../types/models'

const pending: ExtraUpdater = {
  id: 'dietpi',
  status: 'update_available',
  installed_version: '9.20.1',
  available_version: '10.8.2',
  pending_count: 1,
  readiness: 'enabled',
  notices: [{ code: 'major_upgrade' }, { code: 'dietpi_auto_apt_upgrade' }],
}

describe('[EXTRA-UPD-001 AC-039] the updater list on the device page', () => {
  it('shows name, both versions, status and each notice in words', () => {
    render(<ExtraUpdatersList extras={[pending]} />)
    const row = screen.getByTestId('extra-updater-dietpi')
    expect(within(row).getByText('DietPi')).toBeInTheDocument()
    expect(row).toHaveTextContent('Installed 9.20.1')
    expect(row).toHaveTextContent('Available 10.8.2')
    expect(row).toHaveTextContent('Update available')
    expect(row).toHaveTextContent('major upgrade: the kernel or firmware may change')
    expect(row).toHaveTextContent('CONFIG_CHECK_APT_UPDATES=2')
    expect(row).not.toHaveTextContent('Will not be applied')
  })

  it('shows the unknown reason in words and never says up to date', () => {
    render(<ExtraUpdatersList extras={[{ id: 'dietpi', status: 'unknown', unknown_reason: 'check_disabled', pending_count: 0 }]} />)
    const row = screen.getByTestId('extra-updater-dietpi')
    expect(row).toHaveTextContent('Unknown: its own update check is turned off')
    expect(row.textContent?.toLowerCase()).not.toContain('up to date')
    expect(row).toHaveTextContent('Installed unknown')
  })

  it('says why a pending updater will not be applied', () => {
    render(<ExtraUpdatersList extras={[{ ...pending, readiness: 'root_side_missing' }]} />)
    expect(screen.getByTestId('extra-updater-dietpi')).toHaveTextContent('Will not be applied: the root-side opt-in is missing')
  })

  it('shows an up to date updater as up to date', () => {
    render(<ExtraUpdatersList extras={[{ ...pending, status: 'up_to_date', notices: undefined }]} />)
    expect(screen.getByTestId('extra-updater-dietpi')).toHaveTextContent('Up to date')
  })

  it('renders nothing for an absent or empty list', () => {
    const { container, rerender } = render(<ExtraUpdatersList extras={undefined} />)
    expect(container).toBeEmptyDOMElement()
    rerender(<ExtraUpdatersList extras={[]} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders hostile text as text and scrolls with the thin scrollbar', () => {
    render(<ExtraUpdatersList extras={[{ ...pending, id: 'dietpi', installed_version: '<b>9</b>' }]} />)
    expect(screen.getByTestId('extra-updaters').querySelector('b')).toBeNull()
    expect(screen.getByTestId('extra-updaters')).toHaveClass('scrollbar-thin')
  })
})
