import { Badge } from './ui'

interface Props {
  severity: 'info' | 'warning' | 'critical'
}

/** Severity pill. The label and tone come from the shared enum label map. */
export default function SeverityBadge({ severity }: Props) {
  return <Badge category="severity" value={severity} />
}
