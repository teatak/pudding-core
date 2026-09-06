import { closestCenter, DndContext, getClientRect, KeyboardSensor, MouseSensor, useSensor, useSensors, type Modifier } from "@dnd-kit/core";
import { horizontalListSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS, type Transform } from "@dnd-kit/utilities";
import { useImperativeHandle, type ComponentProps, type ReactNode } from "react";
import { cn } from "@/lib/utils";

function constrainTabTransform(transform: Transform, tab: Pick<DOMRect, "left" | "right">, container: Pick<DOMRect, "left" | "right">): Transform {
  return {
    ...transform,
    y: 0,
    x: Math.max(container.left - tab.left, Math.min(transform.x, container.right - tab.right)),
  };
}
const restrictTabDrag: Modifier = ({ transform, draggingNodeRect, containerNodeRect }) => draggingNodeRect && containerNodeRect
  ? constrainTabTransform(transform, draggingNodeRect, containerNodeRect)
  : { ...transform, y: 0 };
const tabDragModifiers = [restrictTabDrag];

export function SortableTabList({ ids, onMove, children }: {
  ids: string[];
  onMove: (activeID: string, overID: string) => void;
  children: ReactNode;
}) {
  const sensors = useSensors(useSensor(MouseSensor, { activationConstraint: { distance: 6 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));
  return <DndContext collisionDetection={closestCenter} sensors={sensors} modifiers={tabDragModifiers} onDragEnd={({ active, over }) => {
    if (over && active.id !== over.id) onMove(String(active.id), String(over.id));
  }}>
    <SortableContext items={ids} strategy={horizontalListSortingStrategy}>{children}</SortableContext>
  </DndContext>;
}

export function SortableTab({ id, selected, disabled, className, children, ref, ...props }: Omit<ComponentProps<"div">, "children"> & {
  id: string;
  selected: boolean;
  disabled?: boolean;
  className?: string;
  children: (handleProps: ComponentProps<"button">) => ReactNode;
}) {
  const { attributes, listeners, setNodeRef, node, transform, transition, isDragging } = useSortable({ id, disabled });
  useImperativeHandle(ref, () => node.current as HTMLDivElement);
  // dnd-kit adds the ancestor scroll delta after context modifiers. Constrain
  // the rendered position too, so edge autoscrolling cannot clip the active tab.
  const renderedTransform = isDragging && transform && node.current?.parentElement
    ? constrainTabTransform(transform, getClientRect(node.current, { ignoreTransform: true }), node.current.parentElement.getBoundingClientRect())
    : transform;
  // Different-width tabs only translate; dnd-kit scales the drag source to the hovered rect.
  return <div {...props} ref={setNodeRef} style={{ transform: CSS.Translate.toString(renderedTransform), transition }} data-selected={selected} className={cn("pudding-workspace-content-tab group", className, isDragging && "z-20 opacity-70")}>
    {children(disabled ? {} : { ...attributes, ...listeners })}
  </div>;
}
