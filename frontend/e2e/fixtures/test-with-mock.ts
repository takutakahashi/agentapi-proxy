import { test as base, expect } from '@playwright/test';
import { mockServer } from './mock-server';

export const test = base.extend({
  // Start mock server before each test
  page: async ({ page }, use) => {
    // Enable API mocking in browser context
    await page.route('**/api/**', async (route) => {
      const request = route.request();
      const method = request.method();
      const url = new URL(request.url());
      const pathname = url.pathname;

      // Handle different API endpoints
      if (pathname === '/api/proxy/messages' && method === 'POST') {
        // Mock streaming response
        const encoder = new TextEncoder();
        const stream = new ReadableStream({
          start(controller) {
            controller.enqueue(encoder.encode('event: message_start\n'));
            controller.enqueue(encoder.encode('data: {"type":"message_start","message":{"id":"msg_test","type":"message","role":"assistant","content":[],"model":"claude-3-5-sonnet-20241022"}}\n\n'));
            
            controller.enqueue(encoder.encode('event: content_block_start\n'));
            controller.enqueue(encoder.encode('data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}\n\n'));
            
            const text = 'This is a mock response from the API';
            for (const char of text) {
              controller.enqueue(encoder.encode('event: content_block_delta\n'));
              controller.enqueue(encoder.encode(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"${char}"}}\n\n`));
            }
            
            controller.enqueue(encoder.encode('event: content_block_stop\n'));
            controller.enqueue(encoder.encode('data: {"type":"content_block_stop","index":0}\n\n'));
            
            controller.enqueue(encoder.encode('event: message_stop\n'));
            controller.enqueue(encoder.encode('data: {"type":"message_stop"}\n\n'));
            
            controller.close();
          }
        });

        await route.fulfill({
          status: 200,
          contentType: 'text/event-stream',
          body: 'event: message_start\ndata: {"type":"message_start"}\n\nevent: content_block_delta\ndata: {"type":"content_block_delta","delta":{"type":"text_delta","text":"This is a mock response from the API"}}\n\nevent: message_stop\ndata: {"type":"message_stop"}\n\n',
        });
      } else {
        await route.continue();
      }
    });

    await use(page);
  },
});

export { expect };