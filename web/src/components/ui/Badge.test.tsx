import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { Badge, type BadgeTone } from './Badge'

describe('[UIF AC-020] badge uses the shared map and tone', () => {
  it('shows the mapped label and the danger tone for critical', () => {
    render(<Badge category="severity" value="critical" />)
    const badge = screen.getByText('Critical')
    expect(badge).toHaveAttribute('data-tone', 'danger')
    expect(screen.queryByText('critical')).toBeNull()
  })

  it('shows the mapped label and tone for a command status', () => {
    render(<Badge category="commandStatus" value="queued" />)
    const badge = screen.getByText('Queued')
    expect(badge).toHaveAttribute('data-tone', 'warn')
  })

  it('lets an explicit tone override the category tone', () => {
    render(<Badge category="severity" value="critical" tone="neutral" />)
    expect(screen.getByText('Critical')).toHaveAttribute('data-tone', 'neutral')
  })

  it('humanizes an unknown value and renders an em dash for a missing one', () => {
    render(
      <>
        <Badge category="eventType" value="cpu_high" />
        <Badge category="severity" value={undefined} />
      </>,
    )
    expect(screen.getByText('Cpu high')).toBeInTheDocument()
    expect(screen.getByText('—')).toBeInTheDocument()
  })

  it.each<BadgeTone>(['neutral', 'accent', 'success', 'warn', 'danger', 'info'])(
    'renders children with data-tone=%s',
    tone => {
      render(<Badge tone={tone}>Label</Badge>)
      expect(screen.getByText('Label')).toHaveAttribute('data-tone', tone)
    },
  )

  it('defaults the children form to the neutral tone', () => {
    render(<Badge>Label</Badge>)
    expect(screen.getByText('Label')).toHaveAttribute('data-tone', 'neutral')
  })

  it('gives each tone a different class set', () => {
    const tones: BadgeTone[] = ['neutral', 'accent', 'success', 'warn', 'danger', 'info']
    const classes = tones.map(tone => {
      const { unmount } = render(<Badge tone={tone}>x</Badge>)
      const c = screen.getByText('x').className
      unmount()
      return c
    })
    expect(new Set(classes).size).toBe(tones.length)
  })
})
