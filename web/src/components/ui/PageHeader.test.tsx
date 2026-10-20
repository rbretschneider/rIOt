import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { PageHeader } from './PageHeader'

describe('PageHeader', () => {
  it('[APP-WORKSPACE AC-006] renders one title and independent optional slots', () => {
    render(
      <PageHeader
        title="Device name"
        eyebrow="Device"
        description="Description"
        actions={<button disabled>Restart</button>}
      >
        <a href="/">Devices</a>
        <span>Offline</span>
      </PageHeader>,
    )
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1)
    expect(
      screen.getByRole('heading', { name: 'Device name' }),
    ).toBeInTheDocument()
    expect(screen.getByText('Description')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Restart' })).toBeDisabled()
    expect(screen.getByRole('link', { name: 'Devices' })).toHaveAttribute(
      'href',
      '/',
    )
    expect(screen.getByText('Offline')).toBeInTheDocument()
  })
  it('[APP-WORKSPACE AC-006] supports a title without optional metadata or controls', () => {
    const { container } = render(<PageHeader title="Devices" />)
    expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent(
      'Devices',
    )
    expect(container.querySelectorAll('button,a')).toHaveLength(0)
  })
})
