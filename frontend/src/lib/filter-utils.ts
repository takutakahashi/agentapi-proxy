import { Session } from '../types/agentapi'

export interface FilterValue {
  key: string
  value: string
  count: number
}

export interface FilterGroup {
  key: string
  label: string
  values: FilterValue[]
}

export interface SessionFilter {
  metadataFilters: Record<string, string[]>
  environmentFilters: Record<string, string[]>
  tagFilters: Record<string, string[]>
  status?: Session['status']
}

/**
 * Extract all unique metadata, environment keys and tags and their values from sessions
 */
export function extractFilterGroups(sessions: Session[]): FilterGroup[] {
  const metadataKeys = new Map<string, Map<string, number>>()
  const environmentKeys = new Map<string, Map<string, number>>()
  const tagKeys = new Map<string, Map<string, number>>()

  // Handle null/undefined sessions gracefully
  if (!sessions || !Array.isArray(sessions)) {
    return []
  }

  // Process each session to extract keys and values
  sessions.forEach(session => {
    // Process metadata
    if (session.metadata) {
      Object.entries(session.metadata).forEach(([key, value]) => {
        if (value !== null && value !== undefined) {
          const stringValue = String(value)
          if (!metadataKeys.has(key)) {
            metadataKeys.set(key, new Map<string, number>())
          }
          const valueMap = metadataKeys.get(key)!
          valueMap.set(stringValue, (valueMap.get(stringValue) || 0) + 1)
        }
      })
    }

    // Process environment variables
    if (session.environment) {
      Object.entries(session.environment).forEach(([key, value]) => {
        if (value !== null && value !== undefined && value !== '') {
          if (!environmentKeys.has(key)) {
            environmentKeys.set(key, new Map<string, number>())
          }
          const valueMap = environmentKeys.get(key)!
          valueMap.set(value, (valueMap.get(value) || 0) + 1)
        }
      })
    }

    // Process tags
    if (session.tags) {
      Object.entries(session.tags).forEach(([key, value]) => {
        if (value !== null && value !== undefined && value !== '') {
          if (!tagKeys.has(key)) {
            tagKeys.set(key, new Map<string, number>())
          }
          const valueMap = tagKeys.get(key)!
          valueMap.set(value, (valueMap.get(value) || 0) + 1)
        }
      })
    }
  })

  const filterGroups: FilterGroup[] = []

  // Convert tags to FilterGroups (prioritize tags first)
  tagKeys.forEach((valueMap, key) => {
    const values: FilterValue[] = Array.from(valueMap.entries())
      .map(([value, count]) => ({ key, value, count }))
      .sort((a, b) => b.count - a.count) // Sort by count descending

    filterGroups.push({
      key: `tags.${key}`,
      label: `Tag: ${key}`,
      values
    })
  })

  // Convert metadata keys to FilterGroups
  metadataKeys.forEach((valueMap, key) => {
    const values: FilterValue[] = Array.from(valueMap.entries())
      .map(([value, count]) => ({ key, value, count }))
      .sort((a, b) => b.count - a.count) // Sort by count descending

    filterGroups.push({
      key: `metadata.${key}`,
      label: `Metadata: ${key}`,
      values
    })
  })

  // Convert environment keys to FilterGroups
  environmentKeys.forEach((valueMap, key) => {
    const values: FilterValue[] = Array.from(valueMap.entries())
      .map(([value, count]) => ({ key, value, count }))
      .sort((a, b) => b.count - a.count) // Sort by count descending

    filterGroups.push({
      key: `environment.${key}`,
      label: `Environment: ${key}`,
      values
    })
  })

  // Sort filter groups alphabetically
  return filterGroups.sort((a, b) => a.label.localeCompare(b.label))
}

/**
 * Apply filters to sessions (client-side filtering)
 */
