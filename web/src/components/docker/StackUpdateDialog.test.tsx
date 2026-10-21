import { beforeEach, expect, it, vi } from 'vitest'
import { act, fireEvent, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { renderWithProviders, makeTestQueryClient } from '../../test/renderWithProviders'
import type { Command } from '../../types/models'
import StackUpdateDialog from './StackUpdateDialog'

const mock = vi.hoisted(() => ({ stack: vi.fn(), hosts: vi.fn(), stackAction: vi.fn(), getActiveCommands: vi.fn(), getCommand: vi.fn(), cancelCommand: vi.fn() }))
vi.mock('../../api/docker', () => ({ dockerApi: mock }))
vi.mock('../../api/client', () => ({ api: mock }))
const stack = { device_id: 'host-1', hostname: 'host-one', project: 'app', state: 'down', running: 0, total: 2, updates: 0, path_state: 'agreed', agreed_working_dir: '/srv/app', agreed_config_files: ['compose.yaml'] }
const host = { device_id: 'host-1', capabilities: ['stack_actions_v1'] }
const command: Command = { id: 'cmd-1', device_id: 'host-1', action: 'docker_update', status: 'running', result_msg: '', params: { compose_project: 'app' }, target_keys: ['docker:compose:/srv/app'], duration_ms: null, exit_code: null, created_at: '2026-10-20T10:00:00Z', updated_at: '2026-10-20T10:00:00Z' }
beforeEach(() => {
  vi.resetAllMocks()
  mock.stack.mockResolvedValue(stack)
  mock.hosts.mockResolvedValue([host])
  mock.stackAction.mockResolvedValue(command)
  mock.getActiveCommands.mockResolvedValue([])
  mock.getCommand.mockResolvedValue(command)
})
function renderDialog() {
  const onClose = vi.fn()
  const queryClient = makeTestQueryClient()
  const view = renderWithProviders(<StackUpdateDialog deviceId="host-1" project="app" onClose={onClose} />, { queryClient })
  return { ...view, onClose }
}
async function ready() {
  await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm update' })).toBeEnabled())
}
it('[AC-001][AC-004] shows target and consequences, and close never sends an update', async () => {
  const user = userEvent.setup()
  const { onClose } = renderDialog()
  await ready()
  expect(screen.getByRole('dialog', { name: 'Update app' })).toBeInTheDocument()
  expect(screen.getByText('host-one')).toBeInTheDocument()
  expect(screen.getByText(/\/srv\/app/)).toBeInTheDocument()
  expect(screen.getByText(/stopped services may start/)).toBeInTheDocument()
  await user.click(screen.getByRole('button', { name: 'Close' }))
  expect(onClose).toHaveBeenCalledOnce()
  expect(mock.stackAction).not.toHaveBeenCalled()
})
it('[AC-003] blocks cached agreed paths until the fresh detail response arrives', async () => {
  let resolve!: (value: typeof stack) => void
  mock.stack.mockReturnValue(new Promise<typeof stack>((done) => { resolve = done }))
  const queryClient = makeTestQueryClient()
  queryClient.setQueryData(['docker-stack', 'host-1', 'app'], stack)
  renderWithProviders(<StackUpdateDialog deviceId="host-1" project="app" onClose={() => {}} />, { queryClient })
  expect(screen.getByRole('button', { name: 'Confirm update' })).toBeDisabled()
  await act(async () => { resolve({ ...stack, path_state: 'pending_ack' }) })
  expect(await screen.findByText(/Review this stack’s Compose paths/)).toBeInTheDocument()
  expect(screen.getByRole('button', { name: 'Confirm update' })).toBeDisabled()
  expect(mock.stackAction).not.toHaveBeenCalled()
})
it.each([
  { path_state: 'conflict', agreed_working_dir: '/srv/app' },
  { path_state: 'agreed', agreed_working_dir: '' },
])('[AC-003] refuses unsafe path state $path_state with directory "$agreed_working_dir"', async (paths) => {
  mock.stack.mockResolvedValue({ ...stack, ...paths })
  renderDialog()
  await screen.findByText(/Review this stack’s Compose paths/)
  expect(screen.getByRole('button', { name: 'Confirm update' })).toBeDisabled()
})
it('[AC-003] refuses missing capability and displays the reason', async () => {
  mock.hosts.mockResolvedValue([{ ...host, capabilities: [] }])
  renderDialog()
  await screen.findByText(/Requires an agent with stack actions support/)
  expect(screen.getByRole('button', { name: 'Confirm update' })).toBeDisabled()
})
it.each(['stack', 'hosts'] as const)('[AC-003] fails closed on fresh %s read errors even with cached data', async (resource) => {
  mock[resource].mockRejectedValue(new Error('Fresh read failed'))
  const queryClient = makeTestQueryClient()
  queryClient.setQueryData(['docker-stack', 'host-1', 'app'], stack)
  queryClient.setQueryData(['docker-hosts'], [host])
  renderWithProviders(<StackUpdateDialog deviceId="host-1" project="app" onClose={() => {}} />, { queryClient })
  await screen.findByText('Fresh read failed')
  expect(screen.getByRole('button', { name: 'Confirm update' })).toBeDisabled()
  expect(mock.stackAction).not.toHaveBeenCalled()
})
it('[AC-004] warns when the selected stack contains rIOt infrastructure', async () => {
  mock.stack.mockResolvedValue({ ...stack, self_role: 'server' })
  renderDialog()
  expect(await screen.findByText(/This stack contains rIOt infrastructure/)).toBeInTheDocument()
})
it('[AC-005] sends one exact update request despite repeated confirmation while starting', async () => {
  let resolve!: (value: Command) => void
  mock.stackAction.mockReturnValue(new Promise<Command>((done) => { resolve = done }))
  renderDialog()
  await ready()
  const button = screen.getByRole('button', { name: 'Confirm update' })
  act(() => { fireEvent.click(button); fireEvent.click(button) })
  expect(mock.stackAction).toHaveBeenCalledExactlyOnceWith('host-1', 'app', { action: 'update' })
  await act(async () => { resolve(command) })
  expect(await screen.findByRole('button', { name: 'Update in progress' })).toBeDisabled()
})
it('[AC-005] adopts a conflict instead of creating another command', async () => {
  const user = userEvent.setup()
  mock.stackAction.mockRejectedValue(Object.assign(new Error('Already running'), { name: 'CommandConflictError', existingCommandId: 'existing', existingStatus: 'running' }))
  mock.getCommand.mockResolvedValue({ ...command, id: 'existing' })
  renderDialog()
  await ready()
  await user.click(screen.getByRole('button', { name: 'Confirm update' }))
  await waitFor(() => expect(mock.getCommand).toHaveBeenCalledWith('host-1', 'existing'))
  expect(await screen.findByRole('link', { name: 'View steps and output' })).toHaveAttribute('href', expect.stringContaining('command=existing'))
  expect(mock.stackAction).toHaveBeenCalledOnce()
})
it('[AC-006] keeps a refused dispatch visible without automatically retrying', async () => {
  const user = userEvent.setup()
  mock.stackAction.mockRejectedValue(new Error('Compose paths changed'))
  renderDialog()
  await ready()
  await user.click(screen.getByRole('button', { name: 'Confirm update' }))
  expect(await screen.findByText('Compose paths changed')).toBeInTheDocument()
  expect(mock.stackAction).toHaveBeenCalledOnce()
  expect(screen.queryByRole('link', { name: 'View steps and output' })).not.toBeInTheDocument()
})
it('[AC-006] retains failure and refreshes each affected query once without retrying', async () => {
  const user = userEvent.setup()
  const failure = { ...command, status: 'failed', result_msg: 'Compose pull failed' }
  mock.stackAction.mockResolvedValue(failure)
  mock.getCommand.mockResolvedValue(failure)
  const { queryClient } = renderDialog()
  const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
  await ready()
  await user.click(screen.getByRole('button', { name: 'Confirm update' }))
  await screen.findByText('Compose pull failed')
  for (const queryKey of [['docker-stack', 'host-1', 'app'], ['docker-stacks'], ['docker-containers']]) {
    expect(invalidate.mock.calls.filter(([filters]) => JSON.stringify(filters?.queryKey) === JSON.stringify(queryKey))).toHaveLength(1)
  }
  expect(screen.getByRole('button', { name: 'Retry update' })).toBeInTheDocument()
  expect(mock.stackAction).toHaveBeenCalledOnce()
})
it('[AC-006] reattaches active work without dispatch, and closing does not cancel', async () => {
  const user = userEvent.setup()
  mock.getActiveCommands.mockResolvedValue([command])
  const { onClose } = renderDialog()
  await screen.findByRole('link', { name: 'View steps and output' })
  expect(screen.getByRole('button', { name: 'Update in progress' })).toBeDisabled()
  await user.click(screen.getByRole('button', { name: 'Close' }))
  expect(onClose).toHaveBeenCalledOnce()
  expect(mock.stackAction).not.toHaveBeenCalled()
  expect(mock.cancelCommand).not.toHaveBeenCalled()
})
