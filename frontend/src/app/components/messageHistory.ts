type MessageWithID = {
  id: string | number
}

function messageKey(message: MessageWithID): string {
  return `${typeof message.id}:${String(message.id)}`
}

/**
 * Merge a refreshed tail/history window into the messages already rendered.
 *
 * Refresh endpoints may return only the latest window, and an empty response can
 * be transient while an ACP bridge reconnects. Keep messages outside that window,
 * replace matching entries with their refreshed representation, and append new
 * entries in server order.
 */
export function mergeRefreshedMessageHistory<T extends MessageWithID>(
  current: T[],
  refreshed: T[],
): T[] {
  if (refreshed.length === 0) return current

  const refreshedByID = new Map(refreshed.map(message => [messageKey(message), message]))
  const merged = current.map(message => refreshedByID.get(messageKey(message)) ?? message)
  const existingIDs = new Set(current.map(messageKey))

  for (const message of refreshed) {
    if (!existingIDs.has(messageKey(message))) {
      merged.push(message)
    }
  }

  return merged
}
