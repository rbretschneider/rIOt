// Added by QA Engineer
// Covers AC-013: nginx metric types must appear in the AlertRuleSettings METRICS
// dropdown and METRIC_DEFAULTS must include defaults for all three nginx metrics.
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import AlertRuleSettings from './AlertRuleSettings'

vi.mock('../../api/settings', () => ({
  settingsApi: {
    getAlertRules: vi.fn().mockResolvedValue([]),
    getAlertTemplates: vi.fn().mockResolvedValue([
      {
        id: 'nginx_5xx_high',
        name: 'Nginx 5xx Errors High',
        category: 'webserver',
        metric: 'nginx_5xx_count',
        operator: '>',
        threshold: 10,
        severity: 'critical',
        cooldown_seconds: 300,
        description: 'Fires when nginx 5xx error count exceeds the threshold per telemetry interval',
      },
      {
        id: 'nginx_4xx_high',
        name: 'Nginx 4xx Errors High',
        category: 'webserver',
        metric: 'nginx_4xx_count',
        operator: '>',
        threshold: 50,
        severity: 'warning',
        cooldown_seconds: 900,
        description: 'Fires when nginx 4xx error count exceeds the threshold per telemetry interval',
      },
    ]),
  },
}))

vi.mock('../../api/client', () => ({
  api: {},
}))

function renderWithProviders() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AlertRuleSettings />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('[AC-013] nginx metrics in AlertRuleSettings', () => {
  it('includes nginx_5xx_count, nginx_4xx_count, nginx_request_count in the metric select', async () => {
    // Added by QA Engineer — Covers AC-013
    renderWithProviders()

    // Open the "Add Rule" form by clicking the first "Add Rule" button
    const addRuleButtons = await screen.findAllByRole('button', { name: /add rule/i })
    fireEvent.click(addRuleButtons[0])

    // The metric <select> is the first combobox in the modal.
    // Its options are the METRICS array entries.
    const selects = screen.getAllByRole('combobox')
    // First select in the modal is the Metric select (before Operator)
    const metricSelect = selects[0]

    const optionValues = Array.from(metricSelect.querySelectorAll('option')).map(
      (o) => (o as HTMLOptionElement).value,
    )

    expect(optionValues).toContain('nginx_5xx_count')
    expect(optionValues).toContain('nginx_4xx_count')
    expect(optionValues).toContain('nginx_request_count')
  })

  it('shows nginx labels "Nginx 5xx Errors", "Nginx 4xx Errors", "Nginx Request Count" in options', async () => {
    // Added by QA Engineer — Covers AC-013
    renderWithProviders()

    const addRuleButtons = await screen.findAllByRole('button', { name: /add rule/i })
    fireEvent.click(addRuleButtons[0])

    const selects = screen.getAllByRole('combobox')
    const metricSelect = selects[0]

    const optionLabels = Array.from(metricSelect.querySelectorAll('option')).map(
      (o) => (o as HTMLOptionElement).textContent,
    )

    expect(optionLabels).toContain('Nginx 5xx Errors')
    expect(optionLabels).toContain('Nginx 4xx Errors')
    expect(optionLabels).toContain('Nginx Request Count')
  })
})
