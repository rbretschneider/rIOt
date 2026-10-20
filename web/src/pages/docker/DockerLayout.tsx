import { Outlet } from 'react-router-dom'
import { EmptyState } from '../../components/ui'
import { useFeatures } from '../../hooks/useFeatures'

export default function DockerLayout() {
  const { isEnabled } = useFeatures()
  if (!isEnabled('docker'))
    return (
      <EmptyState
        title="Docker is disabled"
        description="Enable Docker in Settings to view this area."
      />
    )
  return <Outlet />
}
