// runs-fr frontend: list live runs and attach a browser terminal to a run's
// pod session.
//
// Wire protocol (mirrors the server's message-type demux in internal/term):
//   browser -> server  BINARY frame = exec stdin (keystrokes)
//   browser -> server  TEXT frame   = control JSON, today only {"resize":{cols,rows}}
//   server  -> browser BINARY frame = terminal output (exec stdout/stderr)
//
// We deliberately do NOT use @xterm/addon-attach: it has no resize control
// channel and would send keystrokes in a shape the server's text/binary demux
// does not expect. The pump below is hand-written to match the server exactly.
import { Terminal } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";

interface Run {
  id: string;
  plan: string;
  phase: string;
  branch: string;
  harness: string;
  status: string;
  podName: string;
}

const runsEl = document.getElementById("runs") as HTMLElement;
const statusEl = document.getElementById("status") as HTMLElement;
const terminalEl = document.getElementById("terminal") as HTMLElement;

const encoder = new TextEncoder();

// Live attach state, torn down whenever another run is selected.
let term: Terminal | null = null;
let fit: FitAddon | null = null;
let ws: WebSocket | null = null;
let resizeObserver: ResizeObserver | null = null;

function setStatus(text: string): void {
  statusEl.textContent = text;
}

// attachURL builds the same-origin websocket URL for a run, upgrading the
// scheme to wss when the page itself is served over https.
function attachURL(id: string): string {
  const scheme = location.protocol === "https:" ? "wss" : "ws";
  return `${scheme}://${location.host}/api/runs/${encodeURIComponent(id)}/attach`;
}

// teardown disposes the current terminal and closes the socket so selecting a
// different run never leaks a terminal, socket, or observer.
function teardown(): void {
  resizeObserver?.disconnect();
  resizeObserver = null;
  if (ws) {
    ws.onclose = null;
    ws.onerror = null;
    ws.onmessage = null;
    ws.close();
    ws = null;
  }
  term?.dispose();
  term = null;
  fit = null;
}

// sendResize tells the server the new terminal geometry as a TEXT control
// frame; the server routes it to the exec stream's TerminalSizeQueue. It is a
// no-op until the socket is open.
function sendResize(): void {
  if (!term || ws?.readyState !== WebSocket.OPEN) {
    return;
  }
  ws.send(JSON.stringify({ resize: { cols: term.cols, rows: term.rows } }));
}

function attach(run: Run): void {
  teardown();
  setStatus(`Attaching to ${run.id}…`);

  term = new Terminal({ convertEol: false, cursorBlink: true, fontSize: 13 });
  fit = new FitAddon();
  term.loadAddon(fit);
  term.open(terminalEl);
  fit.fit();

  // BINARY frames are stdin: forward every keystroke the terminal emits.
  term.onData((data) => {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(encoder.encode(data));
    }
  });

  // A fit() on resize updates term.cols/rows and fires onResize; relay it to
  // the server as a control frame. Window + container resizes both flow here.
  term.onResize(() => sendResize());
  resizeObserver = new ResizeObserver(() => fit?.fit());
  resizeObserver.observe(terminalEl);

  const socket = new WebSocket(attachURL(run.id));
  socket.binaryType = "arraybuffer";
  ws = socket;

  socket.onopen = () => {
    setStatus(`Attached to ${run.id} (${run.podName})`);
    // Send the geometry the terminal already settled on before the socket
    // opened, then keep it focused for input.
    sendResize();
    term?.focus();
  };
  // Server output arrives as binary frames; write the raw bytes to the terminal.
  socket.onmessage = (ev: MessageEvent) => {
    if (ev.data instanceof ArrayBuffer) {
      term?.write(new Uint8Array(ev.data));
    }
  };
  socket.onclose = () => setStatus(`Disconnected from ${run.id}.`);
  socket.onerror = () => setStatus(`Connection error on ${run.id}.`);
}

function renderRuns(runs: Run[]): void {
  runsEl.replaceChildren();

  const heading = document.createElement("h1");
  heading.textContent = "Live runs";
  runsEl.append(heading);

  if (runs.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "No live runs.";
    runsEl.append(empty);
    return;
  }

  for (const run of runs) {
    const btn = document.createElement("button");
    btn.type = "button";
    btn.className = "run";

    const id = document.createElement("div");
    id.className = "run-id";
    id.textContent = run.id;

    const meta = document.createElement("div");
    meta.className = "run-meta";
    meta.textContent = [run.phase, run.branch, run.status]
      .filter((s) => s)
      .join(" · ");

    btn.append(id, meta);
    btn.addEventListener("click", () => {
      for (const el of runsEl.querySelectorAll<HTMLElement>(".run")) {
        el.removeAttribute("aria-current");
      }
      btn.setAttribute("aria-current", "true");
      attach(run);
    });
    runsEl.append(btn);
  }
}

async function loadRuns(): Promise<void> {
  try {
    const resp = await fetch("/api/runs", { headers: { Accept: "application/json" } });
    if (!resp.ok) {
      throw new Error(`HTTP ${resp.status}`);
    }
    renderRuns((await resp.json()) as Run[]);
  } catch (err) {
    runsEl.replaceChildren();
    const heading = document.createElement("h1");
    heading.textContent = "Live runs";
    const e = document.createElement("div");
    e.className = "error";
    e.textContent = `Failed to load runs: ${String(err)}`;
    runsEl.append(heading, e);
  }
}

window.addEventListener("resize", () => fit?.fit());
void loadRuns();
