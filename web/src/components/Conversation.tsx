import { useEffect, useState } from "react";

import type { Session } from "@/api/client";
import { ChatColumn } from "@/components/ChatColumn";
import { Composer } from "@/components/Composer";
import { Transcript } from "@/components/Transcript";

// 会话体:收口正文(Transcript)+ 输入框(Composer)+ 顶部遮罩,三者共用 ChatColumn
// 同一条内容列,等宽对齐由结构保证。
//   - 顶部遮罩在这里(Conversation 在 header 之下,故 top-0 即贴 toolbar 下沿);
//   - 底部遮罩随 Composer(其高度随输入变化),放在 Composer 内,但宽度同样走 ChatColumn。
export function Conversation({ token, session }: { token: string; session: Session }) {
  const [submitError, setSubmitError] = useState<string | null>(null);

  useEffect(() => {
    setSubmitError(null);
  }, [session.id]);

  return (
    <div className="relative flex min-h-0 flex-1 flex-col overflow-hidden">
      {/* 标题栏与会话区的渐变衔接:只盖内容列、不铺满整宽(不挡 resize handle / 滚动条) */}
      <div className="pointer-events-none absolute inset-x-0 top-0 z-10">
        <ChatColumn>
          <div className="h-6 bg-gradient-to-b from-background to-transparent" />
        </ChatColumn>
      </div>
      <Transcript submitError={submitError} token={token} sessionID={session.id} sessionRunning={session.running} />
      <Composer token={token} session={session} onSubmitError={setSubmitError} />
    </div>
  );
}
