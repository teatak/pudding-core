import { Folder, GitBranch } from "@/components/icons";

export function ProjectStartIllustration() {
  return (
    <div aria-hidden="true" className="flex items-center -space-x-3">
      <div className="grid size-14 -rotate-12 place-items-center rounded-2xl border border-amber-500/15 dark:border-amber-300/20 bg-amber-50 text-amber-600 dark:text-amber-300/85 dark:bg-[#302920]">
        <Folder className="size-6" strokeWidth={1.5} />
      </div>
      <div className="relative z-10 flex h-20 w-28 flex-col overflow-hidden rounded-2xl border border-indigo-500/20 dark:border-indigo-300/25 bg-indigo-50 dark:bg-[#282838]">
        <div className="flex h-6 shrink-0 items-center gap-1 border-b border-indigo-500/10 dark:border-indigo-300/15 px-2.5">
          <span className="size-1 rounded-full bg-indigo-400/70 dark:bg-indigo-300/75" />
          <span className="size-1 rounded-full bg-indigo-400/45 dark:bg-indigo-300/45" />
          <span className="size-1 rounded-full bg-indigo-400/25 dark:bg-indigo-300/30" />
          <span className="ml-2 h-1.5 w-8 rounded-full bg-indigo-500/15 dark:bg-indigo-300/20" />
        </div>
        <div className="flex flex-1 items-center gap-2 px-3">
          <div className="flex flex-col gap-1.5 border-r border-indigo-500/10 dark:border-indigo-300/15 pr-1.5">
            <span className="size-1 rounded-sm bg-indigo-500/20 dark:bg-indigo-300/30" />
            <span className="size-1 rounded-sm bg-indigo-500/20 dark:bg-indigo-300/30" />
            <span className="size-1 rounded-sm bg-indigo-500/20 dark:bg-indigo-300/30" />
            <span className="size-1 rounded-sm bg-indigo-500/20 dark:bg-indigo-300/30" />
          </div>
          <div className="flex flex-1 flex-col gap-1.5">
            <span className="h-1 w-8 rounded-full bg-indigo-500/50 dark:bg-indigo-300/60" />
            <span className="ml-2 h-1 w-10 rounded-full bg-teal-500/40 dark:bg-teal-300/45" />
            <span className="ml-2 h-1 w-6 rounded-full bg-indigo-500/25 dark:bg-indigo-300/30" />
            <span className="h-1 w-4 rounded-full bg-indigo-500/50 dark:bg-indigo-300/60" />
          </div>
        </div>
      </div>
      <div className="grid size-14 rotate-12 place-items-center rounded-2xl border border-emerald-500/15 dark:border-emerald-300/20 bg-emerald-50 text-emerald-600 dark:text-emerald-300/85 dark:bg-[#202e29]">
        <GitBranch className="size-6" strokeWidth={1.5} />
      </div>
    </div>
  );
}
