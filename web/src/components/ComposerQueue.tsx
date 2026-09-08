import { DndContext, KeyboardSensor, PointerSensor, closestCenter, useSensor, useSensors, type Modifier } from "@dnd-kit/core";
import { SortableContext, arrayMove, defaultAnimateLayoutChanges, sortableKeyboardCoordinates, useSortable, verticalListSortingStrategy, type AnimateLayoutChanges } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { CornerDownLeft, FileText, GripVertical, Pencil, Trash2 } from "@/components/icons";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useI18n } from "@/i18n";
import { attachmentResourceURL } from "@/lib/attachmentURL";
import { cn } from "@/lib/utils";
import type { PendingUserMessage } from "@/state/overlayStore";
import type { ReactNode } from "react";

type Props = {
  inputs: PendingUserMessage[];
  persistedIDs: Set<string>;
  busy: boolean;
  reordering: boolean;
  editingID?: string;
  token: string;
  turnID?: string;
  onEdit: (id: string) => void;
  onDelete: (id: string) => void;
  onSteer: (input: PendingUserMessage, turnID: string) => void;
  onReorder: (ids: string[]) => void;
};

// Transformed rows contribute to scrollable overflow. Keep the active row on
// the vertical axis and inside its viewport; edge auto-scroll still handles
// queues longer than the viewport without growing the scrollable content.
const constrainQueueDrag: Modifier = ({ transform, draggingNodeRect, scrollableAncestorRects }) => {
  const viewport = scrollableAncestorRects[0];
  return {
    ...transform,
    x: 0,
    y: draggingNodeRect && viewport
      ? Math.min(viewport.bottom - draggingNodeRect.bottom, Math.max(viewport.top - draggingNodeRect.top, transform.y))
      : transform.y,
  };
};
const queueDragModifiers = [constrainQueueDrag];
// Keep neighbour displacement during sorting, but commit the order without
// replaying a layout animation after the pointer/keyboard drag ends.
const animateQueueLayoutChanges: AnimateLayoutChanges = (args) => args.isSorting && defaultAnimateLayoutChanges(args);

export function ComposerQueue(props: Props) {
  const { t } = useI18n();
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 5 } }), useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }));
  if (!props.inputs.length) return null;
  const ids = props.inputs.map((input) => input.clientMessageID);
  const sortable = !props.busy && !props.editingID && props.inputs.every((input) => props.persistedIDs.has(input.clientMessageID) && input.status !== "steering");
  return (
    <section aria-label={t("composer.queueTitle")} className={cn("@container relative mx-2 -mb-3 rounded-t-2xl border border-b-0 border-border bg-muted/70 px-1 pt-1 pb-4", props.reordering && "[&_button:disabled]:opacity-100")} data-composer-queue>
      <DndContext sensors={sensors} modifiers={queueDragModifiers} collisionDetection={closestCenter} onDragEnd={({ active, over }) => {
        if (sortable && over && active.id !== over.id) props.onReorder(arrayMove(ids, ids.indexOf(String(active.id)), ids.indexOf(String(over.id))));
      }}>
        <SortableContext items={ids} strategy={verticalListSortingStrategy}>
          <div className="max-h-40 overflow-x-hidden overflow-y-auto overscroll-contain px-1 py-0.5">
            {props.inputs.map((input) => <QueueRow key={input.clientMessageID} {...props} input={input} sortable={sortable && ids.length > 1} />)}
          </div>
        </SortableContext>
      </DndContext>
    </section>
  );
}

function QueueRow({ input, sortable, ...props }: Props & { input: PendingUserMessage; sortable: boolean }) {
  const { t } = useI18n();
  const id = input.clientMessageID;
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id, disabled: !sortable, animateLayoutChanges: animateQueueLayoutChanges });
  const locked = props.busy || Boolean(props.editingID) || !props.persistedIDs.has(id) || input.status === "steering";
  const attachments = (input.parts || []).filter((part) => part.type === "attachment");
  const title = input.text || attachments.map((part) => part.name).join(", ") || (input.parts || []).flatMap((part) => part.type === "local_folder" || part.type === "project_reference" ? [part.path] : []).join(", ");
  return (
    <div ref={setNodeRef} style={{ transform: CSS.Transform.toString(transform), transition }} className={cn("relative flex min-w-0 items-center gap-1 rounded-lg px-1 py-0.5", isDragging && "z-10 bg-muted shadow-md", props.editingID === id && "bg-accent")} data-queued-input={id}>
      {/* Queue acknowledgements briefly lock sorting, but must not flash every grip. */}
      <button ref={setActivatorNodeRef} type="button" {...attributes} {...listeners} disabled={!sortable} aria-label={t("composer.queueDrag")} className="flex size-7 shrink-0 touch-none items-center justify-center rounded-md text-muted-foreground outline-none enabled:cursor-grab focus-visible:ring-2 focus-visible:ring-ring active:cursor-grabbing">
        <GripVertical className="size-3.5" />
      </button>
      {attachments.slice(0, 2).map((part) => part.mime.startsWith("image/") ? <img key={part.id} alt={part.name} src={attachmentResourceURL(part, props.token)} className="size-6 shrink-0 rounded border border-border object-cover" /> : <FileText key={part.id} aria-label={part.name} className="size-4 shrink-0 text-muted-foreground" />)}
      {attachments.length > 2 ? <span className="text-xs text-muted-foreground">+{attachments.length - 2}</span> : null}
      <span className="min-w-0 flex-1 truncate px-1 text-sm" title={title}>{title}</span>
      {input.status === "steering" ? <span className="inline-flex items-center gap-1 text-xs text-muted-foreground"><Spinner className="size-3.5" />{t("composer.queueSteering")}</span> : input.status === "editing" ? <span className="text-xs text-muted-foreground">{t("composer.queueEditing")}</span> : null}
      {props.turnID ? <QueueAction label={t("transcript.guideQueued")} disabled={locked || input.status !== "queued"} onClick={() => props.onSteer(input, props.turnID!)}><CornerDownLeft /></QueueAction> : null}
      <QueueAction label={t("transcript.editQueued")} disabled={locked} onClick={() => props.onEdit(id)}><Pencil /></QueueAction>
      <QueueAction label={t("transcript.cancelQueued")} disabled={locked} onClick={() => props.onDelete(id)}><Trash2 /></QueueAction>
    </div>
  );
}

function QueueAction({ label, disabled, onClick, children }: { label: string; disabled: boolean; onClick: () => void; children: ReactNode }) {
  return <Tooltip><TooltipTrigger asChild><Button type="button" variant="ghost" size="icon" className="size-7 shrink-0 text-muted-foreground [&_svg]:size-3.5" aria-label={label} disabled={disabled} onClick={onClick}>{children}</Button></TooltipTrigger><TooltipContent>{label}</TooltipContent></Tooltip>;
}
