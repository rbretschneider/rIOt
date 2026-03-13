import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { settingsApi } from '../../api/settings'
import { FEATURES, type FeatureKey } from '../../hooks/useFeatures'

export default function FeatureSettings() {
  const qc = useQueryClient()
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

  if (isLoading) {
    return <div className="text-gray-400">Loading...</div>
  }

  return (
    <div>
      <div className="mb-4">
        <h2 className="text-lg font-semibold text-white">Dashboard Features</h2>
        <p className="text-sm text-gray-400 mt-1">
          Toggle visibility of features across the dashboard. Disabled features are hidden from all views.
          Data collection continues regardless of these settings.
        </p>
      </div>

      <div className="space-y-2">
        {(Object.keys(FEATURES) as FeatureKey[]).map(key => {
          const feature = FEATURES[key]
          const enabled = toggles[key] !== false
          return (
            <div
              key={key}
              className="bg-gray-900 rounded-lg border border-gray-800 p-4 flex items-center justify-between"
            >
              <div>
                <span className="text-white font-medium">{feature.label}</span>
                <p className="text-xs text-gray-500 mt-0.5">{feature.description}</p>
              </div>
              <button
                onClick={() => toggle(key)}
                disabled={mutation.isPending}
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors focus:outline-none ${
                  enabled ? 'bg-blue-600' : 'bg-gray-700'
                }`}
              >
                <span
                  className={`inline-block h-4 w-4 rounded-full bg-white transition-transform ${
                    enabled ? 'translate-x-6' : 'translate-x-1'
                  }`}
                />
              </button>
            </div>
          )
        })}
      </div>

      <div className="mt-6 p-4 bg-gray-900/50 rounded-lg border border-gray-800/50">
        <p className="text-xs text-gray-500">
          These toggles control UI visibility only. Agents will continue collecting all enabled metrics.
          Per-device overrides may be supported in a future release.
        </p>
      </div>
    </div>
  )
}
