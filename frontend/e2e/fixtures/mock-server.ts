import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';

// Mock data
const mockProfiles = [
  {
    id: 'default',
    name: 'Default Profile',
    apiKey: 'test-api-key',
    baseURL: 'http://localhost:8000',
    model: 'claude-3-5-sonnet-20241022',
    temperature: 0.7,
    maxTokens: 4096,
    isDefault: true,
    repositoryHistory: []
  }
];

const mockApiResponse = {
  id: 'msg_test123',
  type: 'message',
  role: 'assistant',
  content: [
    {
      type: 'text',
      text: 'This is a mock response from the API'
    }
  ],
  model: 'claude-3-5-sonnet-20241022',
  stop_reason: 'end_turn',
  stop_sequence: null,
  usage: {
    input_tokens: 100,
    output_tokens: 50
  }
};

// Mock handlers
export const handlers = [
  // Auth endpoints
  http.post('/api/auth/login', async ({ request }) => {
    const { apiKey } = await request.json() as { apiKey: string };
    if (apiKey === 'valid-test-key') {
      return HttpResponse.json({ success: true });
    }
    return HttpResponse.json({ error: 'Invalid API key' }, { status: 401 });
  }),

  http.get('/api/auth/status', () => {
    return HttpResponse.json({
      authenticated: true
    });
  }),

  http.post('/api/auth/logout', () => {
    return HttpResponse.json({ success: true });
  }),

  // Profile endpoints
  http.get('/api/profiles', () => {
    return HttpResponse.json(mockProfiles);
  }),

  http.post('/api/profiles', async ({ request }) => {
    const newProfile = await request.json() as Record<string, unknown>;
    return HttpResponse.json({ ...newProfile, id: 'new-profile-id' });
  }),

  // Agent API proxy
  http.post('/api/proxy/messages', async ({ request }) => {
    const body = await request.json() as any;
    
    // Simulate streaming response
    if (body.stream) {
      return new HttpResponse(
        new ReadableStream({
          start(controller) {
            controller.enqueue(new TextEncoder().encode('event: message_start\n'));
            controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ type: 'message_start', message: mockApiResponse })}\n\n`));
            
            controller.enqueue(new TextEncoder().encode('event: content_block_start\n'));
            controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ type: 'content_block_start', index: 0, content_block: { type: 'text', text: '' } })}\n\n`));
            
            const text = 'This is a mock streaming response from the API';
            for (const char of text) {
              controller.enqueue(new TextEncoder().encode('event: content_block_delta\n'));
              controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ type: 'content_block_delta', index: 0, delta: { type: 'text_delta', text: char } })}\n\n`));
            }
            
            controller.enqueue(new TextEncoder().encode('event: content_block_stop\n'));
            controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ type: 'content_block_stop', index: 0 })}\n\n`));
            
            controller.enqueue(new TextEncoder().encode('event: message_delta\n'));
            controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ type: 'message_delta', delta: { stop_reason: 'end_turn', stop_sequence: null } })}\n\n`));
            
            controller.enqueue(new TextEncoder().encode('event: message_stop\n'));
            controller.enqueue(new TextEncoder().encode(`data: ${JSON.stringify({ type: 'message_stop' })}\n\n`));
            
            controller.close();
          }
        }),
        {
          headers: {
            'Content-Type': 'text/event-stream',
            'Cache-Control': 'no-cache',
            'Connection': 'keep-alive',
          },
        }
      );
    }
    
    return HttpResponse.json(mockApiResponse);
  }),

  // Health check
  http.get('/api/proxy/health', () => {
    return HttpResponse.json({ status: 'ok' });
  }),
];

// Setup mock server
export const mockServer = setupServer(...handlers);