export function applySessionFilters(sessions: Session[], filters: SessionFilter): Session[] {
  // Handle null/undefined sessions gracefully
  if (!sessions || !Array.isArray(sessions)) {
    return []
  }

  return sessions.filter(session => {
    // Status filter
    if (filters.status && session.status !== filters.status) {
      return false
    }

    // Tag filters
    for (const [key, values] of Object.entries(filters.tagFilters)) {
      if (values.length === 0) continue
      
      const sessionValue = session.tags?.[key]
      if (!sessionValue || !values.includes(sessionValue)) {
        return false
      }
    }

    // Metadata filters
    for (const [key, values] of Object.entries(filters.metadataFilters)) {
      if (values.length === 0) continue
      
      const sessionValue = session.metadata?.[key]
      if (!sessionValue || !values.includes(String(sessionValue))) {
        return false
      }
    }

    // Environment filters
    for (const [key, values] of Object.entries(filters.environmentFilters)) {
      if (values.length === 0) continue
      
      const sessionValue = session.environment?.[key]
      if (!sessionValue || !values.includes(sessionValue)) {
        return false
      }
    }

    return true
  })
}

/**
 * Convert URL query parameters to SessionFilter
 */
export function parseFiltersFromURL(searchParams: URLSearchParams): SessionFilter {
  const metadataFilters: Record<string, string[]> = {}
  const environmentFilters: Record<string, string[]> = {}
  const tagFilters: Record<string, string[]> = {}
  let status: Session['status'] | undefined

  for (const [key, value] of searchParams.entries()) {
    if (key === 'status' && ['creating', 'starting', 'active', 'unhealthy', 'stopped', 'error', 'timeout', 'unknown'].includes(value)) {
      status = value as Session['status']
    } else if (key.startsWith('tags.')) {
      const tagKey = key.replace('tags.', '')
      tagFilters[tagKey] = value.split(',').filter(v => typeof v === 'string' && v.trim() !== '')
    } else if (key.startsWith('metadata.')) {
      const metadataKey = key.replace('metadata.', '')
      metadataFilters[metadataKey] = value.split(',').filter(v => typeof v === 'string' && v.trim() !== '')
    } else if (key.startsWith('environment.')) {
      const envKey = key.replace('environment.', '')
      environmentFilters[envKey] = value.split(',').filter(v => typeof v === 'string' && v.trim() !== '')
    }
  }

  return {
    metadataFilters,
    environmentFilters,
    tagFilters,
    status
  }
}

/**
 * Convert SessionFilter to URL query parameters
 */
export function filtersToURLParams(filters: SessionFilter): URLSearchParams {
  const params = new URLSearchParams()

  if (filters.status) {
    params.set('status', filters.status)
  }

  Object.entries(filters.tagFilters).forEach(([key, values]) => {
    if (values.length > 0) {
      params.set(`tags.${key}`, values.join(','))
    }
  })

  Object.entries(filters.metadataFilters).forEach(([key, values]) => {
    if (values.length > 0) {
      params.set(`metadata.${key}`, values.join(','))
    }
  })

  Object.entries(filters.environmentFilters).forEach(([key, values]) => {
    if (values.length > 0) {
      params.set(`environment.${key}`, values.join(','))
    }
  })

  return params
}

/**
 * Get filter values for session creation based on current filters
 */
export function getFilterValuesForSessionCreation(filters: SessionFilter): {
  metadata: Record<string, unknown>
  environment: Record<string, string>
  tags: Record<string, string>
} {
  const metadata: Record<string, unknown> = {}
  const environment: Record<string, string> = {}
  const tags: Record<string, string> = {}

  // Take the first value from each filter (most common/recent)
  Object.entries(filters.tagFilters).forEach(([key, values]) => {
    if (values.length > 0) {
      tags[key] = values[0]
    }
  })

  Object.entries(filters.metadataFilters).forEach(([key, values]) => {
    if (values.length > 0) {
      metadata[key] = values[0]
    }
  })

  Object.entries(filters.environmentFilters).forEach(([key, values]) => {
    if (values.length > 0) {
      environment[key] = values[0]
    }
  })

  return { metadata, environment, tags }
}
