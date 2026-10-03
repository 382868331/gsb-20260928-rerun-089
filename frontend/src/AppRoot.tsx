import { ErrorBoundary } from './App';
import { AppInner } from './App';

export default function App() {
  return (
    <ErrorBoundary>
      <AppInner />
    </ErrorBoundary>
  );
}
