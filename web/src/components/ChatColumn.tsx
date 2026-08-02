import type { Ref, ReactNode } from "react";

import { cn } from "@/lib/utils";

// 会话内容列:正文、输入框、上下遮罩共用的同一条居中列。
// 宽度:窄屏两边各留 20px,宽屏封顶 768px。
// 内容填满这条列,所以正文文字 / 输入框 / 遮罩天然等宽对齐——列宽只在这里
// 定义一次,改一次全齐,不会再出现"改了正文忘了遮罩 / 输入框"的零散问题。
export function ChatColumn({
  className,
  children,
  innerRef,
}: {
  className?: string;
  children?: ReactNode;
  innerRef?: Ref<HTMLDivElement>;
}) {
  return (
    <div ref={innerRef} className={cn("mx-auto w-[calc(100%-2.5rem)] min-w-0 max-w-3xl", className)}>
      {children}
    </div>
  );
}
