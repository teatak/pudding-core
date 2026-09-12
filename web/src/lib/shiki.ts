type HighlightJob = { code: string; lang?: string; receive: (html: string | null) => void };

let worker: Worker | undefined;
let nextID = 0;
let runningID: number | undefined;
const jobs = new Map<number, HighlightJob>();

function pump() {
  if (runningID !== undefined || jobs.size === 0) return;
  if (!worker) {
    worker = new Worker(new URL("./shiki.worker.ts", import.meta.url), { type: "module" });
    worker.onmessage = ({ data }: MessageEvent<{ id: number; html: string | null }>) => {
      const job = jobs.get(data.id);
      jobs.delete(data.id);
      runningID = undefined;
      job?.receive(data.html);
      pump();
    };
    worker.onerror = (event) => {
      console.error("Code highlighting worker failed", event.message);
      worker?.terminate();
      worker = undefined;
      const failed = [...jobs.values()];
      jobs.clear();
      runningID = undefined;
      failed.forEach((job) => job.receive(null));
    };
  }
  const [id, job] = jobs.entries().next().value!;
  runningID = id;
  worker.postMessage({ id, code: job.code, lang: job.lang });
}

// Keep at most one worker job in flight. Effect cleanup removes superseded
// queued versions before they consume CPU and ignores any already-running reply.
export function requestCodeHighlight(code: string, lang: string | undefined, receive: (html: string | null) => void) {
  const id = ++nextID;
  jobs.set(id, { code, lang, receive });
  pump();
  return () => { jobs.delete(id); };
}

if (import.meta.hot) {
  import.meta.hot.dispose(() => {
    worker?.terminate();
    jobs.clear();
  });
}
