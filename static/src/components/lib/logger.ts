/** Scoped console logging. Nothing is ever sent to a server. */
export function createLogger(scope: string) {
  return {
    warn: (message: string, ...details: unknown[]) => console.warn(`[mam:${scope}] ${message}`, ...details),
  };
}
