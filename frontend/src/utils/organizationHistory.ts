import { RepositoryHistoryItem, loadFullGlobalSettings } from '../types/settings';

const ORGANIZATION_HISTORY_PREFIX = 'agentapi-org-history-';
const MAX_HISTORY_SIZE = 10;

export interface OrganizationRepositoryHistory {
  organization: string;
  repositories: RepositoryHistoryItem[];
}

export type { RepositoryHistoryItem };

export class OrganizationHistory {
  // 組織固有のキーを生成
  private static getOrganizationKey(organization: string): string {
    return `${ORGANIZATION_HISTORY_PREFIX}${organization}`;
  }

  static getOrganizationHistory(organization: string): RepositoryHistoryItem[] {
    if (!organization) return [];

    try {
      const key = this.getOrganizationKey(organization);
      const stored = localStorage.getItem(key);
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

  static addRepositoryToOrganization(organization: string, repository: string): void {
    if (!organization || !repository.trim()) return;

    const history = this.getOrganizationHistory(organization);
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
      const key = this.getOrganizationKey(organization);
      localStorage.setItem(key, JSON.stringify(trimmed));
    } catch {
      // ストレージエラーを無視
    }
  }

  static getAllOrganizationHistories(): OrganizationRepositoryHistory[] {
    const histories: OrganizationRepositoryHistory[] = [];

    try {
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (key && key.startsWith(ORGANIZATION_HISTORY_PREFIX)) {
          const organization = key.substring(ORGANIZATION_HISTORY_PREFIX.length);
          const repositories = this.getOrganizationHistory(organization);
          if (repositories.length > 0) {
            histories.push({ organization, repositories });
          }
        }
      }
    } catch {
      // ストレージエラーを無視
    }

    return histories;
  }

  static clearOrganizationHistory(organization: string): void {
    if (!organization) return;

    try {
      const key = this.getOrganizationKey(organization);
      localStorage.removeItem(key);
    } catch {
      // ストレージエラーを無視
    }
  }

  static clearAllOrganizationHistories(): void {
    try {
      const keysToRemove: string[] = [];
      for (let i = 0; i < localStorage.length; i++) {
        const key = localStorage.key(i);
        if (key && key.startsWith(ORGANIZATION_HISTORY_PREFIX)) {
          keysToRemove.push(key);
        }
      }
      keysToRemove.forEach(key => localStorage.removeItem(key));
    } catch {
      // ストレージエラーを無視
    }
  }

  static searchOrganizationRepositories(organization: string, query: string): string[] {
    const history = this.getOrganizationHistory(organization);
    const lowerQuery = query.toLowerCase();

    return history
      .filter(item => item.repository.toLowerCase().includes(lowerQuery))
      .map(item => item.repository);
  }

  // グローバルのリポジトリ履歴からサジェストを取得
  static getRepositorySuggestions(query?: string): string[] {
    try {
      const settings = loadFullGlobalSettings();
      let repositories: string[] = [];

      // 履歴から取得
      if (settings && settings.repositoryHistory) {
        repositories = settings.repositoryHistory
          .map(item => item.repository)
          .filter(repo => repo && repo.trim());
      }

      // sessionStorageからGitHubリポジトリリストを取得して追加
      if (typeof window !== 'undefined') {
        try {
          const userRepos = sessionStorage.getItem('user_repositories');
          if (userRepos) {
            const parsedRepos: string[] = JSON.parse(userRepos);
            // 既存の履歴にないリポジトリを追加
            repositories = [...repositories, ...parsedRepos];
          }
        } catch {
          // sessionStorageエラーを無視
        }
      }

      if (query && query.trim()) {
        const lowerQuery = query.toLowerCase();
        repositories = repositories.filter(repo =>
          repo.toLowerCase().includes(lowerQuery)
        );
      }

      // 重複を削除
      const uniqueRepositories: string[] = [...new Set(repositories)];

      // 履歴にあるものを優先してソート
      const historyRepos = settings?.repositoryHistory?.map(item => item.repository) || [];
      const sortedRepos = uniqueRepositories.sort((a, b) => {
        const aInHistory = historyRepos.indexOf(a);
        const bInHistory = historyRepos.indexOf(b);

        // 両方とも履歴にある場合は履歴順
        if (aInHistory !== -1 && bInHistory !== -1) {
          return aInHistory - bInHistory;
        }
        // 片方だけ履歴にある場合は履歴にあるものを優先
        if (aInHistory !== -1) return -1;
        if (bInHistory !== -1) return 1;
        // 両方とも履歴にない場合はアルファベット順
        return a.localeCompare(b);
      });

      return sortedRepos.slice(0, 50); // より多くのリポジトリを表示可能に
    } catch {
      return [];
    }
  }

  // リポジトリ履歴と一覧を分けて取得
  static getRepositorySuggestionsGrouped(query?: string): {
    history: string[];
    repositories: string[];
  } {
    try {
      const settings = loadFullGlobalSettings();
      let historyRepos: string[] = [];
      let allRepos: string[] = [];

      // 履歴から取得
      if (settings && settings.repositoryHistory) {
        historyRepos = settings.repositoryHistory
          .map(item => item.repository)
          .filter(repo => repo && repo.trim());
      }

      // sessionStorageからGitHubリポジトリリストを取得
      if (typeof window !== 'undefined') {
        try {
          const userRepos = sessionStorage.getItem('user_repositories');
          if (userRepos) {
            const parsedRepos: string[] = JSON.parse(userRepos);
            allRepos = parsedRepos;
          }
        } catch {
          // sessionStorageエラーを無視
        }
      }

      // クエリでフィルタ
      if (query && query.trim()) {
        const lowerQuery = query.toLowerCase();
        historyRepos = historyRepos.filter(repo =>
          repo.toLowerCase().includes(lowerQuery)
        );
        allRepos = allRepos.filter(repo =>
          repo.toLowerCase().includes(lowerQuery)
        );
      }

      // 履歴にないリポジトリのみを一覧として返す
      const historySet = new Set(historyRepos);
      const repositoriesOnly = allRepos.filter(repo => !historySet.has(repo));

      // 重複を削除してソート
      const uniqueHistory = [...new Set(historyRepos)];
      const uniqueRepositories = [...new Set(repositoriesOnly)].sort((a, b) => a.localeCompare(b));

      return {
        history: uniqueHistory.slice(0, 20),
        repositories: uniqueRepositories.slice(0, 50)
      };
    } catch {
      return { history: [], repositories: [] };
    }
  }
}
