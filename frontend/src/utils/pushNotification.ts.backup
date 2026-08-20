const VAPID_PUBLIC_KEY = process.env.NEXT_PUBLIC_VAPID_PUBLIC_KEY || 'BOv-qOWAZ4--eLYAQNk-0jZPDGHH3rrmb4RFaQglVpdz_zQrS5wH1quNS4aWoWSDnRbPO764YURRZt8_B2OMkDQ';

export class PushNotificationManager {
  private registration: ServiceWorkerRegistration | null = null;
  private subscription: PushSubscription | null = null;

  async initialize(): Promise<boolean> {
    if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
      console.warn('プッシュ通知はサポートされていません');
      return false;
    }

    try {
      // まずプッシュ通知用のService Workerを登録
      const pushSWRegistration = await navigator.serviceWorker.register('/sw-push.js', {
        scope: '/'
      });
      console.log('プッシュ通知用Service Worker登録成功:', pushSWRegistration);
      
      // プッシュ通知用のService Workerを使用
      this.registration = pushSWRegistration;
      await this.registration.update();
      
      return true;
    } catch (error) {
      console.error('Service Worker登録失敗:', error);
      return false;
    }
  }

  async requestPermission(): Promise<boolean> {
    const permission = await Notification.requestPermission();
    if (permission !== 'granted') {
      console.warn('通知許可が拒否されました');
      return false;
    }
    return true;
  }

  async subscribe(): Promise<boolean> {
    if (!this.registration) {
      console.error('Service Workerが登録されていません');
      return false;
    }

    try {
      // 既存のサブスクリプションを確認
      this.subscription = await this.registration.pushManager.getSubscription();
      
      if (!this.subscription) {
        // 新しいサブスクリプションを作成
        this.subscription = await this.registration.pushManager.subscribe({
          userVisibleOnly: true,
          applicationServerKey: this.urlBase64ToUint8Array(VAPID_PUBLIC_KEY)
        });
      }

      // サーバーにサブスクリプションを送信
      await this.sendSubscriptionToServer(this.subscription);
      console.log('プッシュサブスクリプション作成成功:', this.subscription);
      return true;
    } catch (error) {
      console.error('プッシュサブスクリプション作成失敗:', error);
      return false;
    }
  }

  async unsubscribe(): Promise<boolean> {
    if (!this.subscription) {
      console.warn('アクティブなサブスクリプションがありません');
      return false;
    }

    try {
      await this.subscription.unsubscribe();
      await this.removeSubscriptionFromServer(this.subscription);
      this.subscription = null;
      console.log('プッシュサブスクリプション削除成功');
      return true;
    } catch (error) {
      console.error('プッシュサブスクリプション削除失敗:', error);
      return false;
    }
  }

  async sendSubscriptionToServer(subscription: PushSubscription): Promise<void> {
    const response = await fetch('/api/subscribe', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(subscription.toJSON()),
    });

    if (!response.ok) {
      throw new Error('サブスクリプション送信に失敗しました');
    }
  }

  async removeSubscriptionFromServer(subscription: PushSubscription): Promise<void> {
    const response = await fetch('/api/subscribe', {
      method: 'DELETE',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ endpoint: subscription.endpoint }),
    });

    if (!response.ok) {
      throw new Error('サブスクリプション削除に失敗しました');
    }
  }

  async testNotification(title: string = 'テスト通知', body: string = 'プッシュ通知のテストです'): Promise<void> {
    const response = await fetch('/api/send-notification', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ title, body }),
    });

    if (!response.ok) {
      throw new Error('テスト通知送信に失敗しました');
    }
  }

  getSubscriptionStatus(): { isSubscribed: boolean; endpoint?: string } {
    return {
      isSubscribed: !!this.subscription,
      endpoint: this.subscription?.endpoint
    };
  }

  async sendLocalNotification(title: string, body: string): Promise<void> {
    if (!this.registration) {
      // フォールバック: ブラウザ標準通知
      if (Notification.permission === 'granted') {
        new Notification(title, { body, icon: '/icon-192x192.png' });
      }
      return;
    }

    // Service Worker経由で通知
    await this.registration.showNotification(title, {
      body,
      icon: '/icon-192x192.png',
      badge: '/icon-192x192.png',
      tag: 'agent-response'
    });
  }

  private urlBase64ToUint8Array(base64String: string): Uint8Array {
    const padding = '='.repeat((4 - base64String.length % 4) % 4);
    const base64 = (base64String + padding)
      .replace(/-/g, '+')
      .replace(/_/g, '/');

    const rawData = window.atob(base64);
    const outputArray = new Uint8Array(rawData.length);

    for (let i = 0; i < rawData.length; ++i) {
      outputArray[i] = rawData.charCodeAt(i);
    }
    return outputArray;
  }
}

export const pushNotificationManager = new PushNotificationManager();