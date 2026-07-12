import { FitAddon } from "@xterm/addon-fit";
import { Terminal as XTerm } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef } from "react";

import { terminalWebSocketURL, type Terminal } from "@/api/client";
import { useI18n } from "@/i18n";
import { cn } from "@/lib/utils";
import { useTheme } from "@/theme/theme";
import {
  DEFAULT_TERMINAL_DIMENSIONS,
  normalizeTerminalDimensions,
  type TerminalDimensions,
} from "@/terminal/terminalDimensions";

type TerminalStatusMessage = {
  type: "status";
  status: Terminal["status"];
  exitCode?: number;
};

export function TerminalSurface({
  active,
  activeTerminalID,
  fallbackDimensions = DEFAULT_TERMINAL_DIMENSIONS,
  initialDimensionsByID = {},
  sessionID,
  terminals,
  token,
  onStatus,
}: {
  active: boolean;
  activeTerminalID?: string;
  fallbackDimensions?: TerminalDimensions;
  initialDimensionsByID?: Record<string, TerminalDimensions>;
  sessionID: string;
  terminals: Terminal[];
  token: string;
  onStatus: (terminalID: string, status: Terminal["status"], exitCode?: number) => void;
}) {
  const { resolved } = useTheme();
  return (
    <div
      aria-hidden={!active}
      className={cn(
        "absolute inset-0 z-20 overflow-hidden rounded-none",
        resolved === "dark" ? "bg-[#0d1117] text-[#d8dee9]" : "bg-white text-[#24292f]",
        !active && "pointer-events-none invisible opacity-0",
      )}
    >
      {terminals.map((item) => (
        <TerminalPane
          key={item.id}
          active={active && item.id === activeTerminalID}
          initialDimensions={initialDimensionsByID[item.id] || fallbackDimensions}
          item={item}
          resolvedTheme={resolved}
          sessionID={sessionID}
          token={token}
          onStatus={onStatus}
        />
      ))}
    </div>
  );
}

