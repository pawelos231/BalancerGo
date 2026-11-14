import type { DiagnosticRequest, DiagnosticResponse } from './diagnostics.ts';

// Keep one compile running and retain only the latest draft waiting behind it.
export class LanguageDiagnosticQueue {
  private revision = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private ready: DiagnosticRequest | null = null;
  private inFlight: DiagnosticRequest | null = null;
  private disposed = false;
  private readonly send: (request: DiagnosticRequest) => void;
  private readonly publish: (source: string, response: DiagnosticResponse) => void;

  constructor(
    send: (request: DiagnosticRequest) => void,
    publish: (source: string, response: DiagnosticResponse) => void,
  ) {
    this.send = send;
    this.publish = publish;
  }

  // Replace the waiting draft and restart its 100 ms typing pause.
  check(source: string): void {
    if (this.disposed) return;
    const request = { revision: ++this.revision, source };

    if (this.timer !== null) clearTimeout(this.timer);
    this.ready = null;
    this.timer = setTimeout(() => {
      this.timer = null;
      this.ready = request;
      this.dispatch();
    }, 100);
  }

  // Finish the active compile, publish if current, then send the latest ready draft.
  receive(response: DiagnosticResponse): void {
    if (this.disposed || this.inFlight?.revision !== response.revision) return;
    const request = this.inFlight;
    this.inFlight = null;

    if (request.revision === this.revision) this.publish(request.source, response);
    this.dispatch();
  }

  // Cancel pending work and ignore all later edits and replies.
  dispose(): void {
    this.disposed = true;
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
    this.ready = null;
    this.inFlight = null;
  }

  private dispatch(): void {
    if (this.disposed || this.inFlight || !this.ready) return;
    const request = this.ready;
    this.ready = null;
    this.inFlight = request;
    this.send(request);
  }
}
