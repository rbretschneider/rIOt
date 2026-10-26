import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { PatchConfirmOptions } from './PatchConfirmOptions'
import type { ExtraUpdater, UpdateInfo } from '../types/models'

const BOX = 'Apply DietPi update in this run'

const dietpi: ExtraUpdater = {
  id: 'dietpi', status: 'update_available', installed_version: '9.20.1', available_version: '10.8.2',
  pending_count: 1, readiness: 'enabled',
}

function updates(extra: ExtraUpdater[] | undefined): UpdateInfo {
  return {
    package_manager: 'apt', total_installed: 100, pending_updates: 3, pending_security_count: 0,
    pending_kernel_update: false, unattended_upgrades: false, allow_reboot: true,
    supports_reboot_if_required: true, extra_updaters: extra,
  } as UpdateInfo
}

function Harness({ info }: { info: UpdateInfo }) {
  const [reboot, setReboot] = useState(false)
  const [extras, setExtras] = useState(true)
  return <PatchConfirmOptions updates={info} checked={reboot} onChange={setReboot} extrasChecked={extras} onExtrasChange={setExtras} />
}

describe('[EXTRA-UPD-001 AC-040] the "Apply DietPi update in this run" box', () => {
  it('is shown ticked for a pending, enabled DietPi', () => {
    render(<Harness info={updates([dietpi])} />)
    expect(screen.getByRole('checkbox', { name: BOX })).toBeChecked()
  })

  it('reports an untick to the caller', async () => {
    const onExtrasChange = vi.fn()
    render(<PatchConfirmOptions updates={updates([dietpi])} checked={false} onChange={() => {}} extrasChecked onExtrasChange={onExtrasChange} />)
    await userEvent.click(screen.getByRole('checkbox', { name: BOX }))
    expect(onExtrasChange).toHaveBeenCalledWith(false)
  })

  it('defaults to ticked when the caller does not control it', () => {
    render(<PatchConfirmOptions updates={updates([dietpi])} checked={false} onChange={() => {}} />)
    expect(screen.getByRole('checkbox', { name: BOX })).toBeChecked()
  })

  it('switches the statements when unticked', async () => {
    render(<Harness info={updates([dietpi])} />)
    expect(screen.getByText(/stops and restarts the services it manages/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('checkbox', { name: BOX }))
    expect(screen.queryByText(/stops and restarts the services it manages/)).not.toBeInTheDocument()
    expect(screen.getByText(/will not be applied in this run: you unticked/)).toBeInTheDocument()
  })

  it('discloses version change, order, limit, service stop, autopurge and the reboot pointer', () => {
    render(<Harness info={updates([{ ...dietpi, notices: [{ code: 'major_upgrade' }] }])} />)
    expect(screen.getByText(/DietPi 9\.20\.1 to 10\.8\.2 will be applied after the apt packages\. It may take up to 40 minutes\./)).toBeInTheDocument()
    expect(screen.getByText(/stops and restarts the services it manages.*apt-get autopurge/)).toBeInTheDocument()
    expect(screen.getByText(/kernel or firmware may change.*Tick "Reboot automatically if the patch requires it"/)).toBeInTheDocument()
    expect(screen.getByRole('checkbox', { name: 'Reboot automatically if the patch requires it' })).toBeInTheDocument()
  })

  it('has no box and says why for DietPi not enabled or unknown', () => {
    const { unmount } = render(<Harness info={updates([{ ...dietpi, readiness: 'not_opted_in' }])} />)
    expect(screen.queryByRole('checkbox', { name: BOX })).not.toBeInTheDocument()
    expect(screen.getByText(/will not be applied in this run: it is not opted in/)).toBeInTheDocument()
    unmount()
    render(<Harness info={updates([{ ...dietpi, status: 'unknown', unknown_reason: 'check_disabled' }])} />)
    expect(screen.queryByRole('checkbox', { name: BOX })).not.toBeInTheDocument()
    expect(screen.getByText(/DietPi will not be applied in this run: its update status is unknown/)).toBeInTheDocument()
  })

  it('does not change the dialog of a host without extra updaters', () => {
    render(<Harness info={updates(undefined)} />)
    expect(screen.queryByRole('checkbox', { name: BOX })).not.toBeInTheDocument()
    expect(screen.queryByText(/DietPi/)).not.toBeInTheDocument()
  })
})
