interface ContainerActionState {
  state: string
  health_status?: string
  update_status?: string
  update_available?: boolean | null
}
export function blockingCommandId(error: unknown): string | undefined {
  return error instanceof Error &&
    'existingCommandId' in error &&
    typeof error.existingCommandId === 'string'
    ? error.existingCommandId
    : undefined
}
export function containerPrimaryAction(
  container: ContainerActionState,
  active = false,
): 'start' | 'update' | 'restart' | undefined {
  if (active) return
  if (['exited', 'created', 'dead'].includes(container.state)) return 'start'
  const update = container.update_status
    ? container.update_status === 'update_available'
    : container.update_available === true
  if (container.state === 'running' && update) return 'update'
  if (container.state === 'running' && container.health_status === 'unhealthy')
    return 'restart'
}
export function bulkApplicability(
  action: string,
  container: ContainerActionState,
  active = false,
) {
  if (active) return false
  switch (action) {
    case 'start':
      return ['exited', 'created', 'dead'].includes(container.state)
    case 'stop':
      return ['running', 'restarting', 'paused'].includes(container.state)
    case 'restart':
      return container.state === 'running'
    case 'update':
      return container.update_status
        ? container.update_status === 'update_available'
        : container.update_available === true
    default:
      return false
  }
}
export function stackPrimaryAction(
  stack: {
    state: string
    running: number
    total: number
    updates: number
    services?: { health_status?: string }[]
  },
  active = false,
): 'up' | 'update' | 'restart' | undefined {
  if (active) return
  if (
    stack.state === 'down' ||
    stack.running === 0 ||
    stack.running < stack.total
  )
    return 'up'
  if (stack.updates > 0) return 'update'
  if (stack.services?.some((service) => service.health_status === 'unhealthy'))
    return 'restart'
}
