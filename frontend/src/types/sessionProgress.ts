// Session creation progress types

export type SessionCreationStatus =
  | 'creating'
  | 'completed'
  | 'failed';

export interface SessionCreationProgress {
  status: SessionCreationStatus;
  message: string;
  repository?: string;
  errorMessage?: string;
  startTime: Date;
}

export interface ProgressStepConfig {
  status: SessionCreationStatus;
  label: string;
  activeLabel: string;
  completedLabel: string;
  percentage: number;
}

// ステップ設定の定数
export const SESSION_CREATION_STEPS: ProgressStepConfig[] = [
  {
    status: 'creating',
    label: 'セッション作成',
    activeLabel: 'セッション作成中...',
    completedLabel: '作成完了',
    percentage: 50,
  },
];

// ステータスに応じたパーセンテージを取得
export function getProgressPercentage(status: SessionCreationStatus): number {
  if (status === 'completed') return 100;
  if (status === 'failed') {
    // 失敗した場合は、その時点のパーセンテージを返す
    return 0;
  }
  const step = SESSION_CREATION_STEPS.find((s) => s.status === status);
  return step?.percentage ?? 0;
}

// ステータスに応じたステップインデックスを取得
export function getStepIndex(status: SessionCreationStatus): number {
  if (status === 'completed') return SESSION_CREATION_STEPS.length;
  if (status === 'failed') return -1;
  return SESSION_CREATION_STEPS.findIndex((s) => s.status === status);
}
