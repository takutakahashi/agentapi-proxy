"use client";

import React from "react";
import { OneClickPushNotifications } from "@/app/components/OneClickPushNotifications";

interface NotificationChannelSettingsProps {
  slackUserId?: string;
  notificationChannels?: string[];
  onSlackUserIdChange: (slackUserId: string) => void;
  onChannelsChange: (channels: string[]) => void;
}

// Default channels when none configured (backward compat)
const DEFAULT_CHANNELS = ["web", "slack"];

export function SlackSettings({
  slackUserId,
  notificationChannels,
  onSlackUserIdChange,
  onChannelsChange,
}: NotificationChannelSettingsProps) {
  const channels = notificationChannels ?? DEFAULT_CHANNELS;
  const webEnabled = channels.includes("web");
  const slackEnabled = channels.includes("slack");

  const handleChannelToggle = (channel: string, enabled: boolean) => {
    let newChannels: string[];
    if (enabled) {
      newChannels = [...channels.filter((c) => c !== channel), channel];
    } else {
      newChannels = channels.filter((c) => c !== channel);
      // Note: we intentionally keep Slack User ID when disabling the channel
      // so it can be re-enabled without re-entering the ID
    }
    onChannelsChange(newChannels);
  };

  return (
    <div className="space-y-6">
      <p className="text-sm text-gray-600 dark:text-gray-400">
        通知を受け取るチャネルを選択してください。
      </p>

      {/* Web Push */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={webEnabled}
            onChange={(e) => handleChannelToggle("web", e.target.checked)}
            className="w-4 h-4 text-blue-600 bg-gray-100 border-gray-300 rounded focus:ring-blue-500 dark:focus:ring-blue-600 dark:ring-offset-gray-800 focus:ring-2 dark:bg-gray-700 dark:border-gray-600"
          />
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Web プッシュ通知
          </span>
        </label>
        {webEnabled && (
          <div className="ml-7">
            <OneClickPushNotifications />
          </div>
        )}
      </div>

      {/* Slack DM */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={slackEnabled}
            onChange={(e) => handleChannelToggle("slack", e.target.checked)}
            className="w-4 h-4 text-blue-600 bg-gray-100 border-gray-300 rounded focus:ring-blue-500 dark:focus:ring-blue-600 dark:ring-offset-gray-800 focus:ring-2 dark:bg-gray-700 dark:border-gray-600"
          />
          <span className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Slack DM
          </span>
        </label>
        {slackEnabled && (
          <div className="ml-7 space-y-2">
            <p className="text-xs text-gray-500 dark:text-gray-400">
              Slack User ID を設定すると、通知を Slack DM で受け取れます。
              Slack User ID は Slack のプロフィールページから確認できます（例: U012AB3CD）。
            </p>
            <label
              htmlFor="slack-user-id"
              className="block text-xs font-medium text-gray-700 dark:text-gray-300 mb-1"
            >
              Slack User ID
            </label>
            <input
              id="slack-user-id"
              type="text"
              value={slackUserId || ""}
              onChange={(e) => onSlackUserIdChange(e.target.value)}
              placeholder="U012AB3CD"
              className="w-full px-3 py-2 border border-gray-300 dark:border-gray-600 rounded-md shadow-sm focus:outline-none focus:ring-2 focus:ring-blue-500 dark:bg-gray-700 dark:text-white text-sm font-mono"
            />
            <p className="text-xs text-gray-500 dark:text-gray-400">
              空欄にすると Slack DM 通知が送信されません。
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
