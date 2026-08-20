export class InitialMessageCache {
  private static readonly STORAGE_KEY = 'agentapi_initial_message_cache'
  private static readonly MAX_CACHE_SIZE = 2

  static getCachedMessages(): string[] {
    try {
      const cached = localStorage.getItem(this.STORAGE_KEY)
      if (!cached) return []
      const messages = JSON.parse(cached)
      return Array.isArray(messages) ? messages : []
    } catch {
      return []
    }
  }

  static addMessage(message: string): void {
    if (!message.trim()) return

    try {
      const messages = this.getCachedMessages()

      // 既に同じメッセージが存在する場合は、それを削除してから先頭に追加
      const filteredMessages = messages.filter(m => m !== message)

      // 新しいメッセージを先頭に追加
      const updatedMessages = [message, ...filteredMessages]

      // 最大数を超えた場合は古いものを削除
      const limitedMessages = updatedMessages.slice(0, this.MAX_CACHE_SIZE)

      localStorage.setItem(this.STORAGE_KEY, JSON.stringify(limitedMessages))
    } catch (error) {
      console.error('Failed to cache initial message:', error)
    }
  }

  static clearCache(): void {
    try {
      localStorage.removeItem(this.STORAGE_KEY)
    } catch (error) {
      console.error('Failed to clear initial message cache:', error)
    }
  }
}
