const REPOSITORY_HISTORY_KEY = 'agentapi-repository-history';
const MAX_HISTORY_SIZE = 10;

export interface RepositoryHistoryItem {
  repository: string;
  lastUsed: Date;
}

export class RepositoryHistory {
  static getHistory(): RepositoryHistoryItem[] {
    try {
      const stored = localStorage.getItem(REPOSITORY_HISTORY_KEY);
      if (!stored) return [];
      
      const items = JSON.parse(stored) as Array<{repository: string; lastUsed: string}>;
      return items.map(item => ({
        repository: item.repository,
        lastUsed: new Date(item.lastUsed)
      }));
    } catch {
      return [];
    }
  }

  static addRepository(repository: string): void {
    if (!repository.trim()) return;

    const history = this.getHistory();
    const existing = history.findIndex(item => item.repository === repository);
    
    if (existing !== -1) {
      history[existing].lastUsed = new Date();
    } else {
      history.unshift({
        repository,
        lastUsed: new Date()
      });
    }

    history.sort((a, b) => b.lastUsed.getTime() - a.lastUsed.getTime());
    
    const trimmed = history.slice(0, MAX_HISTORY_SIZE);
    
    try {
      localStorage.setItem(REPOSITORY_HISTORY_KEY, JSON.stringify(trimmed));
    } catch {
      // ストレージエラーを無視
    }
  }

  static searchRepositories(query: string): string[] {
    const history = this.getHistory();
    const lowerQuery = query.toLowerCase();
    
    return history
      .filter(item => item.repository.toLowerCase().includes(lowerQuery))
      .map(item => item.repository);
  }

  static clearHistory(): void {
    try {
      localStorage.removeItem(REPOSITORY_HISTORY_KEY);
    } catch {
      // ストレージエラーを無視
    }
  }
}