'use client';

interface ProgressBarProps {
  percentage: number;
  status?: 'active' | 'completed' | 'failed';
  showPercentage?: boolean;
  animated?: boolean;
  size?: 'sm' | 'md' | 'lg';
}

export default function ProgressBar({
  percentage,
  status = 'active',
  showPercentage = true,
  animated = true,
  size = 'md',
}: ProgressBarProps) {
  const getBarColor = () => {
    switch (status) {
      case 'completed':
        return 'bg-green-600 dark:bg-green-500';
      case 'failed':
        return 'bg-red-600 dark:bg-red-500';
      default:
        return 'bg-blue-600 dark:bg-blue-500';
    }
  };

  const getHeight = () => {
    switch (size) {
      case 'sm':
        return 'h-1.5';
      case 'lg':
        return 'h-3';
      default:
        return 'h-2';
    }
  };

  const clampedPercentage = Math.min(100, Math.max(0, percentage));

  return (
    <div className="w-full">
      <div className={`w-full bg-gray-200 dark:bg-gray-700 rounded-full overflow-hidden ${getHeight()}`}>
        <div
          className={`${getHeight()} ${getBarColor()} rounded-full ${animated ? 'transition-all duration-500 ease-out' : ''}`}
          style={{ width: `${clampedPercentage}%` }}
          role="progressbar"
          aria-valuenow={clampedPercentage}
          aria-valuemin={0}
          aria-valuemax={100}
        />
      </div>
      {showPercentage && (
        <div className="mt-1 text-center">
          <span className="text-sm font-medium text-gray-600 dark:text-gray-400">
            {Math.round(clampedPercentage)}%
          </span>
        </div>
      )}
    </div>
  );
}
