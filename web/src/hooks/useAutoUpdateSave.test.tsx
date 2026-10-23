import { beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders } from '../test/renderWithProviders'
import { ApiError } from '../api/errors'
import { autoUpdateKey, useAutoUpdateSave } from './useAutoUpdateSave'

const mock = vi.hoisted(() => ({ setAutoUpdate: vi.fn(), deleteAutoUpdate: vi.fn() }))
vi.mock('../api/client', () => ({ api: mock }))

function Harness({ server, stack = false }: { server: boolean; stack?: boolean }) {
  const { save, display } = useAutoUpdateSave()
  const { checked, pending } = display(autoUpdateKey('host-1', stack, 'web'), server)
  return (
    <button
      role="switch"
      aria-checked={checked}
      aria-busy={pending}
      aria-label="Auto-update web"
      onClick={() =>
        void save({ deviceId: 'host-1', hostname: 'host-one', target: 'web', stack, workingDir: stack ? '/srv/web' : '', enabled: !checked })
      }
    />
  )
}

const deferred = () => {
  let resolve!: (v: unknown) => void
  let reject!: (e: unknown) => void
  const promise = new Promise((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('[AC-003] one-click save', () => {
  it('shows the new value while saving and ignores a second flip', async () => {
    const user = userEvent.setup()
    const pending = deferred()
    mock.setAutoUpdate.mockReturnValue(pending.promise)
    renderWithProviders(<Harness server={false} />)
    const toggle = screen.getByRole('switch', { name: 'Auto-update web' })
    await user.click(toggle)
    expect(toggle).toHaveAttribute('aria-checked', 'true')
    expect(toggle).toHaveAttribute('aria-busy', 'true')
    await user.click(toggle)
    expect(mock.setAutoUpdate).toHaveBeenCalledTimes(1)
    expect(mock.setAutoUpdate).toHaveBeenCalledWith('host-1', 'web', false, '', true)
    pending.resolve({ status: 'ok' })
    await waitFor(() => expect(toggle).toHaveAttribute('aria-busy', 'false'))
  })
  it('refreshes the lists after a successful save', async () => {
    const user = userEvent.setup()
    mock.setAutoUpdate.mockResolvedValue({ status: 'ok' })
    const { queryClient } = renderWithProviders(<Harness server={false} stack />)
    const spy = vi.spyOn(queryClient, 'invalidateQueries')
    await user.click(screen.getByRole('switch', { name: 'Auto-update web' }))
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(4))
    expect(spy.mock.calls.map((c) => c[0]?.queryKey)).toEqual([
      ['docker-containers'],
      ['docker-stacks'],
      ['auto-updates', 'host-1'],
      ['docker-stack', 'host-1', 'web'],
    ])
    expect(mock.setAutoUpdate).toHaveBeenCalledWith('host-1', 'web', true, '/srv/web', true)
  })
  it('turns off by storing enabled=false and never deletes the policy', async () => {
    const user = userEvent.setup()
    mock.setAutoUpdate.mockResolvedValue({ status: 'ok' })
    renderWithProviders(<Harness server />)
    await user.click(screen.getByRole('switch', { name: 'Auto-update web' }))
    await waitFor(() => expect(mock.setAutoUpdate).toHaveBeenCalledWith('host-1', 'web', false, '', false))
    expect(mock.deleteAutoUpdate).not.toHaveBeenCalled()
  })
  it('reverts and names the target in an error toast when the save fails', async () => {
    const user = userEvent.setup()
    mock.setAutoUpdate.mockRejectedValue(new ApiError(500, 'failed to save auto-update policy'))
    renderWithProviders(<Harness server={false} />)
    const toggle = screen.getByRole('switch', { name: 'Auto-update web' })
    await user.click(toggle)
    expect(await screen.findByText('Auto-update not changed for container web on host-one')).toBeInTheDocument()
    expect(screen.getByText('failed to save auto-update policy')).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-checked', 'false')
    expect(toggle).toHaveAttribute('aria-busy', 'false')
    mock.setAutoUpdate.mockResolvedValue({ status: 'ok' })
    await user.click(toggle)
    expect(mock.setAutoUpdate).toHaveBeenCalledTimes(2)
  })
})

describe('[AC-011] name clash', () => {
  it.each([
    ['stack', 'A stack with the same name on this host has auto-update on. Turn that off first.'],
    ['container', 'A container with the same name on this host has auto-update on. Turn that off first.'],
    ['<b>odd</b>', 'A policy of the other kind with the same name on this host has auto-update on. Turn that off first.'],
  ])('explains a clash with an enabled %s policy and reverts', async (kind, text) => {
    const user = userEvent.setup()
    mock.setAutoUpdate.mockRejectedValue(new ApiError(409, 'clash', 'auto_update_name_clash', { existing_kind: kind }))
    renderWithProviders(<Harness server={false} />)
    const toggle = screen.getByRole('switch', { name: 'Auto-update web' })
    await user.click(toggle)
    expect(await screen.findByText(text)).toBeInTheDocument()
    expect(toggle).toHaveAttribute('aria-checked', 'false')
  })
})
