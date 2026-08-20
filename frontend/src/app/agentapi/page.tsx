import { Suspense } from 'react';
import AgentAPIChat from '../components/AgentAPIChat';
import LoadingSpinner from '../components/LoadingSpinner';

export default function AgentAPIPage() {
  return (
    <div className="h-dvh bg-gray-50" style={{ position: 'relative', overflow: 'hidden' }}>
      <div className="w-full h-full flex flex-col px-0 sm:container sm:mx-auto sm:px-4" style={{ minHeight: 0 }}>
        <Suspense fallback={<LoadingSpinner />}>
          <AgentAPIChat />
        </Suspense>
      </div>
    </div>
  );
}