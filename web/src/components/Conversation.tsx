import type { Session } from "@/api/client";
import { TranscriptVirtualDemo } from "@/components/transcript/TranscriptVirtualDemo";

// 会话体:收口正文(Transcript)+ 输入框(Composer)+ 顶部遮罩,三者共用 ChatColumn
// 同一条内容列,等宽对齐由结构保证。
//   - 顶部遮罩在这里(Conversation 在 header 之下,故 top-0 即贴 toolbar 下沿);
//   - 底部遮罩随 Composer(其高度随输入变化),放在 Composer 内,但宽度同样走 ChatColumn。
export function Conversation({ token, session }: { token: string; session: Session }) {
  return <TranscriptVirtualDemo sessionID={session.id} sessionRunning={session.running} token={token} />;
}
