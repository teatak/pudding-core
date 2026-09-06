import { Globe, Search } from "@/components/icons";

export function BrowserStartIllustration() {
  return (
    <div aria-hidden="true" className="flex items-center -space-x-3">
      <div className="grid size-14 -rotate-12 place-items-center rounded-2xl border border-cyan-500/15 dark:border-cyan-300/20 bg-cyan-50 text-cyan-600 dark:text-cyan-300/85 dark:bg-[#202d32]">
        <Globe className="size-6" strokeWidth={1.5} />
      </div>
      <div className="relative z-10 flex h-20 w-28 flex-col overflow-hidden rounded-2xl border border-blue-500/20 dark:border-blue-300/25 bg-blue-50 dark:bg-[#242e3c]">
        <div className="flex h-6 shrink-0 items-center gap-1 border-b border-blue-500/10 dark:border-blue-300/15 px-2.5">
          <span className="size-1 rounded-full bg-blue-400/70 dark:bg-blue-300/75" />
          <span className="size-1 rounded-full bg-blue-400/45 dark:bg-blue-300/45" />
          <span className="size-1 rounded-full bg-blue-400/25 dark:bg-blue-300/30" />
          <span className="ml-1 h-1.5 flex-1 rounded-full bg-blue-500/10 dark:bg-blue-300/20" />
        </div>
        <div className="flex flex-1 items-center gap-2 px-3">
          <div className="h-8 w-7 shrink-0 rounded-md bg-blue-500/15 dark:bg-blue-300/20" />
          <div className="flex flex-1 flex-col gap-1.5">
            <span className="h-1.5 w-8 rounded-full bg-blue-500/45 dark:bg-blue-300/45" />
            <span className="h-1 w-full rounded-full bg-blue-500/15 dark:bg-blue-300/20" />
            <span className="h-1 w-6 rounded-full bg-blue-500/15 dark:bg-blue-300/20" />
          </div>
        </div>
      </div>
      <div className="grid size-14 rotate-12 place-items-center rounded-2xl border border-emerald-500/15 dark:border-emerald-300/20 bg-emerald-50 text-emerald-600 dark:text-emerald-300/85 dark:bg-[#202e29]">
        <Search className="size-6" strokeWidth={1.5} />
      </div>
    </div>
  );
}
