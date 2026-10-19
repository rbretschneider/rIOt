import { useState } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ConfirmDialog, type ConfirmDialogProps } from './ConfirmDialog'
import { Modal } from './Modal'
import { ToastProvider, useToast } from './Toast'

type Props = Partial<ConfirmDialogProps<unknown>>

function Harness(props: Props) {
  const [open, setOpen] = useState(true)
  return (
    <ToastProvider>
      <button onClick={() => setOpen(true)}>Open</button>
      <ConfirmDialog
        title="Delete probe"
        message="Remove nas-ping?"
        confirmLabel="Delete"
        onConfirm={() => undefined}
        {...props}
        open={open}
        onClose={() => {
          props.onClose?.()
          setOpen(false)
        }}
      />
    </ToastProvider>
  )
}

function deferred<T = void>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const dialog = () => screen.getByRole('dialog')
const confirmButton = (name = 'Delete') => within(dialog()).getByRole('button', { name })
const cancelButton = () => within(dialog()).getByRole('button', { name: 'Cancel' })

describe('[UIF AC-032] pending visuals', () => {
  it('shows a spinner and the pending label and disables both buttons', async () => {
    const pending = deferred()
    render(<Harness onConfirm={() => pending.promise} pendingLabel="Deleting…" />)
    await userEvent.click(confirmButton())

    const busy = within(dialog()).getByRole('button', { name: 'Deleting…' })
    expect(busy).toBeDisabled()
    expect(busy).toHaveAttribute('aria-busy', 'true')
    expect(busy.querySelector('svg.animate-spin')).not.toBeNull()
    expect(cancelButton()).toBeDisabled()
    await act(async () => pending.resolve())
  })

  it('defaults the pending label to "Working…"', async () => {
    const pending = deferred()
    render(<Harness onConfirm={() => pending.promise} />)
    await userEvent.click(confirmButton())
    expect(within(dialog()).getByRole('button', { name: 'Working…' })).toBeInTheDocument()
    await act(async () => pending.resolve())
  })

  it('hides the close X (a ConfirmDialog is dismissed with Cancel)', () => {
    render(<Harness />)
    expect(within(dialog()).queryByRole('button', { name: 'Close' })).toBeNull()
  })
})

describe('[UIF AC-033] Escape ignored while pending', () => {
  it('keeps the dialog open on Escape and calls onClose zero times', async () => {
    const pending = deferred()
    const onClose = vi.fn()
    render(<Harness onConfirm={() => pending.promise} onClose={onClose} />)
    await userEvent.click(confirmButton())
    await userEvent.keyboard('{Escape}')
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
    await act(async () => pending.resolve())
  })

  it('closes on Escape when idle', async () => {
    const onClose = vi.fn()
    render(<Harness onClose={onClose} />)
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()
  })
})

describe('[UIF AC-034] backdrop ignored while pending', () => {
  it('keeps the dialog open when the backdrop is clicked', async () => {
    const pending = deferred()
    const onClose = vi.fn()
    render(<Harness onConfirm={() => pending.promise} onClose={onClose} />)
    await userEvent.click(confirmButton())
    await userEvent.click(screen.getByTestId('modal-backdrop'))
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    expect(onClose).not.toHaveBeenCalled()
    await act(async () => pending.resolve())
  })
})

describe('[UIF AC-035] no double invoke', () => {
  it('calls the action exactly once when confirm is clicked twice', async () => {
    const pending = deferred()
    const onConfirm = vi.fn(() => pending.promise)
    render(<Harness onConfirm={onConfirm} />)
    const button = confirmButton()
    await userEvent.click(button)
    await userEvent.click(button)
    expect(onConfirm).toHaveBeenCalledTimes(1)
    await act(async () => pending.resolve())
  })

  it('calls the action once even for two synchronous clicks before React re-renders', () => {
    const pending = deferred()
    const onConfirm = vi.fn(() => pending.promise)
    render(<Harness onConfirm={onConfirm} />)
    const button = confirmButton()
    fireEvent.click(button)
    fireEvent.click(button)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })
})

