import { useEffect, useRef, useState, type ReactNode } from 'react'
import { errorMessage } from '../../api/errors'
import { Button } from './Button'
import { Modal } from './Modal'
import { useToast, type ToastInput } from './Toast'

export interface ConfirmDialogProps<T> {
  open: boolean
  title: string
  message: ReactNode
  confirmLabel: string
  pendingLabel?: string
  cancelLabel?: string
  variant?: 'primary' | 'danger'
  /** The action. May be async; a rejection keeps the dialog open with an inline error. */
  onConfirm: () => T | Promise<T>
  /** Cancel, Escape, backdrop (all ignored while pending) and after success. */
  onClose: () => void
  onSuccess?: (result: T) => void
  successToast?: string | ToastInput | ((result: T) => string | ToastInput)
  children?: ReactNode
}

/**
 * Confirmation dialog with an async lifecycle: Idle, Pending (spinner, both buttons
 * disabled, Escape and backdrop ignored), then Closed with an optional success toast,
 * or Error (inline role=alert, stays open for retry or cancel).
 *
 * Initial focus is always Cancel, never Confirm (SEC-001), so the keypress or click
 * that opened the dialog, including key auto-repeat, can never also confirm it.
 */
export function ConfirmDialog<T>(props: ConfirmDialogProps<T>) {
  if (!props.open) return null
  return <OpenConfirmDialog {...props} />
}

function OpenConfirmDialog<T>({
  title,
  message,
  confirmLabel,
  pendingLabel = 'Working…',
  cancelLabel = 'Cancel',
  variant = 'primary',
  onConfirm,
  onClose,
  onSuccess,
  successToast,
  children,
}: ConfirmDialogProps<T>) {
  const toast = useToast()
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const inFlight = useRef(false)
  const mounted = useRef(false)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  // After a failed attempt the focused button was disabled while pending, so focus
  // would be lost to <body>. Put it back on Cancel (never on Confirm).
  useEffect(() => {
    if (error !== null) cancelRef.current?.focus()
  }, [error])

  async function handleConfirm() {
    if (inFlight.current) return
    inFlight.current = true
    setError(null)
    setPending(true)

    let result: T
    try {
      result = await onConfirm()
    } catch (e) {
      inFlight.current = false
      const text = errorMessage(e)
      if (mounted.current) {
        setPending(false)
        setError(text)
      } else {
        // The dialog is gone (navigation), so there is no inline slot left: report the failure once as a toast.
        toast.error('Action failed', `${title.replace(/\?+$/, '')}: ${text}`)
      }
      return
    }

    inFlight.current = false
    const spec = typeof successToast === 'function' ? successToast(result) : successToast
    if (spec) {
      if (typeof spec === 'string') toast.success(spec)
      else toast.show({ tone: spec.tone ?? 'success', title: spec.title, message: spec.message })
    }
    if (!mounted.current) return
    setPending(false)
    onSuccess?.(result)
    onClose()
  }

  return (
    <Modal
      open
      size="sm"
      title={title}
      description={message}
      pending={pending}
      showCloseButton={false}
      initialFocusRef={cancelRef}
      onClose={onClose}
      footer={
        <>
          <Button ref={cancelRef} variant="secondary" disabled={pending} onClick={onClose}>
            {cancelLabel}
          </Button>
          <Button
            variant={variant}
            pending={pending}
            pendingLabel={pendingLabel}
            onClick={() => void handleConfirm()}
          >
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
      {error !== null && (
        <p
          role="alert"
          className="mt-2 rounded-md border border-danger-hover/30 bg-danger-hover/10 px-3 py-2 text-sm text-danger-fg break-words"
        >
          {error}
        </p>
      )}
    </Modal>
  )
}
