import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PatchConfirmOptions } from './PatchConfirmOptions'
import type { UpdateInfo } from '../types/models'

const LABEL = 'Reboot automatically if the patch requires it'

function updates(over: Partial<UpdateInfo> = {}): UpdateInfo {
  return {
    total_installed: 100,
    pending_updates: 3,
    pending_security_count: 0,
    pending_kernel_update: false,
    unattended_upgrades: false,
    allow_reboot: true,
    supports_reboot_if_required: true,
    ...over,
  } as UpdateInfo
}

function Harness({ info }: { info: UpdateInfo | undefined }) {
  const [checked, setChecked] = useState(false)
  return <PatchConfirmOptions updates={info} checked={checked} onChange={setChecked} />
}

describe('[PATCH-FLOW-001 AC-007] the checkbox', () => {
  it('is labelled exactly and starts unchecked', () => {
    render(<Harness info={updates()} />)
    const box = screen.getByRole('checkbox', { name: LABEL })
    expect(box).toBeEnabled()
    expect(box).not.toBeChecked()
  })

  it('reports ticks to the caller', async () => {
    const onChange = vi.fn()
    render(<PatchConfirmOptions updates={updates()} checked={false} onChange={onChange} />)
    await userEvent.click(screen.getByRole('checkbox', { name: LABEL }))
    expect(onChange).toHaveBeenCalledWith(true)
  })

  it('is never shown ticked while it is disabled, even if the caller state is true', () => {
    render(<PatchConfirmOptions updates={updates({ allow_reboot: false })} checked onChange={() => {}} />)
    expect(screen.getByRole('checkbox', { name: LABEL })).not.toBeChecked()
  })
})

describe('[PATCH-FLOW-001 AC-008] allow_reboot=false', () => {
  it('disables the box and shows the visible reason, linked to the box', () => {
    render(<Harness info={updates({ allow_reboot: false })} />)
    const box = screen.getByRole('checkbox', { name: LABEL })
    expect(box).toBeDisabled()
    const reason = screen.getByText('Reboots are turned off on this agent (commands.allow_reboot: false).')
    expect(reason).toBeVisible()
    expect(box).toHaveAccessibleDescription('Reboots are turned off on this agent (commands.allow_reboot: false).')
  })
})

describe('[PATCH-FLOW-001 AC-009] an older agent', () => {
  it('disables the box when neither allow_reboot nor the capability is reported', () => {
    render(<Harness info={updates({ allow_reboot: undefined, supports_reboot_if_required: undefined })} />)
    expect(screen.getByRole('checkbox', { name: LABEL })).toBeDisabled()
    expect(screen.getByText('The agent must be updated to use this option.')).toBeVisible()
  })

  it('disables the box when allow_reboot is true but the capability is missing', () => {
    render(<Harness info={updates({ supports_reboot_if_required: undefined })} />)
    expect(screen.getByRole('checkbox', { name: LABEL })).toBeDisabled()
    expect(screen.getByText('The agent must be updated to use this option.')).toBeVisible()
  })

  it('disables the box without any update telemetry', () => {
    render(<Harness info={undefined} />)
    expect(screen.getByRole('checkbox', { name: LABEL })).toBeDisabled()
  })
})

describe('[PATCH-FLOW-001 AC-010] what happens to kernel/GPU updates', () => {
  const held = { pending_reboot_class_count: 1, hold_enforcement: 'active' as const, held_packages: ['linux-image-amd64'] }
  const HELD = 'Kernel and GPU driver updates are held by rIOt and will not be installed by this run.'
  const TICKED = 'Kernel and GPU driver updates will be installed. The host reboots after the run if a reboot is required.'
  const UNHELD = 'Kernel and GPU driver updates will be installed. A reboot will be needed afterwards, and rIOt will not reboot the host.'

  it('(a) holds active and unticked, then (b) ticked, in the same dialog', async () => {
    render(<Harness info={updates(held)} />)
    expect(screen.getByText(HELD)).toBeVisible()
    expect(screen.queryByText(TICKED)).not.toBeInTheDocument()

    await userEvent.click(screen.getByRole('checkbox', { name: LABEL }))

    expect(screen.getByText(TICKED)).toBeVisible()
    expect(screen.queryByText(HELD)).not.toBeInTheDocument()
  })

  it('(c) holds not active and unticked', () => {
    render(<Harness info={updates({ pending_reboot_class_count: 1 })} />)
    expect(screen.getByText(UNHELD)).toBeVisible()
  })

  it('shows none of the statements when no reboot-class update is pending', () => {
    render(<Harness info={updates({ pending_reboot_class_count: 0 })} />)
    for (const text of [HELD, TICKED, UNHELD]) {
      expect(screen.queryByText(text)).not.toBeInTheDocument()
    }
  })
})

describe('[PATCH-FLOW-001 AC-011] a reboot that is already required', () => {
  it('says so and that, with the box ticked, the host reboots after the run', () => {
    render(<Harness info={updates({ reboot_required: true })} />)
    expect(screen.getByText('A reboot is already required on this host. With the box ticked, the host reboots after the run.')).toBeVisible()
  })
})

describe('[PATCH-FLOW-001 AC-027] stale lists', () => {
  it('explains that Patch refreshes the lists first', () => {
    render(<Harness info={updates({ lists_freshness: 'stale' })} />)
    expect(screen.getByText(/Running Patch refreshes them first/)).toBeVisible()
  })
})