describe('[UIF AC-036] success closes with a toast', () => {
  it('removes the dialog and raises the success toast "Probe deleted"', async () => {
    render(<Harness onConfirm={() => Promise.resolve()} successToast="Probe deleted" />)
    await userEvent.click(confirmButton())
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    const region = screen.getByRole('region', { name: 'Notifications' })
    expect(within(region).getByText('Probe deleted')).toBeInTheDocument()
  })

  it('builds the toast from the result when successToast is a function', async () => {
    render(
      <Harness
        onConfirm={() => Promise.resolve({ sent: 2 })}
        successToast={(r: unknown) => ({ title: 'Update sent', message: `${(r as { sent: number }).sent} sent` })}
      />,
    )
    await userEvent.click(confirmButton())
    const region = screen.getByRole('region', { name: 'Notifications' })
    expect(await within(region).findByText('Update sent')).toBeInTheDocument()
    expect(within(region).getByText('2 sent')).toBeInTheDocument()
  })

  it('passes the result to onSuccess and then calls onClose', async () => {
    const order: string[] = []
    render(
      <Harness
        onConfirm={() => Promise.resolve('result')}
        onSuccess={r => order.push(`success:${String(r)}`)}
        onClose={() => order.push('close')}
      />,
    )
    await userEvent.click(confirmButton())
    await waitFor(() => expect(order).toEqual(['success:result', 'close']))
  })

  it('works with a synchronous action', async () => {
    const onConfirm = vi.fn()
    render(<Harness onConfirm={onConfirm} successToast="Done it" />)
    await userEvent.click(confirmButton())
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(onConfirm).toHaveBeenCalledOnce()
  })

  it('raises no toast when none is configured', async () => {
    render(<Harness onConfirm={() => Promise.resolve()} />)
    await userEvent.click(confirmButton())
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull())
    expect(screen.getByRole('region', { name: 'Notifications' }).querySelectorAll('li')).toHaveLength(0)
  })
})

