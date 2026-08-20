import { Suspense } from 'react';
import SharedSessionChat from '../../components/SharedSessionChat';
import LoadingSpinner from '../../components/LoadingSpinner';

interface SharedSessionPageProps {
  params: Promise<{
    shareToken: string;
  }>;
}

export default async function SharedSessionPage({ params }: SharedSessionPageProps) {
  const { shareToken } = await params;

  return (
    <div className="h-dvh bg-gray-50" style={{ position: 'relative', overflow: 'hidden' }}>
      <div className="w-full h-full flex flex-col px-0 sm:container sm:mx-auto sm:px-4" style={{ minHeight: 0 }}>
        <Suspense fallback={<LoadingSpinner />}>
          <SharedSessionChat shareToken={shareToken} />
        </Suspense>
      </div>
    </div>
  );
}
