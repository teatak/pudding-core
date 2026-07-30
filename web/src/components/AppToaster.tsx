import { CircleCheckIcon, InfoIcon, OctagonXIcon, TriangleAlertIcon } from "@/components/icons";

import { Spinner } from "@/components/Spinner";
import { Toaster } from "@/components/ui/sonner";

export function AppToaster() {
  return (
    <Toaster
      icons={{
        success: <CircleCheckIcon className="size-4" />,
        info: <InfoIcon className="size-4" />,
        warning: <TriangleAlertIcon className="size-4" />,
        error: <OctagonXIcon className="size-4" />,
        loading: <Spinner className="size-4" />,
      }}
      toastOptions={{
        classNames: {
          toast: "cn-toast",
          content: "min-w-0 flex-1",
          description: "min-w-0 max-w-full",
        },
      }}
    />
  );
}
