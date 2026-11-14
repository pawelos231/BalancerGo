import { useEffect, useRef, useState } from 'react';
import type { DiagnosticResponse, SourceDiagnostic } from '../scenario-language/diagnostics.ts';
import { LanguageDiagnosticQueue } from '../scenario-language/diagnosticQueue.ts';

interface CheckedSource {
  source: string;
  diagnostic: SourceDiagnostic | null;
  failure: string | null;
}

// Check drafts without blocking typing or replacing the applied scenario.
export default function useLanguageDiagnostics(source: string, enabled: boolean) {
  const queueRef = useRef<LanguageDiagnosticQueue | null>(null);
  const [checked, setChecked] = useState<CheckedSource | null>(null);
  const [workerFailure, setWorkerFailure] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let worker: Worker;
    try {
      worker = new Worker(new URL('../scenario-language/diagnostics.worker.ts', import.meta.url), {
        type: 'module',
      });
    } catch {
      setWorkerFailure('Live checks are unavailable. Use Compile and apply to validate this source.');
      return;
    }
    const queue = new LanguageDiagnosticQueue(
      request => worker.postMessage(request),
      (checkedSource, response) => {
        if (queueRef.current !== queue) return;
        setWorkerFailure(null);
        setChecked({
          source: checkedSource,
          diagnostic: response.diagnostic,
          failure: response.failure ?? null,
        });
      },
    );
    queueRef.current = queue;
    setWorkerFailure(null);

    worker.onmessage = (event: MessageEvent<DiagnosticResponse>) => {
      queue.receive(event.data);
    };
    worker.onerror = () => {
      if (queueRef.current !== queue) return;
      queueRef.current = null;
      queue.dispose();
      worker.terminate();
      setWorkerFailure('Live checks stopped working. Use Compile and apply to validate this source.');
    };

    return () => {
      if (queueRef.current === queue) queueRef.current = null;
      queue.dispose();
      worker.terminate();
    };
  }, [enabled]);

  useEffect(() => {
    if (enabled) queueRef.current?.check(source);
  }, [source, enabled]);

  // A result belongs to its exact source, including before the next effect runs.
  const current = enabled && checked?.source === source ? checked : null;
  return {
    diagnostic: current?.diagnostic ?? null,
    failure: enabled ? workerFailure ?? current?.failure ?? null : null,
  };
}
