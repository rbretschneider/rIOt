import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type FocusEvent,
  type ReactNode,
} from 'react'
import { focusRing } from './focusRing'

export type ToastTone = 'success' | 'info' | 'error'

export interface ToastInput {
  tone?: ToastTone
  title: string
  message?: string
}

interface ToastRecord {
  id: number
  tone: ToastTone
  title: string
  message?: string
}

export interface ToastApi {
  show: (toast: ToastInput & { tone: ToastTone }) => number
  success: (title: string, message?: string) => number
  info: (title: string, message?: string) => number
  error: (title: string, message?: string) => number
  dismiss: (id: number) => void
}

const DEFAULT_TITLES: Record<ToastTone, string> = {
  success: 'Done',
  info: 'Notice',
  error: 'Something went wrong',
}

const ToastContext = createContext<ToastApi | null>(null)

/** Raises toasts from anywhere under ToastProvider. Throws if the provider is missing. */
export function useToast(): ToastApi {
  const api = useContext(ToastContext)
  if (!api) throw new Error('useToast must be used within ToastProvider')
  return api
}

interface ToastProviderProps {
  children: ReactNode
  /** Auto-dismiss delay for success and info toasts. Errors never auto-dismiss. */
  duration?: number
  /** Maximum visible toasts; raising one more dismisses the oldest. */
  max?: number
  /** Injected clock, used to track the time left when a toast is paused. */
  now?: () => number
}

const defaultNow = () => Date.now()

export function ToastProvider({ children, duration = 5000, max = 4, now = defaultNow }: ToastProviderProps) {
  const [toasts, setToasts] = useState<ToastRecord[]>([])
  const nextId = useRef(1)

  const dismiss = useCallback((id: number) => {
    setToasts(current => current.filter(t => t.id !== id))
  }, [])

  const show = useCallback(
    (input: ToastInput & { tone: ToastTone }) => {
      const id = nextId.current++
      const record: ToastRecord = {
        id,
        tone: input.tone,
        title: input.title || DEFAULT_TITLES[input.tone],
        message: input.message,
      }
      // Newest first; slicing from the front drops the oldest when the cap is hit.
      setToasts(current => [record, ...current].slice(0, max))
      return id
    },
    [max],
  )

  const api = useMemo<ToastApi>(
    () => ({
      show,
      dismiss,
      success: (title, message) => show({ tone: 'success', title, message }),
      info: (title, message) => show({ tone: 'info', title, message }),
      error: (title, message) => show({ tone: 'error', title, message }),
    }),
    [show, dismiss],
  )

  return (
    <ToastContext.Provider value={api}>
      {children}
      {/*
        The region ignores pointer events so its empty area never blocks clicks on the
        page underneath; each toast turns them back on. It sits on --z-toast, above
        modals, so an error raised during a dialog is still visible. A toast is outside
        an aria-modal dialog, so it cannot be reached with Tab while a dialog is open.
      */}
      <div
        role="region"
        aria-label="Notifications"
        className="pointer-events-none fixed bottom-4 right-4 z-(--z-toast) w-80 max-w-[calc(100vw-2rem)]"
      >
        <ol aria-live="polite" className="flex flex-col gap-2">
          {toasts.map(toast => (
            <ToastItem key={toast.id} toast={toast} duration={duration} now={now} onDismiss={dismiss} />
          ))}
        </ol>
      </div>
    </ToastContext.Provider>
  )
}

const TONE_STYLES: Record<ToastTone, { border: string; icon: string }> = {
  success: { border: 'border-l-success', icon: 'text-success-fg' },
  info: { border: 'border-l-info', icon: 'text-info-fg' },
  error: { border: 'border-l-danger', icon: 'text-danger-fg' },
}

