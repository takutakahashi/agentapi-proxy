// プッシュ通知機能のみのService Worker
self.addEventListener('push', (event) => {
  console.log('Push event received:', event);
  
  if (!event.data) {
    console.log('No data in push event');
    return;
  }

  const options = {
    body: 'デフォルトのメッセージ',
    icon: '/icon-192x192.png',
    badge: '/icon-192x192.png',
    tag: 'agentapi-notification',
    renotify: true,
    requireInteraction: false,
    vibrate: [200, 100, 200],
    actions: []
  };

  try {
    const data = event.data.json();
    console.log('Push data:', data);
    
    options.body = data.body || options.body;
    options.title = data.title || 'AgentAPI UI';
    
    if (data.url) {
      options.data = { url: data.url };
    }
    if (data.icon) {
      options.icon = data.icon;
    }
    if (data.badge) {
      options.badge = data.badge;
    }
    if (data.tag) {
      options.tag = data.tag;
    }
  } catch (error) {
    console.error('プッシュデータの解析に失敗:', error);
    options.title = 'AgentAPI UI';
    options.body = event.data.text();
  }

  event.waitUntil(
    self.registration.showNotification(options.title, options)
      .then(() => console.log('Notification shown successfully'))
      .catch(error => console.error('Failed to show notification:', error))
  );
});

self.addEventListener('notificationclick', (event) => {
  console.log('Notification clicked:', event.notification);
  
  event.notification.close();

  const urlToOpen = event.notification.data?.url || '/';
  console.log('Opening URL:', urlToOpen);

  event.waitUntil(
    self.clients.matchAll({
      type: 'window',
      includeUncontrolled: true
    }).then((clientList) => {
      console.log('Found clients:', clientList.length);
      
      // 既存のウィンドウがあるかチェック
      for (const client of clientList) {
        if (client.url.includes(urlToOpen) && 'focus' in client) {
          console.log('Focusing existing client');
          return client.focus();
        }
      }
      
      // 新しいウィンドウを開く
      if (self.clients.openWindow) {
        console.log('Opening new window');
        return self.clients.openWindow(urlToOpen);
      }
    }).catch(error => {
      console.error('Error handling notification click:', error);
    })
  );
});

self.addEventListener('notificationclose', (event) => {
  console.log('通知が閉じられました:', event.notification.tag);
});

console.log('Push notification service worker loaded');