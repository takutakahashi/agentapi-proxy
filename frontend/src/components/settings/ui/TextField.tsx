'use client'

interface TextFieldProps {
  id?: string
  value: string
  onChange: (value: string) => void
  placeholder?: string
  type?: 'text' | 'password' | 'number'
  inputMode?: 'text' | 'numeric'
  pattern?: string
  disabled?: boolean
  className?: string
  error?: string | null
}

export function TextField({
  id,
  value,
  onChange,
  placeholder,
  type = 'text',
  inputMode,
  pattern,
  disabled,
  className = '',
  error,
}: TextFieldProps) {
  return (
    <div>
      <input
        id={id}
        type={type}
        inputMode={inputMode}
        pattern={pattern}
        value={value}
        placeholder={placeholder}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        aria-invalid={error ? true : undefined}
        className={`w-full rounded-md border bg-white dark:bg-gray-800 px-3 py-2 text-sm text-gray-900 dark:text-white focus:outline-none focus:ring-2 focus:border-transparent disabled:opacity-50 ${
          error
            ? 'border-red-400 dark:border-red-600 focus:ring-red-500'
            : 'border-gray-300 dark:border-gray-700 focus:ring-blue-500'
        } ${className}`}
      />
      {error && <p className="mt-1 text-xs text-red-600 dark:text-red-400">{error}</p>}
    </div>
  )
}
