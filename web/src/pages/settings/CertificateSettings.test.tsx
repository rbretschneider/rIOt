import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen } from '@testing-library/react'
import CertificateSettings from './CertificateSettings'
import { settingsApi } from '../../api/settings'
import { renderWithProviders } from '../../test/renderWithProviders'

vi.mock('../../api/settings', () => ({
  settingsApi: {
    getCerts: vi.fn(),
    getBootstrapKeys: vi.fn(),
    revokeCert: vi.fn(),
    createBootstrapKey: vi.fn(),
    deleteBootstrapKey: vi.fn(),
  },
}))

beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(settingsApi.getCerts).mockResolvedValue([
    {
      id: 1,
      device_id: 'abcdef1234567890',
      serial_number: '0123456789abcdef0123',
      not_before: '2026-01-01T00:00:00Z',
      not_after: '2027-03-04T05:06:00Z',
      revoked: false,
      created_at: '2026-01-01T00:00:00Z',
    },
  ] as never)
  vi.mocked(settingsApi.getBootstrapKeys).mockResolvedValue([
    {
      key_hash: 'hash1',
      label: 'lab key',
      used: false,
      created_at: '2026-10-01T08:09:10Z',
      expires_at: '2099-12-31T23:59:58Z',
    },
  ] as never)
})

describe('[UIF AC-109] timestamps carry UTC label', () => {
  it('shows the certificate expiry to the minute with a UTC label', async () => {
    renderWithProviders(<CertificateSettings />)
    expect(await screen.findByText('2027-03-04 05:06 UTC')).toBeInTheDocument()
  })

  it('shows bootstrap key expiry and creation to the second with a UTC label', async () => {
    renderWithProviders(<CertificateSettings />)
    expect(await screen.findByText('2099-12-31 23:59:58 UTC')).toBeInTheDocument()
    expect(screen.getByText('2026-10-01 08:09:10 UTC')).toBeInTheDocument()
  })

  it('never renders an unlabelled locale date', async () => {
    renderWithProviders(<CertificateSettings />)
    await screen.findByText('2027-03-04 05:06 UTC')
    for (const el of Array.from(document.querySelectorAll('time'))) {
      expect(el.textContent).toMatch(/UTC$/)
    }
  })
})