function ToneIcon({ tone }: { tone: ToastTone }) {
  const path =
    tone === 'success'
      ? 'M10 18a8 8 0 1 0 0-16 8 8 0 0 0 0 16Zm3.857-9.809a.75.75 0 0 0-1.214-.882l-3.483 4.79-1.88-1.88a.75.75 0 1 0-1.06 1.061l2.5 2.5a.75.75 0 0 0 1.137-.089l4-5.5Z'
      : tone === 'info'
        ? 'M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Zm-7-4a1 1 0 1 1-2 0 1 1 0 0 1 2 0ZM9 9a.75.75 0 0 0 0 1.5h.253a.25.25 0 0 1 .244.304l-.459 2.066A1.75 1.75 0 0 0 10.747 15H11a.75.75 0 0 0 0-1.5h-.253a.25.25 0 0 1-.244-.304l.459-2.066A1.75 1.75 0 0 0 9.253 9H9Z'
        : 'M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Zm-8-5a.75.75 0 0 1 .75.75v4.5a.75.75 0 0 1-1.5 0v-4.5A.75.75 0 0 1 10 5Zm0 10a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z'
  return (
    <svg aria-hidden="true" viewBox="0 0 20 20" fill="currentColor" className={`mt-0.5 h-5 w-5 shrink-0 ${TONE_STYLES[tone].icon}`}>
      <path fillRule="evenodd" clipRule="evenodd" d={path} />
    </svg>
  )
}

interface ToastItemProps {
  toast: ToastRecord
  duration: number
  now: () => number
  onDismiss: (id: number) => void
}

function ToastItem({ toast, duration, now, onDismiss }: ToastItemProps) {
  const [hovered, setHovered] = useState(false)
  const [focusWithin, setFocusWithin] = useState(false)
  const paused = hovered || focusWithin
  const remaining = useRef(duration)
  const { id, tone } = toast

  // Success and info count down; the effect re-arms with whatever time is left each
  // time the pause state flips. Cleanup subtracts the time that actually elapsed.
  useEffect(() => {
    if (tone === 'error' || paused) return
    const startedAt = now()
    const timer = setTimeout(() => onDismiss(id), remaining.current)
    return () => {
      clearTimeout(timer)
      remaining.current = Math.max(0, remaining.current - (now() - startedAt))
    }
  }, [tone, paused, id, now, onDismiss])

  function handleBlur(e: FocusEvent<HTMLLIElement>) {
    if (!e.currentTarget.contains(e.relatedTarget)) setFocusWithin(false)
  }

  const styles = TONE_STYLES[tone]
  return (
    <li
      role={tone === 'error' ? 'alert' : undefined}
      data-tone={tone}
      onMouseEnter={() => setHovered(true)}
      onMouseLeave={() => setHovered(false)}
      onFocus={() => setFocusWithin(true)}
      onBlur={handleBlur}
      className={`pointer-events-auto flex items-start gap-3 rounded-lg border border-l-4 border-border-strong ${styles.border} bg-raised p-3 shadow-lg shadow-black/40 motion-safe:animate-toast-in`}
    >
      <ToneIcon tone={tone} />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-text wrap-anywhere">{toast.title}</p>
        {toast.message && <p className="mt-0.5 text-xs text-text-muted wrap-anywhere">{toast.message}</p>}
      </div>
      <button
        type="button"
        aria-label="Dismiss notification"
        onClick={() => onDismiss(id)}
        className={`-m-1 shrink-0 rounded-md p-1 text-text-muted transition-colors hover:bg-raised-2 hover:text-text ${focusRing}`}
      >
        <svg aria-hidden="true" viewBox="0 0 20 20" fill="currentColor" className="h-4 w-4">
          <path d="M5.28 4.22a.75.75 0 0 0-1.06 1.06L8.94 10l-4.72 4.72a.75.75 0 1 0 1.06 1.06L10 11.06l4.72 4.72a.75.75 0 1 0 1.06-1.06L11.06 10l4.72-4.72a.75.75 0 0 0-1.06-1.06L10 8.94 5.28 4.22Z" />
        </svg>
      </button>
    </li>
  )
}
