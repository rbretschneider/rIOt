import type { PendingUpdate } from '../types/models'

/**
 * Visually distinct badge for reboot-class pending updates (PATCH-GATE
 * FR-030): violet for GPU driver packages, amber for kernel packages.
 * Standard packages render nothing.
 */
export default function RebootClassBadge({ cls }: { cls?: PendingUpdate['class'] }) {
  if (cls === 'gpu_driver') {
    return (
      <span
        className="ml-2 px-1.5 py-0.5 rounded text-[10px] font-medium bg-violet-500/15 text-violet-300"
        title="GPU driver package — held outside maintenance windows when hold enforcement is on"
      >
        GPU driver
      </span>
    )
  }
  if (cls === 'kernel') {
    return (
      <span
        className="ml-2 px-1.5 py-0.5 rounded text-[10px] font-medium bg-amber-500/15 text-amber-300"
        title="Kernel package — held outside maintenance windows when hold enforcement is on"
      >
        kernel
      </span>
    )
  }
  return null
}
