import { useEffect, useRef } from 'react'
import { Terminal as XTerm } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

interface Props {
  deviceId: string
  containerId: string
  onClose?: () => void
}

export default function Terminal({ deviceId, containerId, onClose }: Props) {
  const termRef = useRef<HTMLDivElement>(null)
  const xtermRef = useRef<XTerm | null>(null)
  const wsRef = useRef<WebSocket | null>(null)

  useEffect(() => {
    if (!termRef.current) return

    const term = new XTerm({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#111827',
        foreground: '#e5e7eb',
        cursor: '#e5e7eb',
        selectionBackground: '#374151',
      },
    })
    const fitAddon = new FitAddon()
    term.loadAddon(fitAddon)
    term.open(termRef.current)
    fitAddon.fit()
    xtermRef.current = term

    // Connect WebSocket
    const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${proto}//${window.location.host}/ws/terminal/${deviceId}/${containerId}`
    const ws = new WebSocket(wsUrl)
    wsRef.current = ws

    ws.onopen = () => {
      term.writeln('\x1b[32mConnected to container...\x1b[0m')
      // Send initial resize
      const dims = JSON.stringify({ cols: term.cols, rows: term.rows })
      ws.send(new Blob([dims]))
    }

    ws.onmessage = (evt) => {
      term.write(evt.data)
    }

    ws.onclose = () => {
      term.writeln('\r\n\x1b[31mConnection closed.\x1b[0m')
    }

    ws.onerror = () => {
      term.writeln('\r\n\x1b[31mConnection error.\x1b[0m')
    }

    // Stdin: terminal → WebSocket as text
    term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(data)
      }
    })

    // Resize: send binary message
    term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        const msg = JSON.stringify({ cols, rows })
        ws.send(new Blob([msg]))
      }
    })

    // Handle window resize
    const handleResize = () => fitAddon.fit()
    window.addEventListener('resize', handleResize)

    return () => {
      window.removeEventListener('resize', handleResize)
      ws.close()
      term.dispose()
    }
  }, [deviceId, containerId])

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-3 py-2 bg-gray-800 border-b border-gray-700">
        <span className="text-xs text-gray-400 font-mono">Terminal</span>
        {onClose && (
          <button onClick={onClose} className="text-gray-500 hover:text-white text-sm">&times;</button>
        )}
      </div>
      <div ref={termRef} className="flex-1 min-h-[300px]" />
    </div>
  )
}
