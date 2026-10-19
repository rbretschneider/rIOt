import { useRef } from 'react'
import type { ContainerInfo } from '../types/models'
import { displayName } from '../utils/docker'
import { Button, Modal } from './ui'

interface Props {
  title: string
  containers: ContainerInfo[]
  networkOrder?: { parent: string; dependents: string[] }
  onConfirm: () => void
  onCancel: () => void
}

export default function UpdateConfirmModal({ title, containers, networkOrder, onConfirm, onCancel }: Props) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  return (
    <Modal
      open
      size="md"
      title={title}
      showCloseButton={false}
      initialFocusRef={cancelRef}
      onClose={onCancel}
      footer={
        <>
          <Button ref={cancelRef} variant="secondary" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant="primary" onClick={onConfirm}>
            Confirm Update
          </Button>
        </>
      }
    >
      <div className="mb-4 max-h-60 space-y-1.5 overflow-y-auto">
        {containers.map(c => (
          <div key={c.id} className="flex items-center gap-2 text-sm">
            <span className="h-1.5 w-1.5 flex-shrink-0 rounded-full bg-warn-fg" />
            <span className="text-text">{displayName(c.riot, c.name)}</span>
            <span className="truncate font-mono text-xs text-text-muted">{c.image}</span>
          </div>
        ))}
      </div>

      {networkOrder && (
        <div className="mb-4 space-y-1 rounded border border-info/30 bg-info/10 px-3 py-2 text-xs text-info-fg">
          <p className="font-medium">Execution order:</p>
          <ol className="list-inside list-decimal space-y-0.5">
            {networkOrder.dependents.length > 0 && (
              <li>Stop dependents: {networkOrder.dependents.join(', ')}</li>
            )}
            <li>Update parent: {networkOrder.parent}</li>
            {networkOrder.dependents.length > 0 && (
              <li>Update &amp; restart dependents: {networkOrder.dependents.join(', ')}</li>
            )}
          </ol>
        </div>
      )}

      <p className="text-xs text-warn-fg">Running services will be briefly interrupted.</p>
    </Modal>
  )
}
