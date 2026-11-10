import { useEffect, useRef, useSyncExternalStore } from 'react';
import { ResultsHistory } from './history';
import { LocalResultsRepository, RESULTS_STORAGE_PREFIX } from './repository';
import type { ResultsSession } from './session';

// The owner stays above routes; history and unsaved candidates survive navigation.
export default function useResults(session: ResultsSession) {
  const historyRef = useRef<ResultsHistory | null>(null);
  if (!historyRef.current) {
    historyRef.current = new ResultsHistory(
      session,
      new LocalResultsRepository(() => window.localStorage),
    );
  }
  const history = historyRef.current;
  const state = useSyncExternalStore(history.subscribe, history.getSnapshot);

  useEffect(() => {
    history.start();
    const changed = (event: StorageEvent) => {
      if (event.key === null || event.key.startsWith(RESULTS_STORAGE_PREFIX)) history.refresh();
    };
    window.addEventListener('storage', changed);
    return () => {
      window.removeEventListener('storage', changed);
      history.stop();
    };
  }, [history]);

  return { state, history };
}
