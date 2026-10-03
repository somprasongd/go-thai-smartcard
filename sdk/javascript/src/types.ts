export interface CardName {
  prefix: string;
  first_name: string;
  middle_name: string;
  last_name: string;
  full_name: string;
}

export interface CardAddress {
  house_no: string;
  moo: string;
  soi: string;
  street: string;
  subdistrict: string;
  district: string;
  province: string;
  address: string;
}

export interface Personal {
  cid: string;
  name: CardName;
  name_eng: CardName;
  dob: string;
  gender: string;
  card_issuer: string;
  issue_date: string;
  expire_date: string;
  address: CardAddress;
  base64_img: string;
}

export interface CardData {
  personal: Personal | null;
  reader?: string;
  card?: { laser_id: string };
  nhso?: {
    main_inscl: string;
    sub_inscl: string;
    main_hospital: string;
    sub_hospital: string;
    paid_type: string;
    issue_date: string;
    expire_date: string;
    update_date: string;
    change_hospital_amount: string;
  };
}

export interface AgentStatus {
  readers: string[];
  selected: string;
  state: string;
  health?: string;
}

export type CommandAction = "get-status" | "refresh-readers" | "read-now";
export interface CommandResult {
  request_id: string;
  action: CommandAction;
  status: "accepted" | "completed" | "failed" | "busy";
  code?: string;
}
export type CompletedCommand = CommandResult & { status: "completed" };
export type ConnectionState = "connecting" | "connected" | "reconnecting" | "disconnected";

export type ClientErrorCode = "NOT_CONNECTED" | "DESTROYED" | "CONNECT_TIMEOUT"
  | "CONNECT_FAILED" | "CONNECTION_LOST" | "CONNECTION_CLOSED" | "COMMAND_TIMEOUT"
  | "COMMAND_FAILED" | "COMMAND_BUSY" | "PENDING_LIMIT" | "SEND_FAILED"
  | "PROTOCOL_ERROR" | "LISTENER_ERROR";

// Unknown means the agent may have finished; callers must decide whether to retry.
export class SmartCardClientError extends Error {
  constructor(
    public readonly code: ClientErrorCode,
    message: string,
    public readonly outcome: "not-sent" | "unknown" | "failed" | "busy" = "not-sent",
    public readonly result?: CommandResult,
  ) {
    super(message);
    this.name = "SmartCardClientError";
  }
}

export interface ClientEvents {
  connection: { state: ConnectionState };
  card: CardData;
  status: AgentStatus;
  inserted: { message: string };
  removed: { message: string };
  "agent-error": { message: string };
  "command-result": CommandResult;
  error: SmartCardClientError;
}

export type WebSocketLike = Pick<WebSocket,
  "readyState" | "send" | "close" | "addEventListener" | "removeEventListener">;

export interface ClientOptions {
  url: string;
  token?: string;
  connectTimeoutMs?: number;
  commandTimeoutMs?: number;
  reconnect?: boolean;
  reconnectMinDelayMs?: number;
  reconnectMaxDelayMs?: number;
  maxPendingCommands?: number;
  // Supply a compatible socket implementation for non-browser environments.
  webSocketFactory?: (url: string) => WebSocketLike;
}
