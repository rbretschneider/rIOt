import { useState, useMemo } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'
import { FEATURES, type FeatureKey } from '../../hooks/useFeatures'

export default function FeatureSettings() {
  const qc = useQueryClient()
  const [filter, setFilter] = useState('')
  const { data: toggles = {}, isLoading } = useQuery({
    queryKey: ['feature-toggles'],
    queryFn: settingsApi.getFeatureToggles,
  })

  const mutation = useMutation({
    mutationFn: (updated: Record<string, boolean>) => settingsApi.saveFeatureToggles(updated),
    onSuccess: (data) => {
      qc.setQueryData(['feature-toggles'], data)
    },
  })

  const toggle = (key: FeatureKey) => {
    const current = toggles[key] !== false // default true
    const updated = { ...toggles, [key]: !current }
    mutation.mutate(updated)
  }

  const featureKeys = useMemo(() => {
    const keys = Object.keys(FEATURES) as FeatureKey[]
    if (!filter.trim()) return keys
    const q = filter.toLowerCase()
    return keys.filter(key => {
      const f = FEATURES[key]
      return f.label.toLowerCase().includes(q) || f.description.toLowerCase().includes(q)
    })
  }, [filter])

  if (isLoading) {
    return <div className="text-gray-400">Loading...</div>
  }

  return (
    <div>
      <div className="mb-4">
        <h2 className="text-lg font-semibold text-white">Dashboard Features</h2>
        <p className="text-sm text-gray-400 mt-1">
          Toggle visibility of features across the dashboard. Disabled features are hidden from all views.
          Data collection continues regardless of these settings. Per-device overrides may be supported in a future release.
        </p>
      </div>

      {/* Search / filter */}
      <div className="mb-4">
        <input
          type="text"
          value={filter}
          onChange={e => setFilter(e.target.value)}
          placeholder="Filter features..."
          className="w-full max-w-sm px-3 py-1.5 text-sm bg-gray-800 border border-gray-700 rounded-md text-gray-200 placeholder-gray-500 focus:outline-none focus:border-gray-600"
        />
      </div>

      {/* Feature grid */}
      <div className="grid grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2">
        {featureKeys.map(key => {
          const feature = FEATURES[key]
          const enabled = toggles[key] !== false
          return (
            <button
              key={key}
              onClick={() => toggle(key)}
              disabled={mutation.isPending}
              className={`text-left rounded-lg border p-3 transition-colors cursor-pointer disabled:opacity-50 ${
                enabled
                  ? 'bg-gray-900 border-gray-700 hover:border-gray-600'
                  : 'bg-gray-900/50 border-gray-800 hover:border-gray-700 opacity-60'
              }`}
            >
              <div className="flex items-center justify-between gap-2">
                <span className={`text-sm font-medium ${enabled ? 'text-white' : 'text-gray-400'}`}>
                  {feature.label}
                </span>
                <span className={`w-2 h-2 rounded-full flex-shrink-0 ${enabled ? 'bg-blue-500' : 'bg-gray-600'}`} />
              </div>
              <p className="text-xs text-gray-500 mt-1 line-clamp-2">{feature.description}</p>
            </button>
          )
        })}
      </div>

      {featureKeys.length === 0 && (
        <p className="text-sm text-gray-500 mt-2">No features match "{filter}"</p>
      )}
    </div>
  )
}
