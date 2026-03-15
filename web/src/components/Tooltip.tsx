import { useState, cloneElement, type ReactElement } from 'react'
import {
  useFloating,
  autoUpdate,
  offset,
  flip,
  shift,
  useHover,
  useClick,
  useFocus,
  useDismiss,
  useInteractions,
  useRole,
  FloatingPortal,
  type Placement,
} from '@floating-ui/react'

interface Props {
  content: string | null | undefined
  children: ReactElement<Record<string, unknown>>
  placement?: Placement
}

export default function Tooltip({ content, children, placement = 'top' }: Props) {
  const [open, setOpen] = useState(false)

  const { refs, floatingStyles, context } = useFloating({
    open,
    onOpenChange: setOpen,
    placement,
    whileElementsMounted: autoUpdate,
    middleware: [offset(6), flip(), shift({ padding: 8 })],
  })

  const hover = useHover(context, { move: false })
  const click = useClick(context)
  const focus = useFocus(context)
  const dismiss = useDismiss(context)
  const role = useRole(context, { role: 'tooltip' })

  const { getReferenceProps, getFloatingProps } = useInteractions([hover, click, focus, dismiss, role])

  if (!content) return children

  const childProps = children.props as Record<string, unknown>

  return (
    <>
      {cloneElement(children, getReferenceProps({ ref: refs.setReference, ...childProps }))}
      {open && (
        <FloatingPortal>
          <div
            ref={refs.setFloating}
            style={floatingStyles}
            {...getFloatingProps()}
            className="z-[100] max-w-xs px-2.5 py-1.5 text-xs text-gray-200 bg-gray-800 border border-gray-700 rounded-md shadow-lg"
          >
            {content}
          </div>
        </FloatingPortal>
      )}
    </>
  )
}
