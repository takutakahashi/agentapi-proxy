'use client'

export default function Offline() {
  return (
    <div className="min-h-dvh flex items-center justify-center bg-gray-50 dark:bg-gray-900">
      <div className="max-w-md mx-auto text-center p-6">
        <div className="mb-4">
          <svg
            className="w-16 h-16 mx-auto text-gray-400 dark:text-gray-500"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M18.364 5.636l-12.728 12.728m0-12.728l12.728 12.728"
            />
          </svg>
        </div>
        <h1 className="text-2xl font-bold text-gray-900 dark:text-white mb-2">
          You&apos;re offline
        </h1>
        <p className="text-gray-600 dark:text-gray-300 mb-4">
          Please check your internet connection and try again.
        </p>
        <button
          onClick={() => window.location.reload()}
          className="bg-blue-500 hover:bg-blue-600 dark:bg-blue-600 dark:hover:bg-blue-700 text-white px-4 py-2 rounded-md transition-colors"
        >
          Try Again
        </button>
      </div>
    </div>
  )
}