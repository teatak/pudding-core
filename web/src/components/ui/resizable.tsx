"use client"

import { useRef } from "react"

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

function ResizableHandle({
  withHandle,
  className,
  onCollapse,
  ...props
}: ResizablePrimitive.SeparatorProps & {
  withHandle?: boolean
  // 点击(非拖拽)分隔条时触发,用于"点 rail|会话 分隔条收起侧栏";
  // 用按下→松开的位移阈值区分点击与拖拽 resize。
  onCollapse?: () => void
}) {
  const pointerDownRef = useRef<{ x: number; y: number } | null>(null)
  return (
    <ResizablePrimitive.Separator
      data-slot="resizable-handle"
      className={cn(
        "group/handle relative flex w-px items-center justify-center bg-border ring-offset-background after:absolute after:inset-y-0 after:left-1/2 after:w-2 after:-translate-x-1/2 focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-hidden aria-[orientation=horizontal]:h-px aria-[orientation=horizontal]:w-full aria-[orientation=horizontal]:after:left-0 aria-[orientation=horizontal]:after:h-2 aria-[orientation=horizontal]:after:w-full aria-[orientation=horizontal]:after:translate-x-0 aria-[orientation=horizontal]:after:-translate-y-1/2 [&[aria-orientation=horizontal]>div]:rotate-90",
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
        <div className="z-10 flex h-6 w-1 shrink-0 rounded-lg bg-border opacity-0 transition-opacity group-hover/handle:opacity-100" />
      )}
    </ResizablePrimitive.Separator>
  )
}

export { ResizableHandle, ResizablePanel, ResizablePanelGroup }
