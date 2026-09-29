export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

const order: Record<LogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 };

/** Minimum level printed to the browser console. Nothing is ever sent to a server. */
let threshold: LogLevel = 'info';

export function setLogLevel(level: LogLevel): void {
  threshold = level;
}

function log(level: LogLevel, scope: string, message: string, ...details: unknown[]): void {
  if (order[level] < order[threshold]) return;
  console[level](`[mam:${scope}] ${message}`, ...details);
}

export function createLogger(scope: string) {
  return {
    debug: (message: string, ...details: unknown[]) => log('debug', scope, message, ...details),
    info: (message: string, ...details: unknown[]) => log('info', scope, message, ...details),
    warn: (message: string, ...details: unknown[]) => log('warn', scope, message, ...details),
    error: (message: string, ...details: unknown[]) => log('error', scope, message, ...details),
  };
}
