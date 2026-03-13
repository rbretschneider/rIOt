import { useState, useEffect, useCallback } from 'react'
import { useQuery, useMutation } from '@tanstack/react-query'
import { settingsApi } from '../api/settings'

const INSTALL_SCRIPT_URL = 'https://raw.githubusercontent.com/rbretschneider/rIOt/main/scripts/install.sh'
const RELEASES_URL = 'https://github.com/rbretschneider/rIOt/releases/latest'

type OS = 'linux' | 'macos' | 'windows'

const OS_LABELS: Record<OS, string> = {
  linux: 'Linux',
  macos: 'macOS',
  windows: 'Windows',
}

function buildCommand(serverUrl: string, opts: { fingerprint?: string; regKey?: string; bootstrapKey?: string }): string {
  const parts = [
    `curl -sSL ${INSTALL_SCRIPT_URL} |`,
    'sudo bash -s --',
    serverUrl,
  ]
  if (opts.bootstrapKey) parts.push(`--bootstrap-key ${opts.bootstrapKey}`)
  if (opts.fingerprint) parts.push(`--fingerprint ${opts.fingerprint}`)
  if (opts.regKey) parts.push(`--key ${opts.regKey}`)
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

  // Detect mTLS: if /api/v1/ca.pem returns 200, mTLS is enabled
  const { data: mtlsEnabled } = useQuery({
    queryKey: ['mtls-check'],
    queryFn: async () => {
      try {
        const r = await fetch('/api/v1/ca.pem', { credentials: 'same-origin' })
        return r.ok
      } catch { return false }
    },
    staleTime: 60 * 1000,
  })

  const [regKey, setRegKey] = useState('')
  useEffect(() => {
    fetch('/api/v1/settings/registration', { credentials: 'same-origin' })
      .then(res => res.json())
      .then(data => setRegKey(data.registration_key || ''))
      .catch(() => {})
  }, [])

  const [tab, setTab] = useState<OS>('linux')
  const [copied, setCopied] = useState(false)
  const serverUrl = window.location.origin
  const fingerprint = certInfo?.fingerprint

  // Auto-generate bootstrap key when mTLS is enabled
  const [bootstrapKey, setBootstrapKey] = useState<string | null>(null)
  const [keyError, setKeyError] = useState('')

  const generateKeyMut = useMutation({
    mutationFn: () => settingsApi.createBootstrapKey({ label: 'Add Device', expires_in_hours: 24 }),
    onSuccess: (data) => {
      setBootstrapKey(data.key)
      setKeyError('')
    },
    onError: () => setKeyError('Failed to generate bootstrap key'),
  })

  // Auto-generate key when modal opens and mTLS is enabled
  useEffect(() => {
    if (mtlsEnabled && !bootstrapKey && !generateKeyMut.isPending) {
      generateKeyMut.mutate()
    }
  }, [mtlsEnabled]) // eslint-disable-line react-hooks/exhaustive-deps

  function handleCopy(text: string) {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const linuxCommand = buildCommand(serverUrl, {
    fingerprint,
    regKey,
    bootstrapKey: mtlsEnabled ? (bootstrapKey ?? undefined) : undefined,
  })

  const macConfigYaml = [
    `server:`,
    `  url: "${serverUrl}"`,
    fingerprint ? `  server_cert_pin: "${fingerprint}"` : '',
    regKey ? `  api_key: "${regKey}"` : '',
    mtlsEnabled && bootstrapKey ? `  bootstrap_key: "${bootstrapKey}"` : '',
  ].filter(Boolean).join('\n')

  const content = (
    <div className="space-y-5">
      {/* mTLS notice */}
      {mtlsEnabled && (
        <div className="bg-blue-900/20 border border-blue-800/40 rounded-lg px-4 py-3">
          <p className="text-xs text-blue-300">
            <span className="font-medium">mTLS is enabled.</span>{' '}
            A one-time bootstrap key has been generated and included in the install command below. Each device needs its own key — click "New Key" to generate another.
          </p>
          {keyError && <p className="text-xs text-red-400 mt-1">{keyError}</p>}
          <button
            onClick={() => { setBootstrapKey(null); generateKeyMut.mutate() }}
            disabled={generateKeyMut.isPending}
            className="mt-2 px-2 py-1 text-xs bg-blue-800/40 text-blue-300 hover:text-white rounded transition-colors"
          >
            {generateKeyMut.isPending ? 'Generating...' : 'New Key'}
          </button>
        </div>
      )}

      {/* OS Tabs */}
      <div className="flex gap-1 bg-gray-800/50 rounded-lg p-1">
        {(Object.keys(OS_LABELS) as OS[]).map(os => (
          <button
            key={os}
            onClick={() => { setTab(os); setCopied(false) }}
            className={`flex-1 px-3 py-1.5 text-xs font-medium rounded-md transition-colors ${
              tab === os
                ? 'bg-gray-700 text-white'
                : 'text-gray-400 hover:text-gray-200'
            }`}
          >
            {OS_LABELS[os]}
          </button>
        ))}
      </div>

      {/* Linux */}
      {tab === 'linux' && (
        <>
          <div>
            <h4 className="text-sm font-medium text-white mb-2">Install Command</h4>
            <p className="text-xs text-gray-500 mb-2">Run this on the target device to install and register the agent:</p>
            {mtlsEnabled && !bootstrapKey && generateKeyMut.isPending ? (
              <div className="bg-gray-800 rounded-lg p-4 text-xs text-gray-500">Generating bootstrap key...</div>
            ) : (
              <div className="relative bg-gray-800 rounded-lg p-4 group">
                <code className="text-xs text-emerald-400 break-all select-all leading-relaxed">{linuxCommand}</code>
                <button
                  onClick={() => handleCopy(linuxCommand)}
                  className="absolute top-2 right-2 px-2 py-1 text-xs rounded bg-gray-700 text-gray-300 hover:bg-gray-600 hover:text-white transition-colors"
                >
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
            )}
          </div>
          <div>
            <h4 className="text-sm font-medium text-white mb-2">What this does</h4>
            <ul className="text-xs text-gray-400 space-y-1 list-disc list-inside">
              <li>Downloads and installs the rIOt agent binary</li>
              <li>Creates a systemd service for automatic startup</li>
              {mtlsEnabled && <li>Enrolls the device with a client certificate (mTLS)</li>}
              <li>Registers the device with this server</li>
              <li>Pins the server TLS certificate via TOFU (trust on first use)</li>
            </ul>
          </div>
          <div>
            <h4 className="text-sm font-medium text-white mb-2">Requirements</h4>
            <p className="text-xs text-gray-400">Linux with systemd, root/sudo access, and curl installed.</p>
          </div>
        </>
      )}

      {/* macOS */}
      {tab === 'macos' && (
        <>
          <div>
            <h4 className="text-sm font-medium text-white mb-2">Install Command</h4>
            <p className="text-xs text-gray-500 mb-2">The install script works on macOS too — it downloads the correct binary and prints manual run instructions:</p>
            {mtlsEnabled && !bootstrapKey && generateKeyMut.isPending ? (
              <div className="bg-gray-800 rounded-lg p-4 text-xs text-gray-500">Generating bootstrap key...</div>
            ) : (
              <div className="relative bg-gray-800 rounded-lg p-4 group">
                <code className="text-xs text-emerald-400 break-all select-all leading-relaxed">{linuxCommand}</code>
                <button
                  onClick={() => handleCopy(linuxCommand)}
                  className="absolute top-2 right-2 px-2 py-1 text-xs rounded bg-gray-700 text-gray-300 hover:bg-gray-600 hover:text-white transition-colors"
                >
                  {copied ? 'Copied!' : 'Copy'}
                </button>
              </div>
            )}
          </div>
          <div>
            <h4 className="text-sm font-medium text-white mb-2">What this does</h4>
            <ul className="text-xs text-gray-400 space-y-1 list-disc list-inside">
              <li>Detects Intel or Apple Silicon architecture</li>
              <li>Downloads the correct agent binary to <code className="text-gray-300">/usr/local/bin</code></li>
              <li>Writes a default config to <code className="text-gray-300">/etc/riot/agent.yaml</code></li>
              {mtlsEnabled && <li>Enrolls the device with a client certificate (mTLS)</li>}
              <li>Prints instructions for running the agent manually or as a launchd service</li>
            </ul>
          </div>
          <div>
            <h4 className="text-sm font-medium text-white mb-2">Requirements</h4>
            <p className="text-xs text-gray-400">macOS with curl installed (included by default). Root/sudo access for installation.</p>
          </div>
        </>
      )}

      {/* Windows */}
      {tab === 'windows' && (
        <>
          <div>
            <h4 className="text-sm font-medium text-white mb-1">1. Download the agent</h4>
            <p className="text-xs text-gray-500 mb-2">
              Download <code className="text-gray-300">riot-agent-windows-amd64.exe</code> from the{' '}
              <a href={RELEASES_URL} target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:text-blue-300">latest release</a>.
            </p>
          </div>
          <div>
            <h4 className="text-sm font-medium text-white mb-1">2. Create the config directory</h4>
            <p className="text-xs text-gray-500 mb-2">Run in an elevated PowerShell:</p>
            <CodeBlock text={'New-Item -ItemType Directory -Force -Path "$env:ProgramData\\riot"'} copied={copied} onCopy={handleCopy} />
          </div>
          <div>
            <h4 className="text-sm font-medium text-white mb-1">3. Write the config file</h4>
            <p className="text-xs text-gray-500 mb-2">
              Create <code className="text-gray-300">%ProgramData%\riot\agent.yaml</code> with the following content:
            </p>
            {mtlsEnabled && !bootstrapKey && generateKeyMut.isPending ? (
              <div className="bg-gray-800 rounded-lg p-4 text-xs text-gray-500">Generating bootstrap key...</div>
            ) : (
              <CodeBlock text={macConfigYaml} copied={copied} onCopy={handleCopy} />
            )}
          </div>
          <div>
            <h4 className="text-sm font-medium text-white mb-1">4. Run the agent</h4>
            <p className="text-xs text-gray-500 mb-2">From an elevated terminal:</p>
            <CodeBlock text={'.\\riot-agent-windows-amd64.exe -config "$env:ProgramData\\riot\\agent.yaml"'} copied={copied} onCopy={handleCopy} />
            <p className="text-xs text-gray-500 mt-2">
              To run as a service, use{' '}
              <a href="https://nssm.cc/" target="_blank" rel="noopener noreferrer" className="text-blue-400 hover:text-blue-300">NSSM</a>{' '}
              or <code className="text-gray-300">sc.exe</code>.
            </p>
          </div>
        </>
      )}
    </div>
  )

  if (inline) {
    return (
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-6">
        <h3 className="text-base font-semibold text-white mb-1">Add Your First Device</h3>
        {mtlsEnabled && (
          <p className="text-xs text-blue-400 mb-4">mTLS is enabled — the install command below includes a bootstrap key for certificate enrollment.</p>
        )}
        {content}
      </div>
    )
  }

  // Modal
  return (
    <div className="fixed inset-0 bg-black/60 flex items-center justify-center z-[60]" onClick={onClose}>
      <div
        className="bg-gray-900 border border-gray-700 rounded-lg w-full max-w-lg mx-4 p-6 max-h-[85vh] overflow-y-auto scrollbar-thin"
        onClick={e => e.stopPropagation()}
      >
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

function CodeBlock({ text, copied, onCopy }: { text: string; copied: boolean; onCopy: (t: string) => void }) {
  return (
    <div className="relative bg-gray-800 rounded-lg p-4 group">
      <pre className="text-xs text-emerald-400 whitespace-pre-wrap break-all select-all leading-relaxed">{text}</pre>
      <button
        onClick={() => onCopy(text)}
        className="absolute top-2 right-2 px-2 py-1 text-xs rounded bg-gray-700 text-gray-300 hover:bg-gray-600 hover:text-white transition-colors"
      >
        {copied ? 'Copied!' : 'Copy'}
      </button>
    </div>
  )
}
