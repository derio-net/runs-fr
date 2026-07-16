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
  // kind is "node" for a node login shell (owner-only, token-gated) and empty
  // for an ordinary run terminal; nodeName is the bare node name for the former.
  kind?: string;
  nodeName?: string;
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
let selectedRun: Run | null = null;
let attachGeneration = 0;
let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
let reconnectAttempt = 0;
// While the tab is hidden we do not burn reconnect attempts; a pending retry is
// parked here and fired the moment the tab becomes visible again.
let pendingReconnect: { run: Run; generation: number } | null = null;

// Reconnects back off indefinitely (capped) rather than giving up after a fixed
// budget: a spec-b agent turn can run for many minutes, far longer than any
// small attempt ceiling, and the tab staying open is the real signal to keep
// trying. The manual "Reconnect" button forces an immediate retry.
const maxReconnectDelay = 10000;

function setStatus(text: string): void {
  statusEl.textContent = text;
}

// getAttachURL builds the same-origin websocket URL for a run, upgrading the
// scheme to wss when the page itself is served over https. For a NODE shell the
// attach is gated by a single-use, owner-bound token, so a fresh token is minted
// on every (re)connect attempt — a URL can never be replayed under the
// single-use rule, which is exactly why reconnect must re-mint rather than reuse
// the last URL. Ordinary run terminals need no token.
async function getAttachURL(run: Run): Promise<string> {
  const scheme = location.protocol === "https:" ? "wss" : "ws";
  const base = `${scheme}://${location.host}/api/runs/${encodeURIComponent(run.id)}/attach`;
  if (run.kind !== "node") {
    return base;
  }
  const name = run.nodeName ?? run.id.replace(/^node-/, "");
  const resp = await fetch(`/api/nodes/${encodeURIComponent(name)}/attach-token`, {
    method: "POST",
    headers: { Accept: "application/json" },
  });
  if (!resp.ok) {
    throw new Error(`attach-token HTTP ${resp.status}`);
  }
  const { token } = (await resp.json()) as { token: string };
  return `${base}?token=${encodeURIComponent(token)}`;
}

// The manual reconnect affordance, created lazily and shown only while a run is
// disconnected (or backing off). Clicking it forces an immediate retry.
let reconnectButton: HTMLButtonElement | null = null;

function hideReconnectButton(): void {
  if (reconnectButton) {
    reconnectButton.hidden = true;
    reconnectButton.onclick = null;
  }
}

function showReconnectButton(run: Run, generation: number): void {
  if (!reconnectButton) {
    reconnectButton = document.createElement("button");
    reconnectButton.type = "button";
    reconnectButton.className = "reconnect";
    reconnectButton.textContent = "Reconnect";
    statusEl.insertAdjacentElement("afterend", reconnectButton);
  }
  reconnectButton.hidden = false;
  reconnectButton.onclick = () => {
    // Stale generation (a different run is now selected) → the openSocket guard
    // makes this a safe no-op.
    if (reconnectTimer) {
      clearTimeout(reconnectTimer);
      reconnectTimer = null;
    }
    pendingReconnect = null;
    reconnectAttempt = 0;
    hideReconnectButton();
    term?.reset();
    void openSocket(run, generation);
  };
}

// teardown disposes the current terminal and closes the socket so selecting a
// different run never leaks a terminal, socket, or observer.
function teardown(): void {
  attachGeneration++;
  selectedRun = null;
  reconnectAttempt = 0;
  pendingReconnect = null;
  hideReconnectButton();
  if (reconnectTimer) {
    clearTimeout(reconnectTimer);
    reconnectTimer = null;
  }
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

// scheduleReconnect backs off (capped, no attempt ceiling) and retries while the
// tab is visible, parking the attempt until visible when it is hidden. It always
// offers the manual Reconnect button and resets the terminal before a
// reconnected attach draws a fresh tmux screen.
function scheduleReconnect(run: Run, generation: number, statusText: string): void {
  if (generation !== attachGeneration || selectedRun !== run) {
    return;
  }
  setStatus(statusText);
  showReconnectButton(run, generation);

  if (document.visibilityState === "hidden") {
    // Do not burn retries on a backgrounded tab; the visibilitychange handler
    // resumes immediately when the tab is shown again.
    pendingReconnect = { run, generation };
    return;
  }

  const delay = Math.min(maxReconnectDelay, 250 * 2 ** reconnectAttempt);
  reconnectAttempt++;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    // Fresh session: clear the stale buffer so the reconnected attach's full
    // redraw is not overlaid on the pre-disconnect screen.
    term?.reset();
    void openSocket(run, generation);
  }, delay);
}

async function openSocket(run: Run, generation: number): Promise<void> {
  if (generation !== attachGeneration || selectedRun !== run) {
    return;
  }

  let url: string;
  try {
    url = await getAttachURL(run);
  } catch (err) {
    // Re-check after the await: the user may have selected another run.
    if (generation !== attachGeneration || selectedRun !== run) {
      return;
    }
    scheduleReconnect(run, generation, `Failed to prepare attach for ${run.id}: ${String(err)}`);
    return;
  }
  // The token mint / URL build is async; re-validate the generation before we
  // commit a socket to shared state.
  if (generation !== attachGeneration || selectedRun !== run) {
    return;
  }

  const socket = new WebSocket(url);
  socket.binaryType = "arraybuffer";
  ws = socket;

  socket.onopen = () => {
    if (generation !== attachGeneration || ws !== socket) {
      socket.close();
      return;
    }
    reconnectAttempt = 0;
    pendingReconnect = null;
    hideReconnectButton();
    setStatus(`Attached to ${run.id} (${run.podName})`);
    // Send the geometry the terminal already settled on before the socket
    // opened, then keep it focused for input.
    sendResize();
    term?.focus();
  };
  // Server output arrives as binary frames; write the raw bytes to the terminal.
  socket.onmessage = (ev: MessageEvent) => {
    if (generation === attachGeneration && ev.data instanceof ArrayBuffer) {
      term?.write(new Uint8Array(ev.data));
    }
  };
  socket.onclose = () => {
    if (generation !== attachGeneration || ws !== socket || selectedRun !== run) {
      return;
    }
    ws = null;
    scheduleReconnect(run, generation, `Disconnected from ${run.id}; reconnecting…`);
  };
  socket.onerror = () => {
    if (generation === attachGeneration && ws === socket) {
      setStatus(`Connection error on ${run.id}; reconnecting…`);
    }
  };
}

function attach(run: Run): void {
  teardown();
  selectedRun = run;
  const generation = attachGeneration;
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

  void openSocket(run, generation);
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

// A single module-global resize listener keeps the active terminal fitted. It
// is intentionally not per-attach (unlike the ResizeObserver, which is torn
// down per run): `fit` is null between and after sessions, so the optional-chain
// guard makes this a safe no-op until a run is attached.
window.addEventListener("resize", () => fit?.fit());

// A tab that was hidden parks its reconnect; fire it the moment it is shown
// again so a long-lived attach comes back immediately instead of on the next
// backoff tick (or never, for a tab hidden across the whole outage).
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState !== "visible" || !pendingReconnect) {
    return;
  }
  const { run, generation } = pendingReconnect;
  pendingReconnect = null;
  if (generation !== attachGeneration || selectedRun !== run) {
    return;
  }
  const delay = Math.min(maxReconnectDelay, 250 * 2 ** reconnectAttempt);
  reconnectAttempt++;
  reconnectTimer = setTimeout(() => {
    reconnectTimer = null;
    term?.reset();
    void openSocket(run, generation);
  }, delay);
});

void loadRuns();
