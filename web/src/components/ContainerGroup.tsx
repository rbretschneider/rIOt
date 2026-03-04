import type { ContainerInfo } from '../types/models'
import type { ContainerGroup as ContainerGroupType } from '../utils/docker'
import ContainerCard from './ContainerCard'

interface Props {
  group: ContainerGroupType
  onContainerClick: (c: ContainerInfo) => void
}

export default function ContainerGroup({ group, onContainerClick }: Props) {
  return (
    <div>
      <div className="flex items-center gap-2 mb-3">
        {group.icon && <span className="text-base">{group.icon}</span>}
        <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider">{group.name}</h3>
        <span className="text-xs text-gray-600">({group.containers.length})</span>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
        {group.containers.map(c => (
          <ContainerCard key={c.id} container={c} onClick={onContainerClick} />
        ))}
      </div>
    </div>
  )
}
