'use client';

import { SessionMessage } from '../../types/agentapi';

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

interface RunningToolsProps {
  messages: SessionMessage[];
}

export default function RunningTools({ messages }: RunningToolsProps) {
  // 実行中のツールを特定: tool_useがあるが、対応するtool_resultがまだないもの
  const runningTools: Array<{ toolUse: ToolUseContent; message: SessionMessage }> = [];

  // tool_resultのIDセットを作成
  const completedToolIds = new Set(
    messages
      .filter(m => m.role === 'tool_result' && m.parentToolUseId)
      .map(m => m.parentToolUseId)
  );

  // tool_useで、まだ完了していないものを探す
  messages.forEach(message => {
    if (message.role === 'agent' && message.toolUseId) {
      const toolUse = parseToolUseContent(message.content);
      if (toolUse && !completedToolIds.has(message.toolUseId)) {
        runningTools.push({ toolUse, message });
      }
    }
  });

  if (runningTools.length === 0) {
    return null;
  }

  return (
    <div className="bg-yellow-50 dark:bg-yellow-900/20 border-t border-yellow-200 dark:border-yellow-800 px-4 sm:px-6 py-3">
      <div className="flex items-start space-x-2">
        <div className="flex-shrink-0">
          <svg className="w-5 h-5 text-yellow-600 dark:text-yellow-400 animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
            <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
        </div>
        <div className="flex-1 min-w-0">
          <div className="text-sm font-medium text-yellow-700 dark:text-yellow-300 mb-2">
            Running Tools ({runningTools.length})
          </div>
          <div className="space-y-2">
            {runningTools.map(({ toolUse, message }) => (
              <div
                key={message.toolUseId}
                className="bg-white dark:bg-gray-800 rounded p-2 border border-yellow-200 dark:border-yellow-700"
              >
                <div className="flex items-center space-x-2">
                  <div className="flex-shrink-0">
                    <div className="w-2 h-2 bg-yellow-500 rounded-full animate-pulse"></div>
                  </div>
                  <div className="text-xs font-medium text-gray-900 dark:text-white">
                    {toolUse.name}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
