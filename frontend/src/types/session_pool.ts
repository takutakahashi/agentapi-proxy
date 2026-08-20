export interface ClusterSessionManager {
  id: string
  name: string
  enabled: boolean
  draining?: boolean
  last_heartbeat_at?: string
}

export interface LogicalSessionPool {
  name: string
  labels?: Record<string, string>
  enabled: boolean
}

export interface SessionPoolSupplier {
  pool: string
  manager_id: string
  labels?: Record<string, string>
  min_idle?: number
  max_runners?: number
  enabled: boolean
  draining?: boolean
  idle_runners?: number
  total_runners?: number
}

export interface SessionPoolBinding {
  id: string
  pool: string
  subject_type: 'user' | 'team' | 'all'
  subject_id: string
  role: 'use' | 'manage'
  enabled: boolean
  priority?: number
  max_concurrent?: number
}
