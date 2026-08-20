'use client'

import { useState } from 'react'
import Link from 'next/link'

export default function FloatingNewSessionButton() {
  const [isHovered, setIsHovered] = useState(false)

  return (
    <Link
      href="/sessions/new"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      className={`
        fixed bottom-6 right-6 z-50
        w-14 h-14 bg-blue-600 hover:bg-blue-700
        text-white rounded-full shadow-lg hover:shadow-2xl
        transition-all duration-300 ease-in-out
        flex items-center justify-center
        ${isHovered ? 'scale-110 shadow-2xl' : 'scale-100'}
        md:hidden
        focus:outline-none focus:ring-4 focus:ring-blue-300 focus:ring-opacity-50
        active:scale-95
      `}
      aria-label="新しいセッションを開始"
      title="新しいセッションを開始"
    >
      <svg
        className="w-6 h-6 md:w-7 md:h-7"
        fill="none"
        stroke="currentColor"
        viewBox="0 0 24 24"
      >
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M12 4v16m8-8H4"
        />
      </svg>
    </Link>
  )
}
