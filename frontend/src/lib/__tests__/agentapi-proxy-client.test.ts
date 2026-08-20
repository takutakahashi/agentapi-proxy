import { afterEach, describe, expect, it, vi } from 'vitest';

import { AgentAPIProxyClient, AgentAPIProxyError } from '../agentapi-proxy-client';

describe('AgentAPIProxyClient Session Runner Pools', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('lists the healthy pools available to the current user', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        session_pools: [{ name: 'native-linux', enabled: true, labels: { arch: 'amd64' } }],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.getAvailableSessionPools()).resolves.toEqual([
      { name: 'native-linux', enabled: true, labels: { arch: 'amd64' } },
    ]);
    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/available-session-pools',
      expect.any(Object),
    );
  });
});

describe('AgentAPIProxyClient ACP message history', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllEnvs();
  });

  it('resumes a session only through the explicit resume endpoint', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ session_id: 'session-1', status: 'restoring' }), {
        status: 202,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.resumeSession('session-1')).resolves.toEqual({
      session_id: 'session-1',
      status: 'restoring',
    });
    expect(fetchMock).toHaveBeenCalledWith(
      'http://proxy.example.test/sessions/session-1/resume',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('rejects when history cannot be fetched instead of returning an empty history', async () => {
    vi.spyOn(globalThis, 'fetch').mockRejectedValueOnce(new Error('connection lost'));
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await expect(client.getACPMessageHistory('session-1', 'acp-session-1')).rejects.toMatchObject({
      status: 0,
      code: 'NETWORK_ERROR',
      message: 'connection lost',
    } satisfies Partial<AgentAPIProxyError>);
  });

  it('sends text and image content blocks unchanged to an ACP session', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ ok: true }), {
        status: 202,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    await client.sendACPPrompt(
      'session-1',
      'acp-session-1',
      [
        { type: 'text', text: 'What is in this image?' },
        { type: 'image', mimeType: 'image/png', data: 'iVBORw0KGgo=' },
      ],
      42
    );

    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(JSON.parse(String(request.body))).toMatchObject({
      id: 42,
      method: 'session/prompt',
      params: {
        sessionId: 'acp-session-1',
        prompt: [
          { type: 'text', text: 'What is in this image?' },
          { type: 'image', mimeType: 'image/png', data: 'iVBORw0KGgo=' },
        ],
      },
    });
  });

  it('restores ACP image output from message history', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        messages: [{
          jsonrpc: '2.0',
          method: 'session/update',
          params: {
            sessionId: 'acp-session-1',
            time: '2026-07-25T00:00:00Z',
            update: {
              sessionUpdate: 'agent_message_chunk',
              content: { type: 'image', mimeType: 'image/png', data: 'iVBORw0KGgo=' },
            },
          },
        }],
        userPromptCount: 0,
        userPrompts: [],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      })
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    const history = await client.getACPMessageHistory('session-1', 'acp-session-1');

    expect(history.messages).toHaveLength(1);
    expect(history.messages[0].images).toEqual([
      { mimeType: 'image/png', data: 'iVBORw0KGgo=' },
    ]);
  });

  it('restores images nested in completed ACP tool results', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({
        messages: [
          {
            jsonrpc: '2.0',
            method: 'session/update',
            params: {
              sessionId: 'acp-session-1',
              update: {
                sessionUpdate: 'tool_call',
                toolCallId: 'image-tool-1',
                kind: 'other',
                title: 'Generate image',
              },
            },
          },
          {
            jsonrpc: '2.0',
            method: 'session/update',
            params: {
              sessionId: 'acp-session-1',
              update: {
                sessionUpdate: 'tool_call_update',
                toolCallId: 'image-tool-1',
                status: 'completed',
                content: [{
                  type: 'content',
                  content: {
                    type: 'image',
                    mimeType: 'image/png',
                    data: 'generated-image-data',
                    uri: '/tmp/generated.png',
                  },
                }],
              },
            },
          },
        ],
        userPromptCount: 0,
        userPrompts: [],
      }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({ baseURL: 'http://proxy.example.test' });

    const history = await client.getACPMessageHistory('session-1', 'acp-session-1');
    const toolResult = history.messages.find(message => message.role === 'tool_result');

    expect(toolResult?.images).toEqual([
      { mimeType: 'image/png', data: 'generated-image-data' },
    ]);
  });

  it('does not ask the BFF to inject the login token into a session body', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ session_id: 'session-1' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test',
      apiKey: 'login-api-key',
    });

    await client.start({ params: { message: 'hello' } });

    const request = fetchMock.mock.calls[0][1] as RequestInit;
    expect(new Headers(request.headers).get('x-inject-github-token')).toBeNull();
    expect(String(request.body)).not.toContain('login-api-key');
  });

  it('never enables debug logging in production even when requested', async () => {
    vi.stubEnv('NODE_ENV', 'production');
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => undefined);
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ name: 'alice', has_data: true }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test',
      apiKey: 'login-api-key',
      debug: true,
    });

    await client.uploadCredentials('alice', { token: 'credential-secret' });

    expect(logSpy).not.toHaveBeenCalled();
  });

  it('omits request and response secrets from opt-in development logs', async () => {
    vi.stubEnv('NODE_ENV', 'development');
    const logSpy = vi.spyOn(console, 'log').mockImplementation(() => undefined);
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
      new Response(JSON.stringify({ id: 'slackbot-1' }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    );
    const client = new AgentAPIProxyClient({
      baseURL: 'http://proxy.example.test?token=url-secret',
      apiKey: 'login-api-key',
      debug: true,
    });

    await client.createSlackBot({
      name: 'bot',
      bot_token: 'xoxb-super-secret',
      app_token: 'xapp-super-secret',
    });

    const serializedLogs = JSON.stringify(logSpy.mock.calls);
    expect(serializedLogs).not.toContain('xoxb-super-secret');
    expect(serializedLogs).not.toContain('xapp-super-secret');
    expect(serializedLogs).not.toContain('login-api-key');
    expect(serializedLogs).not.toContain('url-secret');
  });
});
