import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

// 会话内容列:正文、输入框、上下遮罩共用的同一条居中列。
// 结构:px-5 外层(窄屏两边留白)+ max-w-3xl 内层(居中,宽屏封顶 768)。
// 内容填满这条列,所以正文文字 / 输入框 / 遮罩天然等宽对齐——列宽只在这里
// 定义一次,改一次全齐,不会再出现"改了正文忘了遮罩 / 输入框"的零散问题。
export function ChatColumn({ className, children }: { className?: string; children?: ReactNode }) {
  return (
    <div className="px-5">
      <div className={cn("mx-auto w-full max-w-3xl", className)}>{children}</div>
    </div>
  );
}
