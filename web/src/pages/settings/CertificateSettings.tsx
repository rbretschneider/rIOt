import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'

interface DeviceCert {
  id: number
  device_id: string
  serial_number: string
  not_before: string
  not_after: string
  revoked: boolean
  revoked_at?: string
  created_at: string
}

interface BootstrapKey {
  key_hash: string
  label: string
  used: boolean
  used_by_device?: string
  created_at: string
  expires_at: string
}

interface CreateKeyResponse {
  key: string
  key_hash: string
  label: string
  expires_at: string
}

export default function CertificateSettings() {
  const qc = useQueryClient()
  const [showCreateKey, setShowCreateKey] = useState(false)
  const [newKeyLabel, setNewKeyLabel] = useState('')
  const [newKeyExpiry, setNewKeyExpiry] = useState(24)
  const [createdKey, setCreatedKey] = useState<CreateKeyResponse | null>(null)

  const { data: certs = [], isLoading: certsLoading } = useQuery({
    queryKey: ['settings', 'certs'],
    queryFn: settingsApi.getCerts,
  })

  const { data: keys = [], isLoading: keysLoading } = useQuery({
    queryKey: ['settings', 'bootstrap-keys'],
    queryFn: settingsApi.getBootstrapKeys,
  })

  const revokeMut = useMutation({
    mutationFn: (serial: string) => settingsApi.revokeCert(serial),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings', 'certs'] }),
  })

  const createKeyMut = useMutation({
    mutationFn: (data: { label: string; expires_in_hours: number }) =>
      settingsApi.createBootstrapKey(data),
    onSuccess: (data) => {
      setCreatedKey(data)
      setShowCreateKey(false)
      setNewKeyLabel('')
      setNewKeyExpiry(24)
      qc.invalidateQueries({ queryKey: ['settings', 'bootstrap-keys'] })
    },
  })

  const deleteKeyMut = useMutation({
    mutationFn: (hash: string) => settingsApi.deleteBootstrapKey(hash),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings', 'bootstrap-keys'] }),
  })

  return (
    <div className="space-y-8">
      {/* CA Info */}
      <section>
        <h2 className="text-lg font-semibold text-white mb-3">Certificate Authority</h2>
        <div className="bg-gray-900 border border-gray-800 rounded-lg p-4">
          <p className="text-sm text-gray-400 mb-3">
            The server automatically manages a private CA for mTLS device authentication.
          </p>
          <a
            href="/api/v1/ca.pem"
            download="riot-ca.pem"
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm bg-gray-800 text-gray-300 hover:text-white rounded-md transition-colors"
          >
            Download CA Certificate
          </a>
        </div>
      </section>

      {/* Device Certificates */}
      <section>
        <h2 className="text-lg font-semibold text-white mb-3">Device Certificates</h2>
        {certsLoading ? (
          <p className="text-gray-400 text-sm">Loading...</p>
        ) : certs.length === 0 ? (
          <p className="text-gray-500 text-sm">No device certificates issued yet.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-gray-400 border-b border-gray-800">
                  <th className="pb-2 pr-4">Device ID</th>
                  <th className="pb-2 pr-4">Serial</th>
                  <th className="pb-2 pr-4">Expires</th>
                  <th className="pb-2 pr-4">Status</th>
                  <th className="pb-2" />
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800">
                {(certs as DeviceCert[]).map((cert) => (
                  <tr key={cert.serial_number} className="text-gray-300">
                    <td className="py-2 pr-4 font-mono text-xs">{cert.device_id.slice(0, 8)}</td>
                    <td className="py-2 pr-4 font-mono text-xs">{cert.serial_number.slice(0, 16)}...</td>
                    <td className="py-2 pr-4">{new Date(cert.not_after).toLocaleDateString()}</td>
                    <td className="py-2 pr-4">
                      {cert.revoked ? (
                        <span className="text-red-400">Revoked</span>
                      ) : new Date(cert.not_after) < new Date() ? (
                        <span className="text-yellow-400">Expired</span>
                      ) : (
                        <span className="text-green-400">Active</span>
                      )}
                    </td>
                    <td className="py-2">
                      {!cert.revoked && (
                        <button
                          onClick={() => revokeMut.mutate(cert.serial_number)}
                          disabled={revokeMut.isPending}
                          className="text-xs text-red-400 hover:text-red-300"
                        >
                          Revoke
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {/* Bootstrap Keys */}
      <section>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-lg font-semibold text-white">Bootstrap Keys</h2>
          <button
            onClick={() => setShowCreateKey(true)}
            className="px-3 py-1.5 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700 transition-colors"
          >
            Create Key
          </button>
        </div>

        {/* Created key banner — shown once */}
        {createdKey && (
          <div className="bg-green-900/30 border border-green-700 rounded-lg p-4 mb-4">
            <p className="text-sm text-green-300 font-medium mb-1">Bootstrap key created — copy it now, it won't be shown again:</p>
            <code className="block bg-gray-900 text-green-400 p-2 rounded text-xs font-mono break-all select-all">
              {createdKey.key}
            </code>
            <button
              onClick={() => setCreatedKey(null)}
              className="mt-2 text-xs text-gray-400 hover:text-white"
            >
              Dismiss
            </button>
          </div>
        )}

        {/* Create key form */}
        {showCreateKey && (
          <div className="bg-gray-900 border border-gray-800 rounded-lg p-4 mb-4">
            <div className="flex flex-col sm:flex-row gap-3">
              <input
                type="text"
                placeholder="Label (optional)"
                value={newKeyLabel}
                onChange={(e) => setNewKeyLabel(e.target.value)}
                className="flex-1 px-3 py-1.5 bg-gray-800 border border-gray-700 rounded-md text-sm text-white"
              />
              <div className="flex items-center gap-2">
                <label className="text-sm text-gray-400 whitespace-nowrap">Expires in</label>
                <input
                  type="number"
                  min={1}
                  value={newKeyExpiry}
                  onChange={(e) => setNewKeyExpiry(Number(e.target.value))}
                  className="w-20 px-2 py-1.5 bg-gray-800 border border-gray-700 rounded-md text-sm text-white"
                />
                <span className="text-sm text-gray-400">hours</span>
              </div>
              <div className="flex gap-2">
                <button
                  onClick={() => createKeyMut.mutate({ label: newKeyLabel, expires_in_hours: newKeyExpiry })}
                  disabled={createKeyMut.isPending}
                  className="px-3 py-1.5 text-sm bg-blue-600 text-white rounded-md hover:bg-blue-700"
                >
                  Create
                </button>
                <button
                  onClick={() => setShowCreateKey(false)}
                  className="px-3 py-1.5 text-sm text-gray-400 hover:text-white"
                >
                  Cancel
                </button>
              </div>
            </div>
          </div>
        )}

        {keysLoading ? (
          <p className="text-gray-400 text-sm">Loading...</p>
        ) : keys.length === 0 ? (
          <p className="text-gray-500 text-sm">No bootstrap keys.</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-gray-400 border-b border-gray-800">
                  <th className="pb-2 pr-4">Label</th>
                  <th className="pb-2 pr-4">Status</th>
                  <th className="pb-2 pr-4">Expires</th>
                  <th className="pb-2 pr-4">Created</th>
                  <th className="pb-2" />
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800">
                {(keys as BootstrapKey[]).map((key) => (
                  <tr key={key.key_hash} className="text-gray-300">
                    <td className="py-2 pr-4">{key.label || '—'}</td>
                    <td className="py-2 pr-4">
                      {key.used ? (
                        <span className="text-gray-500">Used</span>
                      ) : new Date(key.expires_at) < new Date() ? (
                        <span className="text-yellow-400">Expired</span>
                      ) : (
                        <span className="text-green-400">Active</span>
                      )}
                    </td>
                    <td className="py-2 pr-4">{new Date(key.expires_at).toLocaleString()}</td>
                    <td className="py-2 pr-4">{new Date(key.created_at).toLocaleString()}</td>
                    <td className="py-2">
                      <button
                        onClick={() => deleteKeyMut.mutate(key.key_hash)}
                        disabled={deleteKeyMut.isPending}
                        className="text-xs text-red-400 hover:text-red-300"
                      >
                        Delete
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
