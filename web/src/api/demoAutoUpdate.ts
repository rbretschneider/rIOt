import type { AutoUpdatePolicy, FleetAutoUpdatePolicy } from '../types/models'

// The server's container policy rule, for the static demo: a container with
// both Compose labels is covered by an enabled stack policy for its project;
// otherwise an enabled container policy under its exact name applies.
export function demoAutoUpdateState(
  policies: AutoUpdatePolicy[],
  name: string,
  labels: Record<string, string> | undefined,
): FleetAutoUpdatePolicy {
  const own = policies.find((p) => p.target === name)
  const project = labels?.['com.docker.compose.project'] ?? ''
  const workDir = labels?.['com.docker.compose.project.working_dir'] ?? ''
  const stack = policies.find((p) => p.target === project)
  return {
    target: name,
    enabled: !!own && !own.is_stack && own.enabled,
    ...(project && workDir && stack?.is_stack && stack.enabled
      ? { covered_by: project }
      : {}),
  }
}
