'use client';

type StepStatus = 'pending' | 'active' | 'completed' | 'failed';

interface ProgressStepProps {
  number: number;
  label: string;
  status: StepStatus;
  description?: string;
  showConnector?: boolean;
}

export default function ProgressStep({
  number,
  label,
  status,
  description,
  showConnector = false,
}: ProgressStepProps) {
  const getIconContent = () => {
    switch (status) {
      case 'completed':
        return (
          <svg className="w-4 h-4 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
          </svg>
        );
      case 'active':
        return (
          <svg className="w-4 h-4 text-white animate-spin" fill="none" viewBox="0 0 24 24">
            <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
            <path
              className="opacity-75"
              fill="currentColor"
              d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"
            />
          </svg>
        );
      case 'failed':
        return (
          <svg className="w-4 h-4 text-white" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
          </svg>
        );
      default:
        return <span className="text-sm font-medium text-gray-500 dark:text-gray-400">{number}</span>;
    }
  };

  const getIconStyle = () => {
    switch (status) {
      case 'completed':
        return 'bg-green-600 dark:bg-green-500';
      case 'active':
        return 'bg-blue-600 dark:bg-blue-500';
      case 'failed':
        return 'bg-red-600 dark:bg-red-500';
      default:
        return 'border-2 border-gray-300 dark:border-gray-600 bg-white dark:bg-gray-800';
    }
  };

  const getTextStyle = () => {
    switch (status) {
      case 'completed':
        return 'text-green-600 dark:text-green-400';
      case 'active':
        return 'text-blue-600 dark:text-blue-400 font-medium';
      case 'failed':
        return 'text-red-600 dark:text-red-400';
      default:
        return 'text-gray-500 dark:text-gray-400';
    }
  };

  const getConnectorStyle = () => {
    switch (status) {
      case 'completed':
        return 'bg-green-600 dark:bg-green-500';
      case 'active':
        return 'bg-blue-600 dark:bg-blue-500';
      default:
        return 'bg-gray-300 dark:bg-gray-600';
    }
  };

  return (
    <div className="flex items-start">
      <div className="flex flex-col items-center mr-4">
        <div
          className={`w-8 h-8 rounded-full flex items-center justify-center ${getIconStyle()}`}
        >
          {getIconContent()}
        </div>
        {showConnector && (
          <div className={`w-0.5 h-8 ${getConnectorStyle()} mt-2`} />
        )}
      </div>
      <div className="flex-1 pt-1">
        <p className={`text-sm ${getTextStyle()}`}>{label}</p>
        {description && (
          <p className="text-xs text-gray-500 dark:text-gray-400 mt-0.5">{description}</p>
        )}
      </div>
    </div>
  );
}
