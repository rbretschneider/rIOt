interface Props {
  severity: 'info' | 'warning' | 'critical'
}

const styles = {
  info: 'bg-blue-500/20 text-blue-400',
  warning: 'bg-amber-500/20 text-amber-400',
  critical: 'bg-red-500/20 text-red-400',
}

export default function SeverityBadge({ severity }: Props) {
  return (
    <span className={`px-2 py-0.5 rounded text-xs font-medium ${styles[severity]}`}>
      {severity}
    </span>
  )
}