function TerminalPane({
  active,
  initialDimensions,
  item,
  resolvedTheme,
  sessionID,
  token,
  onStatus,
}: {
  active: boolean;
  initialDimensions: TerminalDimensions;
  item: Terminal;
  resolvedTheme: "light" | "dark";
  sessionID: string;
  token: string;
  onStatus: (terminalID: string, status: Terminal["status"], exitCode?: number) => void;
}) {
  const { t } = useI18n();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalRef = useRef<XTerm | null>(null);
  const fitRef = useRef<FitAddon | null>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const activeRef = useRef(active);
  const runningRef = useRef(item.status === "running");
  const exitWrittenRef = useRef(false);
  const statusCallbackRef = useRef(onStatus);
  const exitLabelRef = useRef(t("terminal.exited"));
  const initialDimensionsRef = useRef(normalizeTerminalDimensions(initialDimensions));
  activeRef.current = active;
  statusCallbackRef.current = onStatus;
  exitLabelRef.current = t("terminal.exited");

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    let disposed = false;
    let reconnectTimer = 0;
    let reconnectAttempt = 0;
    const terminal = new XTerm({
      allowProposedApi: false,
      cols: initialDimensionsRef.current.columns,
      convertEol: false,
      cursorBlink: true,
      cursorStyle: "bar",
      fontFamily: TERMINAL_FONT_FAMILY,
      fontSize: TERMINAL_FONT_SIZE,
      lineHeight: TERMINAL_LINE_HEIGHT,
      rows: initialDimensionsRef.current.rows,
      scrollback: TERMINAL_SCROLLBACK,
      theme: terminalTheme(resolvedTheme),
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(container);
    terminalRef.current = terminal;
    fitRef.current = fit;

    const writeExit = (exitCode?: number) => {
      if (exitWrittenRef.current) {
        return;
      }
      exitWrittenRef.current = true;
      const suffix = typeof exitCode === "number" ? ` (${exitCode})` : "";
      terminal.write(`\r\n\x1b[90m[${exitLabelRef.current}${suffix}]\x1b[0m\r\n`);
    };

    const sendResize = () => {
      if (!activeRef.current || disposed) {
        return;
      }
      try {
        fit.fit();
      } catch {
        return;
      }
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "resize", columns: terminal.cols, rows: terminal.rows }));
      }
    };

    const connect = () => {
      if (disposed || !runningRef.current) {
        return;
      }
      const socket = new WebSocket(terminalWebSocketURL(token, sessionID, item.id));
      socket.binaryType = "arraybuffer";
      socketRef.current = socket;
      socket.addEventListener("open", () => {
        if (reconnectAttempt > 0) {
          terminal.reset();
        }
        reconnectAttempt = 0;
        globalThis.requestAnimationFrame(sendResize);
      });
      socket.addEventListener("message", (event) => {
        if (event.data instanceof ArrayBuffer) {
          terminal.write(new Uint8Array(event.data));
          return;
        }
        if (typeof event.data !== "string") {
          return;
        }
        try {
          const message = JSON.parse(event.data) as TerminalStatusMessage;
          if (message.type !== "status") {
            return;
          }
          runningRef.current = message.status === "running";
          statusCallbackRef.current(item.id, message.status, message.exitCode);
          if (message.status === "exited") {
            writeExit(message.exitCode);
          }
        } catch {
          // Ignore unknown control messages.
        }
      });
      socket.addEventListener("close", () => {
        if (socketRef.current === socket) {
          socketRef.current = null;
        }
        if (!disposed && runningRef.current) {
          reconnectAttempt += 1;
          reconnectTimer = window.setTimeout(connect, Math.min(4000, 300 * 2 ** Math.min(reconnectAttempt, 4)));
        }
      });
    };

    const dataSubscription = terminal.onData((data) => {
      const socket = socketRef.current;
      if (socket?.readyState === WebSocket.OPEN && runningRef.current) {
        socket.send(JSON.stringify({ type: "input", data }));
      }
    });
    const resizeObserver = new ResizeObserver(() => globalThis.requestAnimationFrame(sendResize));
    resizeObserver.observe(container);

    if (runningRef.current) {
      connect();
    } else {
      writeExit(item.exitCode);
    }

    return () => {
      disposed = true;
      window.clearTimeout(reconnectTimer);
      resizeObserver.disconnect();
      dataSubscription.dispose();
      socketRef.current?.close();
      socketRef.current = null;
      terminalRef.current = null;
      fitRef.current = null;
      terminal.dispose();
    };
  }, [item.id, sessionID, token]);

  useEffect(() => {
    if (terminalRef.current) {
      terminalRef.current.options.theme = terminalTheme(resolvedTheme);
    }
  }, [resolvedTheme]);

  useEffect(() => {
    activeRef.current = active;
    if (!active) {
      return;
    }
    const frame = globalThis.requestAnimationFrame(() => {
      try {
        fitRef.current?.fit();
      } catch {
        // Hidden terminals may briefly have no measurable geometry.
      }
      terminalRef.current?.focus();
      const terminal = terminalRef.current;
      const socket = socketRef.current;
      if (terminal && socket?.readyState === WebSocket.OPEN) {
        socket.send(JSON.stringify({ type: "resize", columns: terminal.cols, rows: terminal.rows }));
      }
    });
    return () => globalThis.cancelAnimationFrame(frame);
  }, [active]);

  useEffect(() => {
    runningRef.current = item.status === "running";
    if (item.status === "exited") {
      socketRef.current?.close();
      if (!exitWrittenRef.current) {
        const suffix = typeof item.exitCode === "number" ? ` (${item.exitCode})` : "";
        terminalRef.current?.write(`\r\n\x1b[90m[${t("terminal.exited")}${suffix}]\x1b[0m\r\n`);
        exitWrittenRef.current = true;
      }
    }
  }, [item.exitCode, item.status, t]);

  return (
    <div
      aria-hidden={!active}
      className={cn("absolute inset-0 p-2", !active && "pointer-events-none invisible")}
      onMouseDown={() => terminalRef.current?.focus()}
    >
      <div ref={containerRef} className="h-full w-full overflow-hidden" />
    </div>
  );
}

