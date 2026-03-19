import { useState, type FormEvent } from 'react'

interface SetupProps {
  onComplete: () => void
}

type TLSMode = 'self-signed' | 'letsencrypt' | 'none'

export default function Setup({ onComplete }: SetupProps) {
  const [step, setStep] = useState(0)
  const [password, setPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [tlsMode, setTlsMode] = useState<TLSMode>('self-signed')
  const [tlsDomain, setTlsDomain] = useState('')
  const [extraSANs, setExtraSANs] = useState<string[]>([])
  const [sanInput, setSanInput] = useState('')
  const [mtlsEnabled, setMtlsEnabled] = useState(false)
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const steps = ['Password', 'TLS', ...(tlsMode === 'letsencrypt' ? ['Domain'] : []), 'Security', 'Review']

  function nextStep() {
    setError('')
    if (step === 0) {
      if (password.length < 8) {
        setError('Password must be at least 8 characters')
        return
      }
      if (password !== confirmPassword) {
        setError('Passwords do not match')
        return
      }
    }
    if (step === 1 && tlsMode === 'letsencrypt') {
      // next step is Domain input
    }
    setStep(s => s + 1)
  }

  function prevStep() {
    setError('')
    setStep(s => s - 1)
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError('')
    setSubmitting(true)
    try {
      const res = await fetch('/api/v1/setup/complete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          password,
          tls_mode: tlsMode,
          tls_domain: tlsMode === 'letsencrypt' ? tlsDomain : '',
          extra_sans: tlsMode === 'self-signed' ? extraSANs : [],
          mtls_enabled: mtlsEnabled,
        }),
      })
      if (!res.ok) {
        const data = await res.json().catch(() => ({ error: 'Setup failed' }))
        setError(data.error || 'Setup failed')
        setSubmitting(false)
        return
      }
      const data = await res.json()
      if (data.tls_enabled) {
        // Server is restarting with TLS — redirect after a short delay
        setTimeout(() => {
          const newUrl = window.location.href.replace(/^http:/, 'https:')
          window.location.href = newUrl
        }, 2000)
      } else {
        onComplete()
      }
    } catch {
      setError('Network error — please try again')
      setSubmitting(false)
    }
  }

  const isLastStep = step === steps.length - 1

  return (
    <div className="min-h-screen bg-gray-950 flex items-center justify-center p-4">
      <div className="bg-gray-900 border border-gray-800 rounded-lg p-8 w-full max-w-lg">
        <h1 className="text-2xl font-bold text-white mb-1 text-center">rIOt Setup</h1>
        <p className="text-sm text-gray-500 text-center mb-6">Configure your monitoring server</p>

        {/* Step indicator */}
        <div className="flex items-center justify-center gap-2 mb-8">
          {steps.map((label, i) => (
            <div key={label} className="flex items-center gap-2">
              <div className={`w-7 h-7 rounded-full flex items-center justify-center text-xs font-medium ${
                i < step ? 'bg-emerald-600 text-white' :
                i === step ? 'bg-blue-600 text-white' :
                'bg-gray-800 text-gray-500'
              }`}>
                {i < step ? '\u2713' : i + 1}
              </div>
              {i < steps.length - 1 && (
                <div className={`w-6 h-px ${i < step ? 'bg-emerald-600' : 'bg-gray-700'}`} />
              )}
            </div>
          ))}
        </div>

        <form onSubmit={isLastStep ? handleSubmit : (e) => { e.preventDefault(); nextStep() }}>
          {/* Step 0: Password */}
          {step === 0 && (
            <div className="space-y-4">
              <h2 className="text-lg font-medium text-white">Set Admin Password</h2>
              <p className="text-sm text-gray-400">Choose a password for the admin dashboard.</p>
              <div>
                <label className="block text-sm text-gray-400 mb-1" htmlFor="setup-password">Password</label>
                <input
                  id="setup-password"
                  type="password"
                  value={password}
                  onChange={e => setPassword(e.target.value)}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="Min. 8 characters"
                  autoFocus
                />
              </div>
              <div>
                <label className="block text-sm text-gray-400 mb-1" htmlFor="setup-confirm">Confirm Password</label>
                <input
                  id="setup-confirm"
                  type="password"
                  value={confirmPassword}
                  onChange={e => setConfirmPassword(e.target.value)}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="Repeat password"
                />
              </div>
            </div>
          )}

          {/* Step 1: TLS Mode */}
          {step === 1 && (
            <div className="space-y-4">
              <h2 className="text-lg font-medium text-white">TLS Configuration</h2>
              <p className="text-sm text-gray-400">How should the server handle HTTPS?</p>
              <div className="space-y-3">
                <TLSOption
                  selected={tlsMode === 'self-signed'}
                  onClick={() => setTlsMode('self-signed')}
                  title="Automatic (Self-Signed)"
                  description="Generate a self-signed certificate. Best for LAN-only setups. Browsers will show a warning on first visit."
                  recommended
                />
                <TLSOption
                  selected={tlsMode === 'letsencrypt'}
                  onClick={() => setTlsMode('letsencrypt')}
                  title="Let's Encrypt"
                  description="Free trusted certificate. Requires a public domain name and port 443 accessible from the internet."
                />
                <TLSOption
                  selected={tlsMode === 'none'}
                  onClick={() => setTlsMode('none')}
                  title="Skip (Reverse Proxy)"
                  description="No TLS on this server. Use if you have nginx, Caddy, or Traefik handling TLS in front."
                />
              </div>
              {tlsMode === 'self-signed' && (
                <div className="mt-4 pt-4 border-t border-gray-700">
                  <p className="text-sm text-gray-400 mb-2">External hostnames <span className="text-gray-600">(optional)</span></p>
                  <p className="text-xs text-gray-500 mb-3">
                    Add DDNS domains or external IPs so remote agents can verify this server's certificate.
                  </p>
                  <div className="flex gap-2 mb-2">
                    <input
                      type="text"
                      value={sanInput}
                      onChange={e => setSanInput(e.target.value)}
                      onKeyDown={e => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          const v = sanInput.trim()
                          if (v && !extraSANs.includes(v)) {
                            setExtraSANs([...extraSANs, v])
                            setSanInput('')
                          }
                        }
                      }}
                      placeholder="e.g. mylab.duckdns.org"
                      className="flex-1 px-3 py-2 text-sm bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                    />
                    <button
                      type="button"
                      onClick={() => {
                        const v = sanInput.trim()
                        if (v && !extraSANs.includes(v)) {
                          setExtraSANs([...extraSANs, v])
                          setSanInput('')
                        }
                      }}
                      className="px-3 py-2 text-sm bg-gray-700 hover:bg-gray-600 text-white rounded-md transition-colors"
                    >
                      Add
                    </button>
                  </div>
                  {extraSANs.length > 0 && (
                    <div className="space-y-1">
                      {extraSANs.map((s, i) => (
                        <div key={i} className="flex items-center justify-between px-3 py-1 bg-gray-800 rounded text-sm">
                          <code className="text-emerald-400 text-xs">{s}</code>
                          <button
                            type="button"
                            onClick={() => setExtraSANs(extraSANs.filter((_, idx) => idx !== i))}
                            className="text-gray-500 hover:text-red-400 text-xs transition-colors"
                          >
                            remove
                          </button>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          {/* Conditional Step: Domain (for Let's Encrypt) */}
          {step === 2 && tlsMode === 'letsencrypt' && (
            <div className="space-y-4">
              <h2 className="text-lg font-medium text-white">Domain Name</h2>
              <p className="text-sm text-gray-400">Enter the domain that points to this server.</p>
              <div>
                <label className="block text-sm text-gray-400 mb-1" htmlFor="setup-domain">Domain</label>
                <input
                  id="setup-domain"
                  type="text"
                  value={tlsDomain}
                  onChange={e => setTlsDomain(e.target.value)}
                  className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded-md text-white placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-blue-500"
                  placeholder="monitor.example.com"
                  autoFocus
                />
              </div>
            </div>
          )}

          {/* Security (mTLS) step */}
          {step === steps.indexOf('Security') && (
            <div className="space-y-4">
              <h2 className="text-lg font-medium text-white">Device Authentication</h2>
              <p className="text-sm text-gray-400">mTLS provides certificate-based device authentication for stronger security.</p>
              <label className="flex items-start gap-3 p-4 bg-gray-800 rounded-lg border border-gray-700 cursor-pointer hover:border-gray-600 transition-colors">
                <input
                  type="checkbox"
                  checked={mtlsEnabled}
                  onChange={e => setMtlsEnabled(e.target.checked)}
                  className="mt-0.5 w-4 h-4 rounded border-gray-600 bg-gray-700 text-blue-600 focus:ring-blue-500"
                />
                <div>
                  <span className="text-sm font-medium text-white">Enable mTLS</span>
                  <p className="text-xs text-gray-400 mt-1">
                    Devices will authenticate using client certificates instead of (or in addition to) API keys. A CA will be generated automatically.
                  </p>
                </div>
              </label>
            </div>
          )}

          {/* Review step */}
          {isLastStep && (
            <div className="space-y-4">
              <h2 className="text-lg font-medium text-white">Review & Complete</h2>
              <div className="bg-gray-800 rounded-lg p-4 space-y-3">
                <ReviewItem label="Admin Password" value="Configured" />
                <ReviewItem label="TLS Mode" value={
                  tlsMode === 'self-signed' ? 'Self-Signed Certificate' :
                  tlsMode === 'letsencrypt' ? `Let's Encrypt (${tlsDomain})` :
                  'Disabled (Reverse Proxy)'
                } />
                {extraSANs.length > 0 && (
                  <ReviewItem label="Extra SANs" value={extraSANs.join(', ')} />
                )}
                <ReviewItem label="mTLS" value={mtlsEnabled ? 'Enabled' : 'Disabled'} />
              </div>
              {tlsMode === 'self-signed' && (
                <p className="text-xs text-amber-400">
                  After setup, the server will restart with HTTPS. Your browser will show a certificate warning — this is expected for self-signed certificates.
                </p>
              )}
            </div>
          )}

          {error && (
            <p className="mt-3 text-sm text-red-400">{error}</p>
          )}

          <div className="flex justify-between mt-6">
            {step > 0 ? (
              <button
                type="button"
                onClick={prevStep}
                className="px-4 py-2 text-sm text-gray-400 hover:text-white transition-colors"
                disabled={submitting}
              >
                Back
              </button>
            ) : <div />}
            <button
              type="submit"
              disabled={submitting || (step === 0 && (!password || !confirmPassword))}
              className="px-6 py-2 bg-blue-600 hover:bg-blue-700 disabled:bg-gray-700 disabled:text-gray-500 text-white rounded-md font-medium text-sm transition-colors"
            >
              {submitting ? 'Setting up...' : isLastStep ? 'Complete Setup' : 'Next'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

function TLSOption({ selected, onClick, title, description, recommended }: {
  selected: boolean
  onClick: () => void
  title: string
  description: string
  recommended?: boolean
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`w-full text-left p-4 rounded-lg border transition-colors ${
        selected
          ? 'border-blue-500 bg-blue-500/10'
          : 'border-gray-700 bg-gray-800 hover:border-gray-600'
      }`}
    >
      <div className="flex items-center gap-2">
        <span className="text-sm font-medium text-white">{title}</span>
        {recommended && (
          <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 bg-emerald-500/20 text-emerald-400 rounded font-medium">
            Recommended
          </span>
        )}
      </div>
      <p className="text-xs text-gray-400 mt-1">{description}</p>
    </button>
  )
}

function ReviewItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between items-center">
      <span className="text-sm text-gray-400">{label}</span>
      <span className="text-sm text-white">{value}</span>
    </div>
  )
}
