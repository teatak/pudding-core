"use client"

import { forwardRef, useCallback, useEffect, useRef, type ComponentProps, type Ref } from "react"

import { ResizableHandle } from "@/components/ui/resizable"
import { cn } from "@/lib/utils"

type WorkspaceResizableHandleProps = ComponentProps<typeof ResizableHandle> & {
  onCollapse?: () => void
}

const workspaceHandleClass =
  "z-10 w-px bg-transparent before:absolute before:inset-y-0 before:left-1/2 before:w-px before:-translate-x-1/2 before:bg-border after:absolute after:inset-y-0 after:left-1/2 after:block after:w-2.5 after:-translate-x-1/2 after:bg-transparent after:pointer-events-auto focus-visible:before:bg-muted-foreground/70 focus-visible:ring-0 focus-visible:ring-offset-0 focus-visible:[&>div]:opacity-100 aria-[orientation=horizontal]:h-px aria-[orientation=horizontal]:w-full aria-[orientation=horizontal]:before:inset-x-0 aria-[orientation=horizontal]:before:inset-y-auto aria-[orientation=horizontal]:before:top-1/2 aria-[orientation=horizontal]:before:h-px aria-[orientation=horizontal]:before:w-full aria-[orientation=horizontal]:before:translate-x-0 aria-[orientation=horizontal]:before:-translate-y-1/2 aria-[orientation=horizontal]:after:inset-x-0 aria-[orientation=horizontal]:after:inset-y-auto aria-[orientation=horizontal]:after:left-0 aria-[orientation=horizontal]:after:top-1/2 aria-[orientation=horizontal]:after:h-2.5 aria-[orientation=horizontal]:after:w-full aria-[orientation=horizontal]:after:translate-x-0 aria-[orientation=horizontal]:after:-translate-y-1/2 [&>div]:h-8 [&>div]:w-1 [&>div]:rounded-lg [&>div]:border-0 [&>div]:bg-muted-foreground/55 [&>div]:opacity-0 [&>div]:transition-opacity hover:[&>div]:bg-muted-foreground/80 hover:[&>div]:opacity-100"

function setRef<T>(ref: Ref<T> | undefined, value: T | null) {
  if (!ref) {
    return
  }
  if (typeof ref === "function") {
    ref(value)
    return
  }
  ref.current = value
}

const WorkspaceResizableHandle = forwardRef<HTMLDivElement, WorkspaceResizableHandleProps>(
  function WorkspaceResizableHandle(
    {
      className,
      elementRef,
      onCollapse,
      onPointerDownCapture,
      onPointerUpCapture,
      withHandle = true,
      ...props
    },
    ref
  ) {
    const pointerDownRef = useRef<{ x: number; y: number } | null>(null)
    const cleanupPointerUpRef = useRef<(() => void) | null>(null)
    const mergedElementRef = useCallback(
      (node: HTMLDivElement | null) => {
        setRef(ref, node)
        setRef(elementRef, node)
      },
      [elementRef, ref]
    )

    useEffect(() => () => cleanupPointerUpRef.current?.(), [])

    return (
      <ResizableHandle
        elementRef={mergedElementRef}
        className={cn(workspaceHandleClass, className)}
        withHandle={withHandle}
        onPointerDownCapture={(event) => {
          onPointerDownCapture?.(event)
          if (onCollapse && event.button === 0) {
            const pointerID = event.pointerId
            const start = { x: event.clientX, y: event.clientY }
            pointerDownRef.current = start
            cleanupPointerUpRef.current?.()

            const stop = () => {
              document.removeEventListener("pointerup", handlePointerUp, true)
              document.removeEventListener("pointercancel", stop, true)
              cleanupPointerUpRef.current = null
            }
            const handlePointerUp = (upEvent: PointerEvent) => {
              if (upEvent.pointerId !== pointerID) {
                return
              }
              stop()
              pointerDownRef.current = null
              if (Math.abs(upEvent.clientX - start.x) < 4 && Math.abs(upEvent.clientY - start.y) < 4) {
                onCollapse()
              }
            }

            document.addEventListener("pointerup", handlePointerUp, true)
            document.addEventListener("pointercancel", stop, true)
            cleanupPointerUpRef.current = stop
          }
        }}
        onPointerUpCapture={(event) => {
          onPointerUpCapture?.(event)
          pointerDownRef.current = null
        }}
        {...props}
      />
    )
  }
)

export { WorkspaceResizableHandle }
