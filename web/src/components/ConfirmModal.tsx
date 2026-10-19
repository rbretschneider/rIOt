import { useRef } from 'react'
import { Button, Modal } from './ui'

interface Props {
  title: string
  message: string
  confirmLabel?: string
  confirmVariant?: 'danger' | 'primary'
  onConfirm: () => void
  onCancel: () => void
}

/**
 * Synchronous confirmation (the caller closes it). For actions that need a pending
 * state and inline errors, use ConfirmDialog instead. Initial focus is Cancel so the
 * gesture that opened the dialog can never also confirm it.
 */
export default function ConfirmModal({ title, message, confirmLabel = 'Confirm', confirmVariant = 'danger', onConfirm, onCancel }: Props) {
  const cancelRef = useRef<HTMLButtonElement>(null)
  return (
    <Modal
      open
      size="sm"
      title={title}
      description={message}
      showCloseButton={false}
      initialFocusRef={cancelRef}
      onClose={onCancel}
      footer={
        <>
          <Button ref={cancelRef} variant="secondary" onClick={onCancel}>
            Cancel
          </Button>
          <Button variant={confirmVariant} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    />
  )
}
