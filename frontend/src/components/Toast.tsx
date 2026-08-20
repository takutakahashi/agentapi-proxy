'use client';

import React from 'react';
import { X } from 'lucide-react';
import { useToast } from '../contexts/ToastContext';

export const ToastContainer: React.FC = () => {
  const { toasts, removeToast } = useToast();

  const getToastStyles = (type?: 'info' | 'success' | 'warning' | 'error') => {
    const baseStyles = 'rounded-lg shadow-lg p-4 mb-2 flex items-start justify-between max-w-md animate-slide-in';
    
    switch (type) {
      case 'success':
        return `${baseStyles} bg-green-600 text-white`;
      case 'warning':
        return `${baseStyles} bg-yellow-500 text-white`;
      case 'error':
        return `${baseStyles} bg-red-600 text-white`;
      case 'info':
      default:
        return `${baseStyles} bg-blue-600 text-white`;
    }
  };

  if (toasts.length === 0) return null;

  return (
    <div className="fixed top-4 right-4 z-50">
      {toasts.map((toast) => (
        <div
          key={toast.id}
          className={getToastStyles(toast.type)}
        >
          <p className="flex-1 mr-2">{toast.message}</p>
          <button
            onClick={() => removeToast(toast.id)}
            className="text-white hover:text-gray-200 transition-colors"
            aria-label="閉じる"
          >
            <X size={20} />
          </button>
        </div>
      ))}
    </div>
  );
};