export function TerminalSizeProbe({
  onDimensionsChange,
}: {
  onDimensionsChange: (dimensions: TerminalDimensions) => void;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const callbackRef = useRef(onDimensionsChange);
  callbackRef.current = onDimensionsChange;

  useEffect(() => {
    const container = containerRef.current;
    if (!container) {
      return;
    }
    const terminal = new XTerm({
      cols: DEFAULT_TERMINAL_DIMENSIONS.columns,
      cursorBlink: false,
      fontFamily: TERMINAL_FONT_FAMILY,
      fontSize: TERMINAL_FONT_SIZE,
      lineHeight: TERMINAL_LINE_HEIGHT,
      rows: DEFAULT_TERMINAL_DIMENSIONS.rows,
      scrollback: TERMINAL_SCROLLBACK,
    });
    const fit = new FitAddon();
    terminal.loadAddon(fit);
    terminal.open(container);

    let frame = 0;
    const reportDimensions = () => {
      frame = 0;
      try {
        fit.fit();
      } catch {
        return;
      }
      callbackRef.current(normalizeTerminalDimensions({ columns: terminal.cols, rows: terminal.rows }));
    };
    const scheduleReport = () => {
      globalThis.cancelAnimationFrame(frame);
      frame = globalThis.requestAnimationFrame(reportDimensions);
    };
    const observer = new ResizeObserver(scheduleReport);
    observer.observe(container);
    scheduleReport();

    return () => {
      globalThis.cancelAnimationFrame(frame);
      observer.disconnect();
      terminal.dispose();
    };
  }, []);

  return (
    <div aria-hidden="true" className="pointer-events-none invisible absolute inset-0 p-2" inert>
      <div ref={containerRef} className="h-full w-full overflow-hidden" />
    </div>
  );
}

const TERMINAL_FONT_FAMILY = '"SFMono-Regular", "Cascadia Code", "Liberation Mono", Menlo, monospace';
const TERMINAL_FONT_SIZE = 13;
const TERMINAL_LINE_HEIGHT = 1.25;
const TERMINAL_SCROLLBACK = 10_000;

function terminalTheme(resolvedTheme: "light" | "dark") {
  if (resolvedTheme === "light") {
    return {
      background: "#ffffff",
      black: "#24292f",
      blue: "#0969da",
      brightBlack: "#57606a",
      brightBlue: "#218bff",
      brightCyan: "#3192aa",
      brightGreen: "#2da44e",
      brightMagenta: "#a475f9",
      brightRed: "#cf222e",
      brightWhite: "#ffffff",
      brightYellow: "#9a6700",
      cursor: "#24292f",
      cyan: "#1b7c83",
      foreground: "#24292f",
      green: "#1a7f37",
      magenta: "#8250df",
      red: "#cf222e",
      selectionBackground: "#ddf4ff",
      white: "#6e7781",
      yellow: "#9a6700",
    };
  }
  return {
    background: "#0d1117",
    black: "#1f242c",
    blue: "#58a6ff",
    brightBlack: "#6e7681",
    brightBlue: "#79c0ff",
    brightCyan: "#56d4dd",
    brightGreen: "#7ee787",
    brightMagenta: "#d2a8ff",
    brightRed: "#ffa198",
    brightWhite: "#ffffff",
    brightYellow: "#e3b341",
    cursor: "#c9d1d9",
    cyan: "#39c5cf",
    foreground: "#c9d1d9",
    green: "#56d364",
    magenta: "#bc8cff",
    red: "#ff7b72",
    selectionBackground: "#264f78",
    white: "#b1bac4",
    yellow: "#d29922",
  };
}
