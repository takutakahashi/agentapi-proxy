interface PushNotificationSettings {
  enabled: boolean;
  autoSubscribe: boolean;
  lastSubscriptionTime?: number;
  endpoint?: string;
  showTestNotifications: boolean;
}

const STORAGE_KEY = 'agentapi-push-notification-settings';

const defaultSettings: PushNotificationSettings = {
  enabled: false,
  autoSubscribe: true,
  showTestNotifications: true,
};

export class PushNotificationSettingsManager {
  private settings: PushNotificationSettings;

  constructor() {
    this.settings = this.loadSettings();
  }

  private loadSettings(): PushNotificationSettings {
    // SSR対応: ブラウザ環境でのみlocalStorageを使用
    if (typeof window === 'undefined') {
      return { ...defaultSettings };
    }

    try {
      const stored = localStorage.getItem(STORAGE_KEY);
      if (stored) {
        const parsed = JSON.parse(stored);
        return { ...defaultSettings, ...parsed };
      }
    } catch (error) {
      console.error('プッシュ通知設定の読み込みに失敗:', error);
    }
    return { ...defaultSettings };
  }

  private saveSettings(): void {
    // SSR対応: ブラウザ環境でのみlocalStorageを使用
    if (typeof window === 'undefined') {
      return;
    }

    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(this.settings));
      console.log('プッシュ通知設定を保存しました:', this.settings);
    } catch (error) {
      console.error('プッシュ通知設定の保存に失敗:', error);
    }
  }

  getSettings(): PushNotificationSettings {
    return { ...this.settings };
  }

  updateSettings(updates: Partial<PushNotificationSettings>): void {
    this.settings = { ...this.settings, ...updates };
    this.saveSettings();
  }

  setEnabled(enabled: boolean): void {
    this.updateSettings({ 
      enabled,
      lastSubscriptionTime: enabled ? Date.now() : undefined
    });
  }

  setAutoSubscribe(autoSubscribe: boolean): void {
    this.updateSettings({ autoSubscribe });
  }

  setEndpoint(endpoint: string | undefined): void {
    this.updateSettings({ endpoint });
  }

  setShowTestNotifications(showTestNotifications: boolean): void {
    this.updateSettings({ showTestNotifications });
  }

  isEnabled(): boolean {
    return this.settings.enabled;
  }

  shouldAutoSubscribe(): boolean {
    return this.settings.autoSubscribe;
  }

  getEndpoint(): string | undefined {
    return this.settings.endpoint;
  }

  shouldShowTestNotifications(): boolean {
    return this.settings.showTestNotifications;
  }

  getLastSubscriptionTime(): number | undefined {
    return this.settings.lastSubscriptionTime;
  }

  // 設定をリセット
  reset(): void {
    this.settings = { ...defaultSettings };
    this.saveSettings();
  }

  // 設定のエクスポート（デバッグ用）
  export(): string {
    return JSON.stringify(this.settings, null, 2);
  }

  // 設定のインポート（デバッグ用）
  import(settingsJson: string): boolean {
    try {
      const imported = JSON.parse(settingsJson);
      this.settings = { ...defaultSettings, ...imported };
      this.saveSettings();
      return true;
    } catch (error) {
      console.error('設定のインポートに失敗:', error);
      return false;
    }
  }
}

// シングルトンインスタンス
export const pushNotificationSettings = new PushNotificationSettingsManager();