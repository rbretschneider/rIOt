// Literal colors for recharts, which needs strings for SVG attributes and
// contentStyle objects (CSS variables are unreliable there). Every value is the
// exact literal the charts already used; the comment names the Tailwind step.
// A palette change must touch this file and the aliases in index.css.

export const chartColors = Object.freeze({
  series: Object.freeze({
    cpu: '#3b82f6', // blue-500
    mem: '#8b5cf6', // violet-500 (device detail memory)
    memAlt: '#10b981', // emerald-500 (fleet charts RAM)
    diskIo: '#f59e0b', // amber-500
    diskRead: '#34d399', // emerald-400
    diskWrite: '#fb923c', // orange-400
    load: '#a855f7', // purple-500
    netRx: '#06b6d4', // cyan-500
    netTx: '#f97316', // orange-500
  }),
  axisText: '#6b7280', // gray-500
  axisLine: '#374151', // gray-700
  grid: '#1f2937', // gray-800
  tooltipBg: '#1f2937', // gray-800
  tooltipBgDense: '#111827', // gray-900
  tooltipBorder: '#374151', // gray-700
  legendText: '#9ca3af', // gray-400
  legendTextHidden: '#4b5563', // gray-600
})
