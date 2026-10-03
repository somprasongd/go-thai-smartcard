import {
  SmartCardClientError,
  type ClientOptions, type ClientEvents, type ConnectionState,
  type CommandAction, type CommandResult, type CompletedCommand, type WebSocketLike,
} from "./types.js";
export * from "./types.js";

type Pending = {
  action: CommandAction;
  resolve: (result: CompletedCommand) => void;
  reject: (error: SmartCardClientError) => void;
  timer: ReturnType<typeof setTimeout>;
};

const record = (value: unknown): value is Record<string, unknown> =>
  typeof value === "object" && value !== null && !Array.isArray(value);

function positive(value: number, name: string): number {
  if (!Number.isInteger(value) || value < 1 || value > 2_147_483_647) {
    throw new RangeError(`${name} must be a positive integer at most 2147483647`);
  }
  return value;
}

export class SmartCardClient {
  private readonly url: string;
  private readonly options: Required<Omit<ClientOptions, "url" | "token">>;
  private socket?: WebSocketLike;
  private detach?: () => void;
  private connectTimer?: ReturnType<typeof setTimeout>;
  private retryTimer?: ReturnType<typeof setTimeout>;
  private waiter?: {
    promise: Promise<void>;
    resolve: () => void;
    reject: (error: SmartCardClientError) => void;
  };
  private running = false;
  private destroyed = false;
  private attempts = 0;
  private sequence = 0;
  private readonly prefix = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`;
  private readonly pending = new Map<string, Pending>();
  private readonly listeners = new Map<keyof ClientEvents, Set<(value: never) => void>>();
  private stateValue: ConnectionState = "disconnected";

  constructor(options: ClientOptions) {
    const url = new URL(options.url);
    if (!["ws:", "wss:"].includes(url.protocol) || url.username || url.password) {
      throw new TypeError("Use an absolute ws:// or wss:// URL without user credentials");
    }
    if (options.token !== undefined) url.searchParams.set("token", options.token);
    this.url = url.href;
    this.options = {
      connectTimeoutMs: positive(options.connectTimeoutMs ?? 5_000, "connectTimeoutMs"),
      commandTimeoutMs: positive(options.commandTimeoutMs ?? 30_000, "commandTimeoutMs"),
      reconnect: options.reconnect ?? true,
      reconnectMinDelayMs: positive(options.reconnectMinDelayMs ?? 1_000, "reconnectMinDelayMs"),
      reconnectMaxDelayMs: positive(options.reconnectMaxDelayMs ?? 30_000, "reconnectMaxDelayMs"),
      maxPendingCommands: positive(options.maxPendingCommands ?? 16, "maxPendingCommands"),
      webSocketFactory: options.webSocketFactory ?? ((address) => new WebSocket(address)),
    };
    if (this.options.reconnectMaxDelayMs < this.options.reconnectMinDelayMs) {
      throw new RangeError("reconnectMaxDelayMs must be at least reconnectMinDelayMs");
    }
  }

  get state(): ConnectionState { return this.stateValue; }

  on<K extends keyof ClientEvents>(event: K, listener: (value: ClientEvents[K]) => void): () => void {
    if (this.destroyed) throw new SmartCardClientError("DESTROYED", "Client is destroyed");
    let set = this.listeners.get(event);
    if (!set) this.listeners.set(event, set = new Set());
    const callback = listener as (value: never) => void;
    set.add(callback);
    return () => { set.delete(callback); };
  }

  // Resolves on socket open, not reader readiness. Initial failure rejects even
  // when background reconnect is enabled; subscribe to connection/status first.
  connect(): Promise<void> {
    if (this.destroyed) return Promise.reject(new SmartCardClientError("DESTROYED", "Client is destroyed"));
    if (this.socket?.readyState === 1) return Promise.resolve();
    if (this.waiter) return this.waiter.promise;
    let resolve!: () => void;
    let reject!: (error: SmartCardClientError) => void;
    const promise = new Promise<void>((yes, no) => { resolve = yes; reject = no; });
    this.waiter = { promise, resolve, reject };
    this.running = true;
    clearTimeout(this.retryTimer);
    this.retryTimer = undefined;
    if (!this.socket) this.startAttempt();
    return promise;
  }

  getStatus(): Promise<CompletedCommand> { return this.command("get-status"); }
  refreshReaders(): Promise<CompletedCommand> { return this.command("refresh-readers"); }
  readNow(): Promise<CompletedCommand> { return this.command("read-now"); }

  // Stops retries but leaves subscriptions usable for a later connect().
  close(): void {
    this.running = false;
    clearTimeout(this.retryTimer);
    this.retryTimer = undefined;
    this.retireSocket();
    const error = new SmartCardClientError("CONNECTION_CLOSED", "Connection closed by client", "unknown");
    this.rejectPending(error);
    this.settleConnect(new SmartCardClientError("CONNECTION_CLOSED", "Connection closed by client"));
    this.attempts = 0;
    this.setState("disconnected");
  }

  destroy(): void {
    if (this.destroyed) return;
    this.destroyed = true;
    this.close();
    this.listeners.clear();
  }

  private command(action: CommandAction): Promise<CompletedCommand> {
    if (this.destroyed) return Promise.reject(new SmartCardClientError("DESTROYED", "Client is destroyed"));
    const socket = this.socket;
    if (!socket || socket.readyState !== 1) {
      return Promise.reject(new SmartCardClientError("NOT_CONNECTED", "Agent is not connected"));
    }
    if (this.pending.size >= this.options.maxPendingCommands) {
      return Promise.reject(new SmartCardClientError("PENDING_LIMIT", "Too many pending commands"));
    }
    const request_id = `${this.prefix}-${++this.sequence}`;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(request_id);
        reject(new SmartCardClientError("COMMAND_TIMEOUT", "Command result timed out; outcome is unknown", "unknown"));
      }, this.options.commandTimeoutMs);
      this.pending.set(request_id, { action, timer, resolve, reject });
      try {
        socket.send(JSON.stringify({ action, request_id }));
      } catch {
        clearTimeout(timer);
        this.pending.delete(request_id);
        reject(new SmartCardClientError("SEND_FAILED", "Could not send command; outcome is unknown", "unknown"));
      }
    });
  }

  private startAttempt(): void {
    if (!this.running) return;
    this.setState(this.attempts ? "reconnecting" : "connecting");
    if (!this.running || this.socket) return;
    let socket: WebSocketLike;
    try { socket = this.options.webSocketFactory(this.url); }
    catch { this.failed(new SmartCardClientError("CONNECT_FAILED", "Could not create connection")); return; }
    this.socket = socket;
    const open: EventListener = () => {
      if (this.socket !== socket) return;
      clearTimeout(this.connectTimer);
      this.connectTimer = undefined;
      this.attempts = 0;
      this.settleConnect();
      this.setState("connected");
      if (this.socket === socket) void this.getStatus().catch((error) => this.emit("error", error));
    };
    const close: EventListener = () => {
      if (this.socket !== socket) return;
      const opened = this.stateValue === "connected";
      this.failed(new SmartCardClientError(opened ? "CONNECTION_LOST" : "CONNECT_FAILED",
        opened ? "Connection lost; pending command outcomes are unknown" : "Could not connect to agent",
        opened ? "unknown" : "not-sent"));
    };
    const message: EventListener = (event) => {
      if (this.socket === socket) this.receive((event as MessageEvent).data);
    };
    // Browsers report handshake failures through error then close; the deadline
    // also covers socket implementations which never deliver close.
    const error: EventListener = () => {};
    const handlers = { open, close, message, error };
    for (const [type, handler] of Object.entries(handlers)) socket.addEventListener(type, handler);
    this.detach = () => {
      for (const [type, handler] of Object.entries(handlers)) socket.removeEventListener(type, handler);
    };
    this.connectTimer = setTimeout(() => {
      if (this.socket === socket) this.failed(new SmartCardClientError("CONNECT_TIMEOUT", "Connection timed out"));
    }, this.options.connectTimeoutMs);
  }

  private failed(error: SmartCardClientError): void {
    this.retireSocket();
    this.rejectPending(error);
    this.settleConnect(error);
    if (this.running && this.options.reconnect) {
      const cap = Math.min(this.options.reconnectMaxDelayMs,
        this.options.reconnectMinDelayMs * 2 ** Math.min(this.attempts++, 30));
      const delay = Math.max(1, Math.round(cap * (0.5 + Math.random() * 0.5)));
      this.retryTimer = setTimeout(() => {
        this.retryTimer = undefined;
        this.startAttempt();
      }, delay);
      this.setState("reconnecting");
    } else {
      this.running = false;
      this.setState("disconnected");
    }
    this.emit("error", error);
  }

  private retireSocket(): void {
    clearTimeout(this.connectTimer);
    this.connectTimer = undefined;
    this.detach?.();
    this.detach = undefined;
    const socket = this.socket;
    this.socket = undefined;
    try { socket?.close(); } catch { /* Cleanup must still settle pending work. */ }
  }

  private settleConnect(error?: SmartCardClientError): void {
    const waiter = this.waiter;
    this.waiter = undefined;
    if (error) waiter?.reject(error); else waiter?.resolve();
  }

  private rejectPending(error: SmartCardClientError): void {
    for (const entry of this.pending.values()) {
      clearTimeout(entry.timer);
      entry.reject(error);
    }
    this.pending.clear();
  }

  private receive(data: unknown): void {
    try {
      if (typeof data !== "string") throw new Error();
      const message: unknown = JSON.parse(data);
      if (!record(message) || typeof message.event !== "string") throw new Error();
      const payload = message.payload;
      switch (message.event) {
        case "smc-command-result": {
          if (!record(payload) || typeof payload.request_id !== "string"
            || typeof payload.action !== "string" || typeof payload.status !== "string"
            || !["accepted", "completed", "failed", "busy"].includes(payload.status)
            || (payload.code !== undefined && typeof payload.code !== "string")) throw new Error();
          const pending = this.pending.get(payload.request_id);
          if (!pending) return; // Late results cannot resurrect a timed-out command.
          if (payload.action !== pending.action) throw new Error();
          const result = payload as unknown as CommandResult;
          if (result.status !== "accepted") {
            clearTimeout(pending.timer);
            this.pending.delete(result.request_id);
            if (result.status === "completed") pending.resolve(result as CompletedCommand);
            else pending.reject(new SmartCardClientError(result.status === "busy" ? "COMMAND_BUSY" : "COMMAND_FAILED",
              "Agent did not complete command", result.status, result));
          }
          this.emit("command-result", result);
          break;
        }
        case "smc-data":
          if (!record(payload) || !(payload.personal === null || record(payload.personal))) throw new Error();
          this.emit("card", payload as unknown as ClientEvents["card"]);
          break;
        case "smc-status":
          if (!record(payload) || !(payload.readers === null ||
            (Array.isArray(payload.readers) && payload.readers.every((value) => typeof value === "string")))
            || typeof payload.selected !== "string" || typeof payload.state !== "string") throw new Error();
          // Go encodes a nil reader slice as null when there are no readers.
          this.emit("status", { ...payload, readers: payload.readers ?? [] } as unknown as ClientEvents["status"]);
          break;
        case "smc-inserted": case "smc-removed": case "smc-error":
          if (!record(payload) || typeof payload.message !== "string") throw new Error();
          this.emit(message.event === "smc-inserted" ? "inserted"
            : message.event === "smc-removed" ? "removed" : "agent-error", { message: payload.message });
          break;
        // Additive agent events are ignored for forward compatibility.
      }
    } catch {
      this.emit("error", new SmartCardClientError("PROTOCOL_ERROR", "Invalid agent message"));
    }
  }

  private setState(state: ConnectionState): void {
    if (this.stateValue === state) return;
    this.stateValue = state;
    this.emit("connection", { state });
  }

  private emit<K extends keyof ClientEvents>(event: K, value: ClientEvents[K]): void {
    for (const listener of [...(this.listeners.get(event) ?? [])]) {
      try { listener(value as never); }
      catch {
        if (event !== "error") this.emit("error", new SmartCardClientError("LISTENER_ERROR", "Client event listener threw"));
      }
    }
  }
}
