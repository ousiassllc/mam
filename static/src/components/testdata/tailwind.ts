/** Loads the real Tailwind CSS of the app (built by web-test-runner.config.mjs from the classes of the components); `restore()` unloads it. */
export async function loadTailwind(): Promise<{ restore(): void }> {
  const link = document.createElement('link');
  link.rel = 'stylesheet';
  link.href = '/__tailwind.css';
  const loaded = new Promise<void>((resolve, reject) => {
    link.onload = () => resolve();
    link.onerror = () => reject(new Error('the Tailwind CSS could not be built'));
  });
  document.head.append(link);
  await loaded;
  return { restore: () => link.remove() };
}
