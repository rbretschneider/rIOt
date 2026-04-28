/** FR-063 / AC-029: Non-blocking banner rendered when wsConnected is false. */
export default function DisconnectedBanner() {
  return (
    <div
      role="status"
      aria-live="polite"
      className="flex items-center gap-2 bg-amber-900/40 border border-amber-800/50 rounded-lg px-4 py-2 text-sm text-amber-300"
      data-testid="disconnected-banner"
    >
      {/* Icon: non-color signal per AC-034 */}
      <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4 shrink-0" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true">
        <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
      </svg>
      <span>Live updates paused — WebSocket disconnected</span>
    </div>
  )
}
