import { useRef, useState } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Modal, type ModalProps } from './Modal'

function Harness(props: Partial<ModalProps> & { startOpen?: boolean; onCloseSpy?: () => void }) {
  const { startOpen = false, onCloseSpy, ...modalProps } = props
  const [open, setOpen] = useState(startOpen)
  return (
    <div>
      <button onClick={() => setOpen(true)}>Open</button>
      <Modal
        title="Edit rule"
        {...modalProps}
        open={open}
        onClose={() => {
          onCloseSpy?.()
          setOpen(false)
        }}
      >
        {modalProps.children ?? (
          <>
            <input aria-label="Name" />
            <button>Inside</button>
          </>
        )}
      </Modal>
    </div>
  )
}

describe('[UIF AC-022] dialog semantics', () => {
  it('exposes role=dialog named by its title with aria-modal', () => {
    render(<Harness startOpen />)
    const dialog = screen.getByRole('dialog', { name: 'Edit rule' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
  })

  it('renders the title as a level-2 heading wired through aria-labelledby', () => {
    render(<Harness startOpen />)
    const dialog = screen.getByRole('dialog')
    const heading = within(dialog).getByRole('heading', { name: 'Edit rule', level: 2 })
    expect(dialog).toHaveAttribute('aria-labelledby', heading.id)
  })

  it('wires the description through aria-describedby', () => {
    render(<Harness startOpen description="This cannot be undone" />)
    const dialog = screen.getByRole('dialog')
    expect(dialog).toHaveAccessibleDescription('This cannot be undone')
  })

  it('renders nothing when closed', () => {
    render(<Harness />)
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('portals to document.body so ancestors cannot clip it', () => {
    const { container } = render(<Harness startOpen />)
    expect(container.querySelector('[role="dialog"]')).toBeNull()
    expect(document.body.querySelector('[role="dialog"]')).not.toBeNull()
  })

  it('takes its layer from the shared z-index scale, not a hard-coded value', () => {
    render(<Harness startOpen />)
    const backdrop = screen.getByTestId('modal-backdrop')
    expect(backdrop.className).toContain('z-(--z-modal)')
    expect(backdrop.className).not.toMatch(/\bz-(40|50|\[60\])\b/)
  })
})

describe('[UIF AC-023] initial focus inside', () => {
  it('moves focus into the dialog when opened from a button', async () => {
    render(<Harness />)
    await userEvent.click(screen.getByRole('button', { name: 'Open' }))
    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
  })

  it('focuses the first control in the body ahead of the close button', async () => {
    render(<Harness />)
    await userEvent.click(screen.getByRole('button', { name: 'Open' }))
    expect(screen.getByRole('textbox', { name: 'Name' })).toHaveFocus()
  })

  it('honours initialFocusRef', () => {
    function WithRef() {
      const ref = useRef<HTMLButtonElement>(null)
      return (
        <Modal open onClose={() => {}} title="T" initialFocusRef={ref}>
          <input aria-label="First" />
          <button ref={ref}>Target</button>
        </Modal>
      )
    }
    render(<WithRef />)
    expect(screen.getByRole('button', { name: 'Target' })).toHaveFocus()
  })

  it('falls back to the footer, then the close button, then the panel', () => {
    const { unmount } = render(
      <Modal open onClose={() => {}} title="T" footer={<button>Cancel</button>}>
        <p>text only</p>
      </Modal>,
    )
    expect(screen.getByRole('button', { name: 'Cancel' })).toHaveFocus()
    unmount()

    const second = render(<Modal open onClose={() => {}} title="T"><p>text only</p></Modal>)
    expect(screen.getByRole('button', { name: 'Close' })).toHaveFocus()
    second.unmount()

    render(<Modal open onClose={() => {}} title="T" showCloseButton={false}><p>text only</p></Modal>)
    expect(screen.getByRole('dialog')).toHaveFocus()
  })
})

describe('[UIF AC-024] Tab wraps forward', () => {
  it('moves focus from the last control to the first', async () => {
    render(<Harness startOpen />)
    const dialog = screen.getByRole('dialog')
    const last = within(dialog).getByRole('button', { name: 'Inside' })
    last.focus()
    await userEvent.tab()
    expect(within(dialog).getByRole('button', { name: 'Close' })).toHaveFocus()
  })

  it('cancels the native Tab so focus cannot leave the dialog', () => {
    render(<Harness startOpen />)
    const last = within(screen.getByRole('dialog')).getByRole('button', { name: 'Inside' })
    last.focus()
    const notPrevented = fireEvent.keyDown(last, { key: 'Tab' })
    expect(notPrevented, 'Tab on the last element must be default-prevented').toBe(false)
  })

  it('pulls focus back in when it has escaped to the page', () => {
    render(<Harness startOpen />)
    ;(screen.getByRole('button', { name: 'Open' })).focus()
    fireEvent.keyDown(document.activeElement ?? document.body, { key: 'Tab' })
    expect(screen.getByRole('dialog').contains(document.activeElement)).toBe(true)
  })

  it('holds focus on the panel when the dialog has nothing focusable', () => {
    render(<Modal open onClose={() => {}} title="T" showCloseButton={false}><p>text</p></Modal>)
    const dialog = screen.getByRole('dialog')
    fireEvent.keyDown(dialog, { key: 'Tab' })
    expect(dialog).toHaveFocus()
  })
})

describe('[UIF AC-025] Shift+Tab wraps backward', () => {
  it('moves focus from the first control to the last', async () => {
    render(<Harness startOpen />)
    const dialog = screen.getByRole('dialog')
    within(dialog).getByRole('button', { name: 'Close' }).focus()
    await userEvent.tab({ shift: true })
    expect(within(dialog).getByRole('button', { name: 'Inside' })).toHaveFocus()
  })

  it('skips disabled and aria-hidden controls when computing the ends', () => {
    render(
      <Modal open onClose={() => {}} title="T" showCloseButton={false}>
        <button>First</button>
        <button>Middle</button>
        <button disabled>Disabled</button>
        <div aria-hidden="true"><button>Hidden</button></div>
      </Modal>,
    )
    const first = screen.getByRole('button', { name: 'First' })
    first.focus()
    fireEvent.keyDown(first, { key: 'Tab', shiftKey: true })
    expect(screen.getByRole('button', { name: 'Middle' })).toHaveFocus()
  })
})

describe('[UIF AC-026] Escape closes', () => {
  it('calls onClose once on Escape', async () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen onCloseSpy={onCloseSpy} />)
    await userEvent.keyboard('{Escape}')
    expect(onCloseSpy).toHaveBeenCalledOnce()
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('ignores Escape while pending', async () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen pending onCloseSpy={onCloseSpy} />)
    await userEvent.keyboard('{Escape}')
    expect(onCloseSpy).not.toHaveBeenCalled()
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('stops listening once closed', async () => {
    const onClose = vi.fn()
    const { rerender } = render(<Modal open onClose={onClose} title="T">x</Modal>)
    rerender(<Modal open={false} onClose={onClose} title="T">x</Modal>)
    await userEvent.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()
  })
})

describe('[UIF AC-027] focus restored to opener', () => {
  it('returns focus to the Open button after Escape', async () => {
    render(<Harness />)
    const opener = screen.getByRole('button', { name: 'Open' })
    await userEvent.click(opener)
    await userEvent.keyboard('{Escape}')
    expect(opener).toHaveFocus()
  })

  it('returns focus after the close button', async () => {
    render(<Harness />)
    const opener = screen.getByRole('button', { name: 'Open' })
    await userEvent.click(opener)
    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(opener).toHaveFocus()
  })

  it('does not throw when the opener left the document', () => {
    function Gone() {
      const [open, setOpen] = useState(false)
      const [showOpener, setShowOpener] = useState(true)
      return (
        <>
          {showOpener && <button onClick={() => { setOpen(true); setShowOpener(false) }}>Open</button>}
          <button onClick={() => setOpen(false)}>Outside</button>
          <Modal open={open} onClose={() => setOpen(false)} title="T">x</Modal>
        </>
      )
    }
    render(<Gone />)
    fireEvent.click(screen.getByRole('button', { name: 'Open' }))
    expect(() => fireEvent.keyDown(document, { key: 'Escape' })).not.toThrow()
    expect(screen.queryByRole('dialog')).toBeNull()
  })
})

describe('[UIF AC-028] backdrop closes, panel does not', () => {
  it('does not close when the panel is clicked', async () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen onCloseSpy={onCloseSpy} />)
    await userEvent.click(screen.getByRole('dialog'))
    expect(onCloseSpy).not.toHaveBeenCalled()
  })

  it('closes once when the backdrop is clicked', async () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen onCloseSpy={onCloseSpy} />)
    await userEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onCloseSpy).toHaveBeenCalledOnce()
  })

  it('does not close when a drag starts in the panel and ends on the backdrop', () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen onCloseSpy={onCloseSpy} />)
    const backdrop = screen.getByTestId('modal-backdrop')
    fireEvent.mouseDown(screen.getByRole('dialog'))
    fireEvent.click(backdrop)
    expect(onCloseSpy).not.toHaveBeenCalled()
  })

  it('does not close on a bare click without a preceding press on the backdrop', () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen onCloseSpy={onCloseSpy} />)
    fireEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onCloseSpy).not.toHaveBeenCalled()
  })

  it('ignores the backdrop while pending', async () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen pending onCloseSpy={onCloseSpy} />)
    await userEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onCloseSpy).not.toHaveBeenCalled()
  })

  it('ignores the backdrop when dismissOnBackdrop is false', async () => {
    const onCloseSpy = vi.fn()
    render(<Harness startOpen dismissOnBackdrop={false} onCloseSpy={onCloseSpy} />)
    await userEvent.click(screen.getByTestId('modal-backdrop'))
    expect(onCloseSpy).not.toHaveBeenCalled()
  })
})

