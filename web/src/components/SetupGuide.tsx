import { useState, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'

const INSTALL_SCRIPT_URL = 'https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh'

function buildCommand(serverUrl: string, fingerprint?: string, regKey?: string): string {
  const parts = [
    `curl -sSL ${INSTALL_SCRIPT_URL} |`,
    'sudo bash -s --',
    serverUrl,
  ]
  if (fingerprint) parts.push(`--fingerprint ${fingerprint}`)
  if (regKey) parts.push(`--key ${regKey}`)
  return parts.join(' ')
}

interface Props {
  inline?: boolean
  onClose?: () => void
}

export default function SetupGuide({ inline, onClose }: Props) {
  const { data: certInfo } = useQuery({
    queryKey: ['server-cert'],
    queryFn: () => fetch('/api/v1/server-cert', { credentials: 'same-origin' }).then(r => r.json()),
    staleTime: 60 * 1000,
  })

  const [regKey, setRegKey] = useState('')
  useEffect(() => {
    fetch('/api/v1/settings/registration', { credentials: 'same-origin' })
      .then(res => res.json())
      .then(data => setRegKey(data.registration_key || ''))
      .catch(() => {})
  }, [])

  const [copied, setCopied] = useState(false)
  const serverUrl = window.location.origin
  const fingerprint = certInfo?.fingerprint
  const command = buildCommand(serverUrl, fingerprint, regKey)

  function handleCopy() {
    navigator.clipboard.writeText(command)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const content = (
    <div className="space-y-5">
      {/* Install Command */}
      <div>
        <h4 className="text-sm font-medium text-white mb-2">Install Command</h4>
        <p className="text-xs text-gray-500 mb-2">Run this on the target device to install and register the agent:</p>
        <div className="relative bg-gray-800 rounded-lg p-4 group">
          <code className="text-xs text-emerald-400 break-all select-all leading-relaxed">{command}</code>
          <button
            onClick={handleCopy}
            className="absolute top-2 right-2 px-2 py-1 text-xs rounded bg-gray-700 text-gray-300 hover:bg-gray-600 hover:text-white transition-colors"
          >
            {copied ? 'Copied!' : 'Copy'}
          </button>
        </div>
      </div>

      {/* What this does */}
      <div>
        <h4 className="text-sm font-medium text-white mb-2">What this does</h4>
        <ul className="text-xs text-gray-400 space-y-1 list-disc list-inside">
          <li>Downloads and installs the rIOt agent binary</li>
          <li>Creates a systemd service for automatic startup</li>
          <li>Registers the device with this server</li>
          <li>Pins the server TLS certificate via TOFU (trust on first use)</li>
        </ul>
      </div>

      {/* Requirements */}
      <div>
        <h4 className="text-sm font-medium text-white mb-2">Requirements</h4>
        <p className="text-xs text-gray-400">Linux with systemd, root/sudo access, and curl installed.</p>
      </div>

      {/* Flags reference */}
      <div>
        <h4 className="text-sm font-medium text-white mb-2">Flags Reference</h4>
        <div className="text-xs">
          <table className="w-full">
            <tbody className="divide-y divide-gray-800">
              <tr>
                <td className="py-1.5 pr-4 font-mono text-gray-300 whitespace-nowrap">--fingerprint</td>
                <td className="py-1.5 text-gray-500">Pin server cert on install (skips TOFU prompt)</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-4 font-mono text-gray-300 whitespace-nowrap">--key</td>
                <td className="py-1.5 text-gray-500">Registration key (if server requires one)</td>
              </tr>
              <tr>
                <td className="py-1.5 pr-4 font-mono text-gray-300 whitespace-nowrap">--version</td>
                <td className="py-1.5 text-gray-500">Install a specific agent version (default: latest)</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </div>
  )

  if (inline) {
    return (
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-6">
        <h3 className="text-base font-semibold text-white mb-4">Add Your First Device</h3>
        {content}
      </div>
    )
  }

  // Modal
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]" onClick={onClose}>
      <div className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg mx-4 p-6" onClick={e => e.stopPropagation()}>
        <div className="flex items-center justify-between mb-4">
          <h3 className="text-lg font-semibold text-white">Add Device</h3>
          <button onClick={onClose} className="text-gray-500 hover:text-white transition-colors">
            <svg xmlns="http://www.w3.org/2000/svg" className="h-5 w-5" viewBox="0 0 20 20" fill="currentColor">
              <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
            </svg>
          </button>
        </div>
        {content}
      </div>
    </div>
  )
}
