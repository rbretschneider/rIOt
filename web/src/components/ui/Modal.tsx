import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type MouseEvent as ReactMouseEvent,
  type ReactNode,
  type RefObject,
} from 'react'
import { createPortal } from 'react-dom'
import { focusRing } from './focusRing'

export type ModalSize = 'sm' | 'md' | 'lg' | 'xl'

export interface ModalProps {
  open: boolean
  onClose: () => void
  title: ReactNode
  description?: ReactNode
  children?: ReactNode
  footer?: ReactNode
  size?: ModalSize
  /** Blocks Escape, backdrop and close-button dismissal while an action is in flight. */
  pending?: boolean
  dismissOnBackdrop?: boolean
  showCloseButton?: boolean
  closeLabel?: string
  /** Element to focus on open. Defaults to the first control in the body, then the footer. */
  initialFocusRef?: RefObject<HTMLElement | null>
  panelClassName?: string
  bodyClassName?: string
}

const SIZES: Record<ModalSize, string> = {
  sm: 'max-w-sm',
  md: 'max-w-md',
  lg: 'max-w-lg',
  xl: 'max-w-3xl',
}

const FOCUSABLE =
  'a[href],button:not([disabled]),input:not([disabled]):not([type=hidden]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

// Open modals, oldest first. Only the last one reacts to the keyboard, so Escape
// closes the topmost dialog and a ConfirmDialog stacked on another modal traps focus alone.
const openModals: string[] = []

function focusablesIn(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
    el => !el.closest('[hidden],[aria-hidden="true"]'),
  )
}

function CloseIcon() {
  return (
    <svg aria-hidden="true" viewBox="0 0 20 20" fill="currentColor" className="h-5 w-5">
      <path d="M5.28 4.22a.75.75 0 0 0-1.06 1.06L8.94 10l-4.72 4.72a.75.75 0 1 0 1.06 1.06L10 11.06l4.72 4.72a.75.75 0 1 0 1.06-1.06L11.06 10l4.72-4.72a.75.75 0 0 0-1.06-1.06L10 8.94 5.28 4.22Z" />
    </svg>
  )
}

/**
 * Accessible modal dialog: portal to body, role=dialog + aria-modal named by its
 * title, focus trap, Escape (unless pending), backdrop click (press and release
 * both on the backdrop), and focus restored to the opener on close.
 */
export function Modal(props: ModalProps) {
  if (!props.open) return null
  return createPortal(<OpenModal {...props} />, document.body)
}

function OpenModal({
  onClose,
  title,
  description,
  children,
  footer,
  size = 'md',
  pending = false,
  dismissOnBackdrop = true,
  showCloseButton = true,
  closeLabel = 'Close',
  initialFocusRef,
  panelClassName = '',
  bodyClassName = '',
}: ModalProps) {
  const modalId = useId()
  const titleId = `${modalId}-title`
  const descriptionId = `${modalId}-description`
  const panelRef = useRef<HTMLDivElement>(null)
  const bodyRef = useRef<HTMLDivElement>(null)
  const footerRef = useRef<HTMLDivElement>(null)
  const closeRef = useRef<HTMLButtonElement>(null)
  const pressedOnBackdrop = useRef(false)

  // Captured during the first render, before the dialog is in the DOM, so it is the opener.
  const [opener] = useState<Element | null>(() => document.activeElement)

  const onCloseRef = useRef(onClose)
  const pendingRef = useRef(pending)
  onCloseRef.current = onClose
  pendingRef.current = pending

  useEffect(() => {
    openModals.push(modalId)

    function onKeyDown(e: KeyboardEvent) {
      if (openModals[openModals.length - 1] !== modalId) return
      if (e.key === 'Escape') {
        if (pendingRef.current || e.isComposing) return
        e.preventDefault()
        onCloseRef.current()
        return
      }
      if (e.key !== 'Tab') return
      const panel = panelRef.current
      if (!panel) return
      const items = focusablesIn(panel)
      if (items.length === 0) {
        e.preventDefault()
        panel.focus()
        return
      }
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      const outside = !active || !panel.contains(active) || active === panel
      if (e.shiftKey) {
        if (active === first || outside) {
          e.preventDefault()
          last.focus()
        }
      } else if (active === last || (outside && active !== panel)) {
        e.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      const at = openModals.lastIndexOf(modalId)
      if (at !== -1) openModals.splice(at, 1)
    }
  }, [modalId])

  useLayoutEffect(() => {
    const panel = panelRef.current
    if (!panel) return
    const target =
      initialFocusRef?.current ??
      (bodyRef.current && focusablesIn(bodyRef.current)[0]) ??
      (footerRef.current && focusablesIn(footerRef.current)[0]) ??
      closeRef.current ??
      panel
    target.focus()

    return () => {
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus()
    }
    // Initial focus runs once per open; later prop changes must not steal focus.
  }, [])

  function handleBackdropMouseDown(e: ReactMouseEvent<HTMLDivElement>) {
    pressedOnBackdrop.current = e.target === e.currentTarget
  }

  function handleBackdropClick(e: ReactMouseEvent<HTMLDivElement>) {
    const startedOnBackdrop = pressedOnBackdrop.current
    pressedOnBackdrop.current = false
    if (e.target !== e.currentTarget || !startedOnBackdrop) return
    if (pending || !dismissOnBackdrop) return
    onClose()
  }

  return (
    <div
      data-testid="modal-backdrop"
      className="fixed inset-0 z-(--z-modal) flex items-center justify-center bg-black/60 p-4"
      onMouseDown={handleBackdropMouseDown}
      onClick={handleBackdropClick}
    >
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description != null ? descriptionId : undefined}
        tabIndex={-1}
        className={`flex max-h-[calc(100vh-2rem)] w-full flex-col rounded-lg border border-border-strong bg-raised shadow-2xl shadow-black/50 focus:outline-none ${SIZES[size]} ${panelClassName}`.trim()}
      >
        <div className="flex items-start justify-between gap-4 px-6 pb-2 pt-5">
          <h2 id={titleId} className="min-w-0 text-lg font-semibold text-text wrap-anywhere">
            {title}
          </h2>
          {showCloseButton && (
            <button
              ref={closeRef}
              type="button"
              aria-label={closeLabel}
              disabled={pending}
              onClick={onClose}
              className={`-mr-2 -mt-1 shrink-0 rounded-md p-1.5 text-text-muted transition-colors hover:bg-raised-2 hover:text-text disabled:cursor-not-allowed disabled:opacity-50 ${focusRing}`}
            >
              <CloseIcon />
            </button>
          )}
        </div>
        {description != null && (
          <div id={descriptionId} className="px-6 pb-2 text-sm text-text-muted break-words">
            {description}
          </div>
        )}
        {children != null && (
          <div
            ref={bodyRef}
            className={`scrollbar-thin min-h-0 flex-1 overflow-y-auto px-6 py-2 break-words ${bodyClassName}`.trim()}
          >
            {children}
          </div>
        )}
        {footer != null && (
          <div
            ref={footerRef}
            className="flex flex-wrap items-center justify-end gap-3 px-6 pb-5 pt-4"
          >
            {footer}
          </div>
        )}
        {/* Without a footer the body would end flush with the panel's rounded edge. */}
        {footer == null && <div className="pb-3" />}
      </div>
    </div>
  )
}