describe('[UIF AC-012] focus-visible ring on the close control', () => {
  it('carries the shared focus-visible ring', () => {
    render(<Harness startOpen />)
    expect(screen.getByRole('button', { name: 'Close' }).className).toContain('focus-visible:ring-2')
  })

  it('is disabled while pending and absent when showCloseButton is false', () => {
    const { unmount } = render(<Harness startOpen pending />)
    expect(screen.getByRole('button', { name: 'Close' })).toBeDisabled()
    unmount()
    render(<Harness startOpen showCloseButton={false} />)
    expect(screen.queryByRole('button', { name: 'Close' })).toBeNull()
  })

  it('honours a custom closeLabel', () => {
    render(<Harness startOpen closeLabel="Dismiss dialog" />)
    expect(screen.getByRole('button', { name: 'Dismiss dialog' })).toBeInTheDocument()
  })
})

describe('[UIF AC-029] stacked modals', () => {
  function Stack({ onInnerClose, onOuterClose }: { onInnerClose: () => void; onOuterClose: () => void }) {
    return (
      <>
        <Modal open onClose={onOuterClose} title="Outer"><button>Outer action</button></Modal>
        <Modal open onClose={onInnerClose} title="Inner"><button>Inner action</button></Modal>
      </>
    )
  }

  it('Escape closes only the topmost modal', async () => {
    const onInnerClose = vi.fn()
    const onOuterClose = vi.fn()
    render(<Stack onInnerClose={onInnerClose} onOuterClose={onOuterClose} />)
    await userEvent.keyboard('{Escape}')
    expect(onInnerClose).toHaveBeenCalledOnce()
    expect(onOuterClose).not.toHaveBeenCalled()
  })

  it('hands the keyboard back to the lower modal once the top one closes', async () => {
    const onOuterClose = vi.fn()
    function Toggle() {
      const [inner, setInner] = useState(true)
      return (
        <>
          <Modal open onClose={onOuterClose} title="Outer">x</Modal>
          <Modal open={inner} onClose={() => setInner(false)} title="Inner">y</Modal>
        </>
      )
    }
    render(<Toggle />)
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: 'Inner' })).toBeNull()
    expect(onOuterClose).not.toHaveBeenCalled()
    await userEvent.keyboard('{Escape}')
    expect(onOuterClose).toHaveBeenCalledOnce()
  })

  it('traps Tab inside the topmost modal only', () => {
    render(<Stack onInnerClose={() => {}} onOuterClose={() => {}} />)
    const inner = screen.getByRole('dialog', { name: 'Inner' })
    const innerLast = within(inner).getByRole('button', { name: 'Inner action' })
    innerLast.focus()
    fireEvent.keyDown(innerLast, { key: 'Tab' })
    expect(inner.contains(document.activeElement)).toBe(true)
  })
})

describe('[UIF SEC-005] long unbroken text cannot push the footer out of reach', () => {
  it('wraps title, description and body, and keeps the footer outside the scrolling body', () => {
    const long = 'x'.repeat(5000)
    render(
      <Modal open onClose={() => {}} title={long} description={long} footer={<button>Confirm</button>}>
        <p>{long}</p>
      </Modal>,
    )
    const dialog = screen.getByRole('dialog')
    expect(within(dialog).getByRole('heading').className).toMatch(/wrap-anywhere|break-words/)
    expect(dialog.className).toContain('max-h-[calc(100vh-2rem)]')
    const footerButton = within(dialog).getByRole('button', { name: 'Confirm' })
    const scrollingBody = dialog.querySelector('.overflow-y-auto')
    expect(scrollingBody).not.toBeNull()
    expect(scrollingBody?.className).toContain('break-words')
    expect(scrollingBody?.contains(footerButton), 'footer must not live inside the scrolling body').toBe(false)
  })
})