describe('[UIF AC-037] failure stays open with inline error', () => {
  it('shows the message in a role=alert inside the dialog and re-enables the buttons', async () => {
    render(<Harness onConfirm={() => Promise.reject(new Error('HTTP 500: Internal Server Error'))} />)
    await userEvent.click(confirmButton())
    const alert = await within(dialog()).findByRole('alert')
    expect(alert).toHaveTextContent('HTTP 500: Internal Server Error')
    expect(confirmButton()).toBeEnabled()
    expect(cancelButton()).toBeEnabled()
    expect(confirmButton()).not.toHaveAttribute('aria-busy')
  })

  it('does not raise a toast for an error shown inline (reported exactly once)', async () => {
    render(<Harness onConfirm={() => Promise.reject(new Error('boom'))} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')
    expect(within(screen.getByRole('region', { name: 'Notifications' })).queryByRole('alert')).toBeNull()
  })

  it('reports a synchronous throw the same way', async () => {
    render(<Harness onConfirm={() => { throw new Error('sync failure') }} />)
    await userEvent.click(confirmButton())
    expect(await within(dialog()).findByRole('alert')).toHaveTextContent('sync failure')
  })

  it('does not call onSuccess or onClose on failure', async () => {
    const onSuccess = vi.fn()
    const onClose = vi.fn()
    render(<Harness onConfirm={() => Promise.reject(new Error('x'))} onSuccess={onSuccess} onClose={onClose} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')
    expect(onSuccess).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })

  it('returns focus to Cancel after a failure, never to Confirm', async () => {
    render(<Harness onConfirm={() => Promise.reject(new Error('x'))} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')
    expect(cancelButton()).toHaveFocus()
  })
})

describe('[UIF AC-038] message-less rejection', () => {
  it.each([
    ['undefined', undefined],
    ['an empty Error', new Error('')],
    ['an object without a message', { code: 7 }],
  ])('shows the generic text for %s', async (_label, reason) => {
    render(<Harness onConfirm={() => Promise.reject(reason)} />)
    await userEvent.click(confirmButton())
    expect(await within(dialog()).findByRole('alert')).toHaveTextContent('Something went wrong. Please try again.')
  })
})

describe('[UIF AC-039] retry after failure', () => {
  it('clears the inline error and invokes the action a second time', async () => {
    const second = deferred()
    const onConfirm = vi
      .fn<() => Promise<unknown>>()
      .mockRejectedValueOnce(new Error('first failure'))
      .mockReturnValueOnce(second.promise)
    render(<Harness onConfirm={onConfirm} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')

    await userEvent.click(confirmButton())

    expect(onConfirm).toHaveBeenCalledTimes(2)
    expect(within(dialog()).queryByRole('alert')).toBeNull()
    expect(within(dialog()).getByRole('button', { name: 'Working…' })).toBeDisabled()
    await act(async () => second.resolve())
  })
})

describe('[UIF AC-040] cancel after failure', () => {
  it('closes the dialog without invoking the action again', async () => {
    const onConfirm = vi.fn(() => Promise.reject(new Error('x')))
    render(<Harness onConfirm={onConfirm} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')
    await userEvent.click(cancelButton())
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('starts the next opening in the idle state with no stale error', async () => {
    render(<Harness onConfirm={() => Promise.reject(new Error('stale'))} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')
    await userEvent.click(cancelButton())
    await userEvent.click(screen.getByRole('button', { name: 'Open' }))
    expect(within(dialog()).queryByRole('alert')).toBeNull()
    expect(confirmButton()).toBeEnabled()
  })
})

describe('[UIF AC-041] danger variant and initial focus', () => {
  it('renders a danger Confirm button and focuses Cancel', () => {
    render(<Harness variant="danger" />)
    expect(confirmButton()).toHaveAttribute('data-variant', 'danger')
    expect(cancelButton()).toHaveFocus()
  })

  it('renders a primary Confirm button for the primary variant', () => {
    render(<Harness variant="primary" />)
    expect(confirmButton()).toHaveAttribute('data-variant', 'primary')
  })

  it('names the dialog by its title and describes it with the message', () => {
    render(<Harness />)
    expect(screen.getByRole('dialog', { name: 'Delete probe' })).toHaveAccessibleDescription('Remove nas-ping?')
  })
})

describe('[UIF SEC-001] the opening gesture can never also confirm', () => {
  it.each(['primary', 'danger'] as const)('%s dialogs never focus Confirm on open', variant => {
    render(<Harness variant={variant} />)
    expect(confirmButton()).not.toHaveFocus()
    expect(cancelButton()).toHaveFocus()
  })

  it('a held Enter after opening by keyboard activates Cancel at worst, never Confirm', async () => {
    const onConfirm = vi.fn()
    function Trigger() {
      const [open, setOpen] = useState(false)
      return (
        <ToastProvider>
          <button onClick={() => setOpen(true)}>Update all</button>
          <ConfirmDialog
            open={open}
            title="Update all"
            message="Send update to every agent?"
            confirmLabel="Update all agents"
            variant="primary"
            onConfirm={onConfirm}
            onClose={() => setOpen(false)}
          />
        </ToastProvider>
      )
    }
    render(<Trigger />)
    const trigger = screen.getByRole('button', { name: 'Update all' })
    trigger.focus()

    const user = userEvent.setup()
    // Enter opens the dialog; the held key then auto-repeats three more times.
    await user.keyboard('{Enter>4/}')

    expect(onConfirm, 'repeat keystrokes must not reach the Confirm button').not.toHaveBeenCalled()
  })

  it('a repeated Enter keydown on the freshly focused control does not trigger Confirm', () => {
    const onConfirm = vi.fn()
    render(<Harness variant="primary" onConfirm={onConfirm} />)
    const focused = document.activeElement as HTMLElement
    for (let i = 0; i < 3; i++) fireEvent.keyDown(focused, { key: 'Enter', repeat: true })
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('a quick double click on the opener does not confirm', async () => {
    const onConfirm = vi.fn()
    function Trigger() {
      const [open, setOpen] = useState(false)
      return (
        <ToastProvider>
          <button onClick={() => setOpen(true)}>Open</button>
          <ConfirmDialog open={open} title="T" message="m" confirmLabel="Go" onConfirm={onConfirm} onClose={() => setOpen(false)} />
        </ToastProvider>
      )
    }
    render(<Trigger />)
    await userEvent.dblClick(screen.getByRole('button', { name: 'Open' }))
    expect(onConfirm).not.toHaveBeenCalled()
  })
})

describe('[UIF SEC-003] a rejection after unmount is reported once as an error toast', () => {
  it('raises a single persistent error toast when the dialog unmounted while pending', async () => {
    const pending = deferred()
    function Host() {
      const [show, setShow] = useState(true)
      return (
        <ToastProvider>
          <button onClick={() => setShow(false)}>Navigate away</button>
          {show && (
            <ConfirmDialog
              open
              title="Reboot web-01?"
              message="m"
              confirmLabel="Reboot"
              variant="danger"
              onConfirm={() => pending.promise}
              onClose={() => {}}
            />
          )}
        </ToastProvider>
      )
    }
    render(<Host />)
    await userEvent.click(confirmButton('Reboot'))
    act(() => screen.getByRole('button', { name: 'Navigate away', hidden: true }).click())
    expect(screen.queryByRole('dialog')).toBeNull()

    await act(async () => pending.reject(new Error('HTTP 502: Bad Gateway')))

    const alerts = within(screen.getByRole('region', { name: 'Notifications' })).getAllByRole('alert')
    expect(alerts).toHaveLength(1)
    expect(alerts[0]).toHaveTextContent('Action failed')
    expect(alerts[0]).toHaveTextContent('Reboot web-01: HTTP 502: Bad Gateway')
  })

  it('does not toast when the failure was shown inline (dialog still mounted)', async () => {
    render(<Harness onConfirm={() => Promise.reject(new Error('inline only'))} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')
    expect(within(screen.getByRole('region', { name: 'Notifications' })).queryAllByRole('alert')).toHaveLength(0)
  })

  it('still raises the success toast, but not onSuccess or onClose, when unmounted before resolving', async () => {
    const pending = deferred<string>()
    const onSuccess = vi.fn()
    const onClose = vi.fn()
    function Host() {
      const [show, setShow] = useState(true)
      return (
        <ToastProvider>
          <button onClick={() => setShow(false)}>Navigate away</button>
          {show && (
            <ConfirmDialog
              open
              title="T"
              message="m"
              confirmLabel="Go"
              onConfirm={() => pending.promise}
              onSuccess={onSuccess}
              onClose={onClose}
              successToast="It worked"
            />
          )}
        </ToastProvider>
      )
    }
    render(<Host />)
    await userEvent.click(confirmButton('Go'))
    act(() => screen.getByRole('button', { name: 'Navigate away', hidden: true }).click())
    await act(async () => pending.resolve('ok'))
    expect(within(screen.getByRole('region', { name: 'Notifications' })).getByText('It worked')).toBeInTheDocument()
    expect(onSuccess).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()
  })
})

describe('[UIF AC-029] stacked Escape closes only the top dialog', () => {
  it('closes the ConfirmDialog and leaves the underlying Modal open', async () => {
    const onModalClose = vi.fn()
    function Stack() {
      const [confirmOpen, setConfirmOpen] = useState(true)
      return (
        <ToastProvider>
          <Modal open onClose={onModalClose} title="Edit rule">
            <button>Edit action</button>
          </Modal>
          <ConfirmDialog
            open={confirmOpen}
            title="Delete rule"
            message="Sure?"
            confirmLabel="Delete"
            variant="danger"
            onConfirm={() => undefined}
            onClose={() => setConfirmOpen(false)}
          />
        </ToastProvider>
      )
    }
    render(<Stack />)
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Delete rule' })).toBeNull()
    expect(screen.getByRole('dialog', { name: 'Edit rule' })).toBeInTheDocument()
    expect(onModalClose).not.toHaveBeenCalled()
  })

  it('returns focus to the control inside the underlying modal after the ConfirmDialog closes', async () => {
    function Stack() {
      const [confirmOpen, setConfirmOpen] = useState(false)
      return (
        <ToastProvider>
          <Modal open onClose={() => {}} title="Edit rule">
            <button onClick={() => setConfirmOpen(true)}>Delete this rule</button>
          </Modal>
          <ConfirmDialog
            open={confirmOpen}
            title="Delete rule"
            message="Sure?"
            confirmLabel="Delete"
            variant="danger"
            onConfirm={() => undefined}
            onClose={() => setConfirmOpen(false)}
          />
        </ToastProvider>
      )
    }
    render(<Stack />)
    const opener = screen.getByRole('button', { name: 'Delete this rule' })
    await userEvent.click(opener)
    await userEvent.keyboard('{Escape}')
    expect(opener).toHaveFocus()
  })
})

describe('[UIF SEC-005] long unbroken messages cannot hide the footer buttons', () => {
  it('wraps the message and the inline error and keeps Cancel and Confirm outside the scrolling body', async () => {
    const long = 'h'.repeat(5000)
    render(
      <Harness
        variant="danger"
        message={`Reboot ${long}?`}
        onConfirm={() => Promise.reject(new Error(long))}
      />,
    )
    await userEvent.click(confirmButton())
    const alert = await within(dialog()).findByRole('alert')
    expect(alert.className).toContain('break-words')
    const body = dialog().querySelector('.overflow-y-auto')
    expect(body?.contains(confirmButton())).toBe(false)
    expect(body?.contains(cancelButton())).toBe(false)
  })
})

// Added by QA Engineer
// Covers AC-035: the synchronous in-flight guard itself. The older double-click tests also pass
// without the guard because the Confirm button is disabled by the time the second click lands.
// Here both activations are queued inside one act(), so React has not re-rendered between them.
describe('[UIF AC-035] QA: the in-flight ref guard, not just the disabled button', () => {
  it('calls the action once when two activations land before React re-renders', async () => {
    const pending = deferred()
    const onConfirm = vi.fn(() => pending.promise)
    render(<Harness onConfirm={onConfirm} />)
    const button = confirmButton()

    await act(async () => {
      button.click()
      button.click()
    })

    expect(onConfirm).toHaveBeenCalledTimes(1)
    await act(async () => pending.resolve())
  })

  it('allows exactly one more invocation after a failure (guard is released, not stuck)', async () => {
    const onConfirm = vi
      .fn<() => Promise<unknown>>()
      .mockRejectedValueOnce(new Error('first'))
      .mockRejectedValueOnce(new Error('second'))
    render(<Harness onConfirm={onConfirm} />)

    await act(async () => {
      confirmButton().click()
      confirmButton().click()
    })
    await within(dialog()).findByRole('alert')
    expect(onConfirm).toHaveBeenCalledTimes(1)

    await userEvent.click(confirmButton())
    await waitFor(() => expect(within(dialog()).getByRole('alert')).toHaveTextContent('second'))
    expect(onConfirm).toHaveBeenCalledTimes(2)
  })
})

// Covers AC-033: Escape while pending, wherever the event is dispatched.
describe('[UIF AC-033] QA: Escape while pending, from anywhere', () => {
  it('ignores Escape dispatched on document.body, on the panel and on the pending button', async () => {
    const pending = deferred()
    const onClose = vi.fn()
    render(<Harness onConfirm={() => pending.promise} onClose={onClose} />)
    await userEvent.click(confirmButton())
    const busy = within(dialog()).getByRole('button', { name: 'Working…' })

    fireEvent.keyDown(document.body, { key: 'Escape' })
    fireEvent.keyDown(dialog(), { key: 'Escape' })
    fireEvent.keyDown(busy, { key: 'Escape' })

    expect(onClose).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await act(async () => pending.resolve())
  })

  it('honours Escape again once a failed attempt has settled', async () => {
    const onClose = vi.fn()
    render(<Harness onConfirm={() => Promise.reject(new Error('x'))} onClose={onClose} />)
    await userEvent.click(confirmButton())
    await within(dialog()).findByRole('alert')
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

// Covers AC-051, AC-055: a toast raised while a modal is open (the toast layer sits above the modal layer).
describe('[UIF AC-051] QA: a toast raised over an open dialog', () => {
  function RaiseToast() {
    const toast = useToast()
    return <button onClick={() => toast.error('Background failure', 'HTTP 500: Internal Server Error')}>raise toast</button>
  }

  function Stage() {
    const [open, setOpen] = useState(true)
    return (
      <ToastProvider>
        <RaiseToast />
        <ConfirmDialog
          open={open}
          title="Delete probe"
          message="Remove nas-ping?"
          confirmLabel="Delete"
          variant="danger"
          onConfirm={() => undefined}
          onClose={() => setOpen(false)}
        />
      </ToastProvider>
    )
  }

  const raise = () => act(() => screen.getByRole('button', { name: 'raise toast', hidden: true }).click())

  it('shows the toast outside the dialog without closing it or moving focus off Cancel', () => {
    render(<Stage />)
    raise()

    const region = screen.getByRole('region', { name: 'Notifications' })
    expect(within(region).getByRole('alert')).toHaveTextContent('HTTP 500: Internal Server Error')
    expect(dialog().contains(region)).toBe(false)
    expect(screen.getByRole('dialog', { name: 'Delete probe' })).toBeInTheDocument()
    expect(cancelButton()).toHaveFocus()
  })

  it('Escape closes the dialog but leaves the persistent error toast in place', async () => {
    render(<Stage />)
    raise()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(within(screen.getByRole('region', { name: 'Notifications' })).getByRole('alert')).toBeInTheDocument()
  })

  it('keeps Tab inside the dialog even while a toast with a Dismiss button is on screen', async () => {
    render(<Stage />)
    raise()
    const user = userEvent.setup()
    for (let i = 0; i < 4; i++) {
      await user.tab()
      expect(dialog().contains(document.activeElement)).toBe(true)
    }
  })
})
