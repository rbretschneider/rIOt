import { statusBgColor } from '../utils/docker'

export default function ContainerStatusBadge({ state }: { state: string }) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium border ${statusBgColor(state)}`}>
      {state}
    </span>
  )
}
