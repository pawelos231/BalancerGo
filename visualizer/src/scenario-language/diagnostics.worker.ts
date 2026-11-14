import { checkLanguageSource } from './diagnostics.ts';
import type { DiagnosticRequest, DiagnosticResponse } from './diagnostics.ts';

// Return the result for this revision without transferring the compiled scenario.
self.onmessage = (event: MessageEvent<DiagnosticRequest>) => {
  const { revision, source } = event.data;
  let response: DiagnosticResponse;

  try {
    response = { revision, diagnostic: checkLanguageSource(source) };
  } catch (error) {
    response = {
      revision,
      diagnostic: null,
      failure: error instanceof Error ? error.message : String(error),
    };
  }

  self.postMessage(response);
};
