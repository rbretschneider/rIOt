import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import RebootClassBadge from './RebootClassBadge'

// [AC-020] Reboot-class pending updates carry a distinct badge in the UI.
describe('[AC-020] RebootClassBadge classifies reboot-class packages', () => {
  it('renders a "GPU driver" badge for gpu_driver packages', () => {
    render(<RebootClassBadge cls="gpu_driver" />)
    expect(screen.getByText('GPU driver')).toBeInTheDocument()
  })

  it('renders a "kernel" badge for kernel packages', () => {
    render(<RebootClassBadge cls="kernel" />)
    expect(screen.getByText('kernel')).toBeInTheDocument()
  })

  it('renders nothing for a standard (unclassified) package', () => {
    const { container } = render(<RebootClassBadge cls={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('gives GPU-driver and kernel badges visually distinct styling', () => {
    const { rerender, container } = render(<RebootClassBadge cls="gpu_driver" />)
    const gpuClass = container.querySelector('span')?.className ?? ''
    rerender(<RebootClassBadge cls="kernel" />)
    const kernelClass = container.querySelector('span')?.className ?? ''
    expect(gpuClass).not.toBe(kernelClass)
    expect(gpuClass).toContain('violet')
    expect(kernelClass).toContain('amber')
  })
})
