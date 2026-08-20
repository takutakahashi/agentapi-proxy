'use client';

import { useEffect, useState } from 'react';
import { SessionMessage } from '../../types/agentapi';
import { createAgentAPIProxyClientFromStorage } from '../../lib/agentapi-proxy-client';

interface ToolUseContent {
  type: 'tool_use';
  name: string;
  id: string;
  input: Record<string, unknown>;
}

function parseToolUseContent(content: string): ToolUseContent | null {
  try {
    const parsed = JSON.parse(content);
    if (parsed.type === 'tool_use' && parsed.name && parsed.id) {
      return parsed as ToolUseContent;
    }
  } catch {
    // Not a valid tool_use JSON
  }
  return null;
}

// ツール名から概要を生成
function getToolDescription(toolName: string, input: Record<string, unknown>): string {
  // descriptionがあれば優先的に使用
  const description = input.description as string | undefined;

  switch (toolName) {
    case 'Task':
      // サブエージェント名を取得
      const subagentType = input.subagent_type as string | undefined;
      if (subagentType && description) {
        return `${subagentType}: ${description}`;
      } else if (subagentType) {
        return `${subagentType}を実行中`;
      } else if (description) {
        return description;
      }
      return 'サブエージェントを実行中';
    case 'Read':
      return description || `ファイル "${input.file_path}" を読み込み中`;
    case 'Write':
      return description || `ファイル "${input.file_path}" を作成中`;
    case 'Edit':
      return description || `ファイル "${input.file_path}" を編集中`;
    case 'Bash':
      return description || 'コマンドを実行中';
    case 'Glob':
      return description || `パターン "${input.pattern}" でファイルを検索中`;
    case 'Grep':
      return description || `"${input.pattern}" を検索中`;
    case 'WebFetch':
      return description || 'Webページを取得中';
    case 'WebSearch':
      return description || `"${input.query}" を検索中`;
    default:
      return description || `${toolName}を実行中`;
  }
}

interface ToolExecutionPaneProps {
  sessionId: string;
  agentStatus?: 'stable' | 'running' | 'error';
}

export default function ToolExecutionPane({ sessionId, agentStatus }: ToolExecutionPaneProps) {
  const [runningTools, setRunningTools] = useState<Array<{ toolUse: ToolUseContent; message: SessionMessage }>>([]);
  const [isEndpointAvailable, setIsEndpointAvailable] = useState<boolean>(true);

  useEffect(() => {
    if (!sessionId || !isEndpointAvailable) {
      return;
    }

    let isMounted = true;

    const fetchToolStatus = async () => {
      try {
        const client = createAgentAPIProxyClientFromStorage();
        const result = await client.getToolStatus(sessionId);

        if (!isMounted) return;

        if (result === null) {
          // 404エラー: エンドポイントが利用できない
          setIsEndpointAvailable(false);
          setRunningTools([]);
          return;
        }

        // メッセージから実行中のツールを抽出
        const tools: Array<{ toolUse: ToolUseContent; message: SessionMessage }> = [];

        result.messages.forEach(message => {
          if (message.role === 'agent' && message.toolUseId) {
            const toolUse = parseToolUseContent(message.content);
            if (toolUse) {
              tools.push({ toolUse, message });
            }
          }
        });

        setRunningTools(tools);
      } catch (error) {
        console.error('[ToolExecutionPane] Failed to fetch tool status:', error);
      }
    };

    // 初回実行
    fetchToolStatus();

    // 2秒ごとにポーリング
    const intervalId = setInterval(fetchToolStatus, 2000);

    return () => {
      isMounted = false;
      clearInterval(intervalId);
    };
  }, [sessionId, isEndpointAvailable]);

  // エンドポイントが利用できない場合は何も表示しない
  if (!isEndpointAvailable) {
    return null;
  }

  return (
    <div className="bg-gray-50 dark:bg-gray-800 border-t border-b border-gray-200 dark:border-gray-700 flex-shrink-0">
      <div className="px-4 sm:px-6 py-2">
        <div className="flex items-center space-x-3 min-h-[20px]">
          {runningTools.length > 0 ? (
            // 実行中のツールがある場合
            runningTools.map(({ toolUse, message }) => (
              <div
                key={message.toolUseId}
                className="flex items-center space-x-2 text-xs"
              >
                <svg className="w-3 h-3 text-yellow-600 dark:text-yellow-400 animate-spin flex-shrink-0" fill="none" viewBox="0 0 24 24">
                  <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                  <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                <span className="text-gray-600 dark:text-gray-400 max-w-[300px] truncate" title={getToolDescription(toolUse.name, toolUse.input)}>
                  {getToolDescription(toolUse.name, toolUse.input)}
                </span>
              </div>
            ))
          ) : agentStatus === 'running' ? (
            // エージェントが running 状態でツールがない場合
            <div className="flex items-center space-x-2 text-xs">
              <svg className="animate-spin w-3 h-3 text-gray-400 dark:text-gray-500 flex-shrink-0" fill="none" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
              </svg>
              <span className="text-gray-400 dark:text-gray-500">
                エージェント実行中
              </span>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  );
}
