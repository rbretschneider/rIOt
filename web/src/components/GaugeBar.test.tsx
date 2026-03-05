import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import GaugeBar from './GaugeBar'

describe('GaugeBar', () => {
  it('renders label and value', () => {
    render(<GaugeBar label="CPU" value={45.3} />)
    expect(screen.getByText('CPU')).toBeInTheDocument()
    expect(screen.getByText('45.3%')).toBeInTheDocument()
  })

  it('uses green color for values under 75%', () => {
    const { container } = render(<GaugeBar label="Mem" value={50} />)
    const bar = container.querySelector('[style]')
    expect(bar?.className).toContain('emerald')
  })

  it('uses amber color for values between 75% and 90%', () => {
    const { container } = render(<GaugeBar label="Mem" value={80} />)
    const bar = container.querySelector('[style]')
    expect(bar?.className).toContain('amber')
  })

  it('uses red color for values over 90%', () => {
    const { container } = render(<GaugeBar label="Disk" value={95} />)
    const bar = container.querySelector('[style]')
    expect(bar?.className).toContain('red')
  })

  it('renders custom unit', () => {
    render(<GaugeBar label="Temp" value={72.0} unit="°C" max={100} />)
    expect(screen.getByText('72.0°C')).toBeInTheDocument()
  })
})
