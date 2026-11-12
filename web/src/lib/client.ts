import type { BackendError } from "./contracts";
export class APIError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
  }
}
export async function getJSON<T>(
  path: string,
  signal: AbortSignal,
): Promise<T> {
  let response: Response;
  try {
    response = await fetch(path, {
      signal: AbortSignal.any([signal, AbortSignal.timeout(7000)]),
      cache: "no-store",
    });
  } catch (e) {
    if (signal.aborted) throw e;
    throw new APIError(
      0,
      "network_timeout",
      "The request could not finish. Check the local services and retry.",
    );
  }
  const body = await response.json();
  if (!response.ok) {
    const error = (body as BackendError).error;
    throw new APIError(
      response.status,
      error?.code ?? "request_failed",
      error?.message ?? "Request failed",
    );
  }
  return body as T;
}
// Abort plus generation identity: even a transport that ignores abort cannot
// commit an old result after a newer request or component cleanup.
export class LatestRequest {
  private generation = 0;
  private controller?: AbortController;
  cancel() {
    this.controller?.abort();
    this.generation++;
  }
  async run<T>(
    load: (signal: AbortSignal) => Promise<T>,
    success: (v: T) => void,
    failure: (e: unknown) => void,
  ) {
    this.cancel();
    const generation = this.generation;
    const controller = new AbortController();
    this.controller = controller;
    try {
      const value = await load(controller.signal);
      if (generation === this.generation) success(value);
    } catch (error) {
      if (generation === this.generation && !controller.signal.aborted)
        failure(error);
    }
  }
}
