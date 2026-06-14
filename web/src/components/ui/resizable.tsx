"use client"

import { forwardRef, useRef, type ElementRef } from "react"

import * as ResizablePrimitive from "react-resizable-panels"

import { cn } from "@/lib/utils"

function ResizablePanelGroup({
  className,
  ...props
}: ResizablePrimitive.GroupProps) {
  return (
    <ResizablePrimitive.Group
      data-slot="resizable-panel-group"
      className={cn(
        "flex h-full w-full aria-[orientation=vertical]:flex-col",
        className
      )}
      {...props}
    />
  )
}

function ResizablePanel({ ...props }: ResizablePrimitive.PanelProps) {
  return <ResizablePrimitive.Panel data-slot="resizable-panel" {...props} />
}

type ResizableHandleProps = ResizablePrimitive.SeparatorProps & {
  withHandle?: boolean
  // 点击(非拖拽)分隔条时触发,用于"点 rail|会话 分隔条收起侧栏";
  // 用按下→松开的位移阈值区分点击与拖拽 resize。
  onCollapse?: () => void
}

const ResizableHandle = forwardRef<ElementRef<typeof ResizablePrimitive.Separator>, ResizableHandleProps>(function ResizableHandle({
  withHandle,
  className,
  onCollapse,
  ...props
}, ref) {
  const pointerDownRef = useRef<{ x: number; y: number } | null>(null)
  return (
    <ResizablePrimitive.Separator
      elementRef={ref}
      data-slot="resizable-handle"
      className={cn(
        "group/handle relative z-10 -mx-[5px] flex w-[10px] cursor-col-resize items-center justify-center bg-transparent ring-offset-background before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-border focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-hidden aria-[orientation=horizontal]:-my-[5px] aria-[orientation=horizontal]:mx-0 aria-[orientation=horizontal]:h-[10px] aria-[orientation=horizontal]:w-full aria-[orientation=horizontal]:cursor-row-resize aria-[orientation=horizontal]:before:inset-x-0 aria-[orientation=horizontal]:before:inset-y-auto aria-[orientation=horizontal]:before:top-1/2 aria-[orientation=horizontal]:before:h-px aria-[orientation=horizontal]:before:w-full aria-[orientation=horizontal]:before:translate-x-0 aria-[orientation=horizontal]:before:-translate-y-1/2 [&[aria-orientation=horizontal]>div]:rotate-90",
        className
      )}
      onPointerDownCapture={
        onCollapse
          ? (event) => {
              pointerDownRef.current = { x: event.clientX, y: event.clientY }
            }
          : undefined
      }
      onPointerUpCapture={
        onCollapse
          ? (event) => {
              const down = pointerDownRef.current
              pointerDownRef.current = null
              if (down && Math.abs(event.clientX - down.x) < 4 && Math.abs(event.clientY - down.y) < 4) {
                onCollapse()
              }
            }
          : undefined
      }
      {...props}
    >
      {withHandle && (
        <div className="pointer-events-none z-10 flex h-6 w-1 shrink-0 rounded-lg bg-border opacity-0 transition-opacity group-hover/handle:opacity-100" />
      )}
    </ResizablePrimitive.Separator>
  )
})

export { ResizableHandle, ResizablePanel, ResizablePanelGroup }
