import { APIError } from "@/api/client";

type SubmitFailureCopy = {
  noModel: string;
  providerConfig: string;
  submitFailed: string;
  turnRunning: string;
};

type SubmitFailure = {
  message: string;
  surface: "conversation" | "toast";
};

const providerConfigErrorPattern = /\b(api[_\s-]?key|invalid[_\s-]?key|credential|provider|unauthori[sz]ed)\b/i;

export function getSubmitFailure(error: unknown, copy: SubmitFailureCopy): SubmitFailure {
  if (error instanceof APIError) {
    if (error.code === "turn_running") {
      return { surface: "toast", message: copy.turnRunning };
    }
    if (error.code === "no_model") {
      return { surface: "conversation", message: copy.noModel };
    }
    if (error.code === "provider_config") {
      return { surface: "conversation", message: copy.providerConfig };
    }
    if (providerConfigErrorPattern.test(error.code)) {
      return { surface: "conversation", message: error.message || copy.providerConfig };
    }
    return { surface: "toast", message: error.message || copy.submitFailed };
  }

  if (error instanceof Error) {
    if (providerConfigErrorPattern.test(error.message)) {
      return { surface: "conversation", message: error.message || copy.providerConfig };
    }
    return { surface: "toast", message: error.message || copy.submitFailed };
  }

  return { surface: "toast", message: copy.submitFailed };
}
