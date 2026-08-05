import { cn } from "@/lib/utils";

export function PuddingWordmark({ className }: { className?: string }) {
  return (
    <div
      aria-label="Pudding"
      className={cn("inline-flex items-center", className)}
      role="img"
    >
      <svg
        aria-hidden="true"
        className="size-6 shrink-0 text-[#584cff] dark:text-[#818cf8]"
        fill="currentColor"
        viewBox="0 0 120 120"
      >
        <path
          fillRule="evenodd"
          d="M39 14H65C91 14 107 29 107 51C107 73 91 88 65 88H44C42.5 88 41.5 89 40.5 90.5L29 105C27 108 25 110 22 110C19 110 17 107.5 17 104V38C17 24 27 14 39 14ZM47 35H67C78 35 85 41 85 51C85 61 78 67 67 67H47C42 67 39 64 39 59V43C39 38 42 35 47 35Z"
        />
      </svg>
      <span
        aria-hidden="true"
        className="-ml-0.5 text-xl leading-none font-semibold tracking-[-0.2px] text-sidebar-foreground"
      >
        udding
      </span>
    </div>
  );
}
