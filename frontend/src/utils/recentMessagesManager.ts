export interface RecentMessage {
  id: string;
  content: string;
  timestamp: number;
}

const STORAGE_KEY = 'recent_messages';
const MAX_RECENT_MESSAGES = 10;

class RecentMessagesManager {
  async saveMessage(content: string): Promise<void> {
    try {
      const messages = await this.getRecentMessages();

      // 重複を避ける（同じ内容のメッセージは最新のものだけ保持）
      const filteredMessages = messages.filter(msg => msg.content !== content);

      const newMessage: RecentMessage = {
        id: crypto.randomUUID(),
        content,
        timestamp: Date.now(),
      };

      // 新しいメッセージを先頭に追加
      const updatedMessages = [newMessage, ...filteredMessages];

      // 最大数を超えた場合は古いものから削除
      const trimmedMessages = updatedMessages.slice(0, MAX_RECENT_MESSAGES);

      localStorage.setItem(STORAGE_KEY, JSON.stringify(trimmedMessages));
    } catch (error) {
      console.error('Failed to save recent message:', error);
    }
  }

  async getRecentMessages(): Promise<RecentMessage[]> {
    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (!stored) return [];

      const messages = JSON.parse(stored) as RecentMessage[];

      // 最新のものから並べる
      return messages.sort((a, b) => b.timestamp - a.timestamp);
    } catch (error) {
      console.error('Failed to get recent messages:', error);
      return [];
    }
  }

  async clearRecentMessages(): Promise<void> {
    try {
      localStorage.removeItem(STORAGE_KEY);
    } catch (error) {
      console.error('Failed to clear recent messages:', error);
    }
  }
}

export const recentMessagesManager = new RecentMessagesManager();
