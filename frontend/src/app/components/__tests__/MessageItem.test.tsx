import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';

import MessageItem from '../MessageItem';

describe('MessageItem ACP images', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders an image-only agent message in the chat', () => {
    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: '',
          images: [{ mimeType: 'image/png', data: 'iVBORw0KGgo=' }],
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    expect(screen.getByRole('img', { name: 'Message image 1' })).toHaveAttribute(
      'src',
      'data:image/png;base64,iVBORw0KGgo=',
    );
  });

  it('renders an image returned by an agent tool', () => {
    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: JSON.stringify({
            type: 'tool_use',
            name: 'GenerateImage',
            id: 'image-tool-1',
            input: {},
          }),
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
          toolUseId: 'image-tool-1',
        }}
        toolResult={{
          id: 2,
          role: 'tool_result',
          content: '',
          images: [{ mimeType: 'image/png', data: 'generated-image-data' }],
          time: '2026-07-25T00:00:01Z',
          type: 'normal',
          parentToolUseId: 'image-tool-1',
          status: 'success',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    expect(screen.getByRole('img', { name: 'Agent output image 1' })).toHaveAttribute(
      'src',
      'data:image/png;base64,generated-image-data',
    );
  });

  it('copies an agent output image to the clipboard', async () => {
    const write = vi.fn().mockResolvedValue(undefined);
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { write },
    });
    vi.stubGlobal('ClipboardItem', class {
      constructor(public items: Record<string, Blob>) {}
    });
    render(
      <MessageItem
        message={{
          id: 1,
          role: 'agent',
          content: '',
          images: [{ mimeType: 'image/png', data: 'aW1hZ2U=' }],
          time: '2026-07-25T00:00:00Z',
          type: 'normal',
        }}
        formatTimestamp={() => '00:00'}
        fontSettings={{ fontSize: 14, fontFamily: 'sans-serif' }}
      />,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Message image 1をコピー' }));

    await waitFor(() => expect(write).toHaveBeenCalledOnce());
    expect(screen.getByRole('button', { name: 'Message image 1をコピー' })).toHaveAttribute(
      'title',
      'コピーしました',
    );
  });
});
