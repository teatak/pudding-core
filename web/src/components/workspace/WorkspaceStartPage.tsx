import type { ReactNode } from "react";

export function WorkspaceStartPage({ icon, title, children }: {
  icon: ReactNode;
  title: string;
  children?: ReactNode;
}) {
  return (
    <section aria-label={title} className="absolute inset-0 z-[1] overflow-y-auto bg-[var(--workspace-background)]">
      <div className="grid min-h-full grid-cols-1 grid-rows-[1fr_auto_1.5fr] items-center justify-items-center px-6 py-12">
        <div className="row-start-2 w-full max-w-lg text-center">
          <div aria-hidden="true" className="mb-6 flex h-20 items-center justify-center">{icon}</div>
          <h2 className="text-xl font-medium tracking-tight text-muted-foreground">{title}</h2>
          <div className="mt-7 empty:hidden">{children}</div>
        </div>
      </div>
    </section>
  );
}
