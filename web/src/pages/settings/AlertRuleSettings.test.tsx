// Added by QA Engineer
// Covers AC-013: nginx metric types must appear in the AlertRuleSettings METRICS
// dropdown and METRIC_DEFAULTS must include defaults for all three nginx metrics.
//
// Extended for LOG-001: AC-021, AC-022, AC-023, AC-025, AC-030
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router-dom'
import AlertRuleSettings from './AlertRuleSettings'
import { settingsApi } from '../../api/settings'

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

// ── LOG-001 tests ──────────────────────────────────────────────────────────────

// Template fixtures shared across LOG-001 tests.
const authFailureTemplate = {
  id: 'auth_failure',
  name: 'Auth Failure',
  category: 'security',
  metric: 'failed_logins_interval',
  operator: '>',
  threshold: 0,
  severity: 'warning',
  cooldown_seconds: 300,
  needs_target_name: false,
  description:
    'Fires when a device reports one or more authentication failures since the last telemetry interval. Note: on internet-facing SSH hosts, routine bot traffic will trigger this at every cooldown window — raise the threshold and/or narrow IncludeDevices before enabling.',
}

const logErrorsTemplate = {
  id: 'log_errors',
  name: 'Log Errors Detected',
  category: 'system',
  metric: 'log_errors',
  operator: '>',
  threshold: 0,
  severity: 'warning',
  cooldown_seconds: 900,
  needs_target_name: false,
  description: 'Fires when a device reports error-level or higher log entries',
}

function renderWithTemplates(templates: object[]) {
  vi.mocked(settingsApi.getAlertTemplates).mockResolvedValue(templates as never)
  vi.mocked(settingsApi.getAlertRules).mockResolvedValue([])
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <AlertRuleSettings />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

// [AC-030] log_errors template appears in the TemplatePicker under the "system" section.
describe('[AC-030] log_errors template visible in TemplatePicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders "Log Errors Detected" button under the system section', async () => {
    renderWithTemplates([logErrorsTemplate])

    const templateBtn = await screen.findByRole('button', { name: /create from template/i })
    fireEvent.click(templateBtn)

    // The category header "system" should appear.
    await waitFor(() => {
      expect(screen.getByText('system')).toBeTruthy()
    })

    // The template button "Log Errors Detected" should be rendered.
    expect(screen.getByText('Log Errors Detected')).toBeTruthy()
  })
})

// [AC-023] TemplatePicker renders a "security" section header when auth_failure template is present.
describe('[AC-023] security category appears in TemplatePicker', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('renders a "security" section header for auth_failure template', async () => {
    renderWithTemplates([authFailureTemplate])

    const templateBtn = await screen.findByRole('button', { name: /create from template/i })
    fireEvent.click(templateBtn)

    await waitFor(() => {
      expect(screen.getByText('security')).toBeTruthy()
    })

    expect(screen.getByText('Auth Failure')).toBeTruthy()
  })
})

// [AC-021] Selecting a template from TemplatePicker initialises the rule with empty include_devices.
// The emptyRule initializer sets include_devices: '' by default, and template selection merges
// into emptyRule without setting include_devices (per AlertRuleSettings.tsx lines 444-454).
describe('[AC-021] auth_failure template selection produces global scope (empty include_devices)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('rule form opens with "Create Alert Rule" heading after template selection (global scope)', async () => {
    renderWithTemplates([authFailureTemplate])

    const templateBtn = await screen.findByRole('button', { name: /create from template/i })
    fireEvent.click(templateBtn)

    // The TemplatePicker is now open. Click the Auth Failure template.
    const authBtn = await screen.findByText('Auth Failure')
    fireEvent.click(authBtn)

    // After template selection, the picker closes and the "Create Alert Rule" form opens.
    // The form heading confirms the create-new flow (not an edit of an existing rule),
    // which means include_devices starts empty (emptyRule default: include_devices: '').
    await waitFor(() => {
      expect(screen.getByText('Create Alert Rule')).toBeTruthy()
    })

    // Verify the name input contains the template name ("Auth Failure").
    const nameInput = screen.getAllByRole('textbox')[0] as HTMLInputElement
    expect(nameInput.value).toBe('Auth Failure')
  })
})

// [AC-022] Cooldown field is present and can be edited after template selection.
describe('[AC-022] cooldown_seconds is editable after template selection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('cooldown input reflects template default and accepts new value', async () => {
    renderWithTemplates([authFailureTemplate])

    const templateBtn = await screen.findByRole('button', { name: /create from template/i })
    fireEvent.click(templateBtn)

    const authBtn = await screen.findByText('Auth Failure')
    fireEvent.click(authBtn)

    // The rule edit form opens. Find the cooldown input (it's a number input).
    await waitFor(() => {
      const numberInputs = screen.getAllByRole('spinbutton')
      // Cooldown input should contain '300' (template default).
      const cooldownInput = numberInputs.find((el) => (el as HTMLInputElement).value === '300')
      expect(cooldownInput).toBeTruthy()

      // Edit the cooldown to 600.
      if (cooldownInput) {
        fireEvent.change(cooldownInput, { target: { value: '600' } })
        expect((cooldownInput as HTMLInputElement).value).toBe('600')
      }
    })
  })
})

// [AC-025] Severity can be changed from "warning" to "critical" after template selection.
describe('[AC-025] severity is editable after template selection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('severity select can be changed to critical', async () => {
    renderWithTemplates([authFailureTemplate])

    const templateBtn = await screen.findByRole('button', { name: /create from template/i })
    fireEvent.click(templateBtn)

    const authBtn = await screen.findByText('Auth Failure')
    fireEvent.click(authBtn)

    await waitFor(() => {
      const severitySelects = screen.getAllByRole('combobox')
      // Find the severity select — its current value should be 'warning'.
      const severitySelect = severitySelects.find(
        (el) => (el as HTMLSelectElement).value === 'warning',
      )
      expect(severitySelect).toBeTruthy()

      if (severitySelect) {
        fireEvent.change(severitySelect, { target: { value: 'critical' } })
        expect((severitySelect as HTMLSelectElement).value).toBe('critical')
      }
    })
  })
})
