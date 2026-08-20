'use client';

import { useEffect, useRef, useCallback } from 'react';
import { AgentAPIProxyClient, ProxySessionStatusEvent } from '../../lib/agentapi-proxy-client';
import { usePageVisibility } from './usePageVisibility';

interface UseSessionsStatusStreamOptions {
  /** The AgentAPIProxyClient instance to use */
  client: AgentAPIProxyClient;
  /** Called whenever a session status change event arrives */
  onStatusChange: (event: ProxySessionStatusEvent) => void;
  /** Called when the SSE connection encounters an error */
  onError?: (error: Error) => void;
  /** Set to false to disable the stream entirely (default: true) */
  enabled?: boolean;
}

/**
 * Subscribes to the proxy-wide SSE endpoint GET /sessions/status/stream.
 * Automatically pauses when the browser tab is hidden and reconnects on
 * visibility restore or after an error.
 */
export function useSessionsStatusStream({
  client,
  onStatusChange,
  onError,
  enabled = true,
}: UseSessionsStatusStreamOptions): void {
  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Keep latest callbacks in refs so we never need to resubscribe just because
  // the caller inline-created a new arrow function.
  const onStatusChangeRef = useRef(onStatusChange);
  onStatusChangeRef.current = onStatusChange;
  const onErrorRef = useRef(onError);
  onErrorRef.current = onError;

  const isVisible = usePageVisibility();

  const disconnect = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  }, []);

  const connect = useCallback(() => {
    if (eventSourceRef.current) return; // already connected

    const es = client.subscribeToSessionsStatusEvents(
      (event) => onStatusChangeRef.current(event),
      (error) => {
        onErrorRef.current?.(error);
        // Close the broken connection and schedule a reconnect
        if (eventSourceRef.current) {
          eventSourceRef.current.close();
          eventSourceRef.current = null;
        }
        if (!reconnectTimerRef.current) {
          reconnectTimerRef.current = setTimeout(() => {
            reconnectTimerRef.current = null;
            connect();
          }, 5000);
        }
      }
    );
    eventSourceRef.current = es;
  }, [client]);

  useEffect(() => {
    if (!enabled) {
      disconnect();
      return;
    }

    if (isVisible) {
      connect();
    } else {
      // Pause when the tab is hidden to save resources; reconnect on restore.
      disconnect();
    }

    return disconnect;
  }, [enabled, isVisible, connect, disconnect]);
}
