import * as api from "$lib/gen/api_pb.js";
import {
  toBinary,
  fromBinary,
  type DescMessage,
  type MessageShape,
  create,
  type MessageInitShape,
  toJson,
} from "@bufbuild/protobuf";
import { auth } from "../stores/auth.svelte";

let ws: WebSocket | null = null;
let connectionPromise: Promise<void> | null = null;

interface QueuedRequest {
  id: string;
  path: string;
  bodyBytes: Uint8Array; // Already encoded by the caller
  responseSchemas?: DescMessage[]; // Explicit list of schemas the caller expects back
  onNext: (payload: any) => void;
  onDone: () => void;
  onError: (err: Error) => void;
}

const requests = new Map<string, QueuedRequest>();
const eventListeners = new Map<string, Array<(data: any) => void>>();
const offlineQueue: QueuedRequest[] = [];

const pathSchemaMap: Record<string, any> = {
  "auth::handshake": api.AuthSuccessResponseSchema,
  "auth::begin": api.AuthBeginResponseSchema,
  "auth::finish": api.AuthSuccessResponseSchema,
  "auth::recovery": api.AuthSuccessResponseSchema,

  "assets::list": api.AssetListSchema,
  "assets::save": api.AssetSchema,
  "assets::delete": api.GenericIDSchema,

  "loans::list": api.LoanListSchema,
  "loans::save": api.LoanSchema,
  "loans::delete": api.GenericIDSchema,

  "incomes::list": api.IncomeListSchema,
  "incomes::save": api.IncomeSchema,
  "incomes::delete": api.GenericIDSchema,

  "bills::list": api.BillListSchema,
  "bills::save": api.BillSchema,
  "bills::delete": api.GenericIDSchema,

  "expenses::list": api.ExpenseListSchema,
  "expenses::save": api.ExpenseSchema,
  "expenses::delete": api.GenericIDSchema,

  "modifications::list": api.ModificationListSchema,
  "modifications::save": api.ModificationSchema,
  "modifications::delete": api.GenericIDSchema,

  "scenarios::list": api.ScenarioListSchema,
  "scenarios::save": api.ScenarioSchema,
  "scenarios::delete": api.GenericIDSchema,

  "virtualaccounts::list": api.VirtualAccountListSchema,
  "virtualaccounts::save": api.VirtualAccountSchema,
  "virtualaccounts::delete": api.GenericIDSchema,

  "pools::list": api.TransactionPoolListSchema,
  "pools::save": api.TransactionPoolSchema,
  "pools::delete": api.GenericIDSchema,

  "rules::list": api.TransactionRuleListSchema,
  "rules::save": api.TransactionRuleSchema,
  "rules::delete": api.GenericIDSchema,

  "tags::list": api.AvailableTagListSchema,
  "tags::save": api.AvailableTagSchema,
  "tags::delete": api.GenericIDSchema,

  "automations::list_plans": api.ExecutionPlanListSchema,
  "automations::save_plan": api.ExecutionPlanSchema,
  "automations::delete_plan": api.GenericIDSchema,
  "automations::list_connections": api.ExecutionConnectionListSchema,
  "automations::save_connection": api.ExecutionConnectionSchema,
  "automations::delete_connection": api.GenericIDSchema,
  "automations::list_logs": api.ExecutionLogListSchema,

  "integrations::list": api.IntegrationListSchema,
  "integrations::save": api.IntegrationSchema,
  "integrations::delete": api.GenericIDSchema,
  "integrations::list_accounts": api.IntegrationAccountListSchema,
};

let activeToken: string | null = null;
if (typeof window !== "undefined") {
  activeToken = localStorage.getItem("auth_token");
}

function toHexString(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

export function connect(): Promise<void> {
  if (connectionPromise) return connectionPromise;

  connectionPromise = new Promise((resolve) => {
    const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsHost =
      window.location.protocol === "https:"
        ? window.location.host
        : `${window.location.hostname}:8080`;
    const wsUrl = `${wsProtocol}//${wsHost}/api/ws`;

    console.log("[WS] Connecting to:", wsUrl);
    ws = new WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";

    ws.onopen = async () => {
      console.log("[WS] Connected successfully");

      if (activeToken) {
        console.log("[WS] Performing automated handshake");
        const [resp, error] = await wsCall(
          "auth::handshake",
          api.AuthHandshakeRequestSchema,
          { token: activeToken },
          [api.AuthSuccessResponseSchema],
        ).one();

        if (error) {
          console.log("[WS] Handshake failed:", error);
          auth.setLoading(false);
          resolve();
          return;
        }

        if (resp) {
          console.log("[WS] Handshake successful");
          auth.login(resp);
        }
      } else {
        auth.setLoading(false);
      }

      resolve();
      while (offlineQueue.length > 0) {
        const req = offlineQueue.shift();
        if (req) sendRequest(req);
      }
    };

    ws.onmessage = (event) => {
      try {
        const rawData = new Uint8Array(event.data);

        // --- 1. TRY STREAM PACKET DETECTION ---
        const streamMsg = tryProtoParse(api.WSResponseSchema, rawData);
        if (streamMsg) {
          const req = requests.get(streamMsg.id);
          if (req) {
            if (streamMsg.data) {
              let decodedPayload: any = null;
              let parseSuccessful = false;

              // Brute-force try parsing against the caller's expected schemas
              if (req.responseSchemas) {
                for (const schema of req.responseSchemas) {
                  const parsed = tryProtoParse(schema, streamMsg.data);
                  if (parsed !== null) {
                    decodedPayload = parsed;
                    parseSuccessful = true;
                    break; // Found the matching schema, exit the loop!
                  }
                }
              }

              // 2. Try the static path map
              if (!parseSuccessful && req.path) {
                const schema = pathSchemaMap[req.path];
                if (schema) {
                  const parsed = tryProtoParse(schema, streamMsg.data);
                  if (parsed !== null) {
                    decodedPayload = parsed;
                    parseSuccessful = true;
                  }
                }
              }

              // 3. Fallback: check all exported schemas to find a structural match (legacy compatibility fallback)
              if (!parseSuccessful && streamMsg.data.length > 0) {
                console.warn(`[WS] Path "${req.path}" did not resolve via responseSchemas or pathSchemaMap. Falling back to brute force parser.`);
                for (const key of Object.keys(api)) {
                  if (key.endsWith("Schema") && key !== "ErrorSchema" && key !== "WSResponseSchema") {
                    const schema = (api as any)[key];
                    const parsed = tryProtoParse(schema, streamMsg.data);
                    if (parsed !== null) {
                      decodedPayload = parsed;
                      parseSuccessful = true;
                      break; // Successfully parsed with a known schema!
                    }
                  }
                }
              }

              // Check if server sent an error (only if we haven't matched an expected schema)
              if (!parseSuccessful) {
                const parsed = tryProtoParse(api.ErrorSchema, streamMsg.data);
                if (parsed !== null && parsed.message) {
                  requests.delete(streamMsg.id);
                  req.onError(new Error(parsed.message));
                  return;
                }
              }

              if (parseSuccessful) {
                req.onNext(decodedPayload);
              } else if (streamMsg.data.length > 0) {
                // Fallback to text decoding if no schema could structurally match the chunk
                requests.delete(streamMsg.id);
                req.onError(
                  new Error("No matching schema found for stream chunk"),
                );
                return;
              }
            }

            if (streamMsg.done) {
              requests.delete(streamMsg.id);
              req.onDone();
            }
          }
          return;
        }

        // --- 2. TRY GLOBAL EVENT DETECTION ---
        const eventMsg = tryProtoParse(api.EventWrapperSchema, rawData);
        if (eventMsg && eventMsg.event) {
          const listeners = eventListeners.get(eventMsg.event);
          if (listeners) {
            for (const listener of listeners) {
              listener(eventMsg.data);
            }
          }
          return;
        }
      } catch (err) {
        console.error("[WS] General incoming message processing failed:", err);
      }
    };

    ws.onclose = () => {
      console.log("[WS] Disconnected, reconnecting in 2s...");
      connectionPromise = null;
      ws = null;
      setTimeout(connect, 2000);
      requests.clear();
    };
  });

  return connectionPromise;
}

function sendRequest(req: QueuedRequest) {
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    offlineQueue.push(req);
    return;
  }

  requests.set(req.id, req);

  const requestObj = create(api.WSRequestSchema, {
    id: req.id,
    path: req.path,
    body: req.bodyBytes,
  });

  const binary = toBinary(api.WSRequestSchema, requestObj);
  ws.send(binary);
}

export function onWsEvent<T extends DescMessage>(
  event: string,
  schema: T,
  callback: (data: MessageShape<T>) => void,
) {
  if (!eventListeners.has(event)) {
    eventListeners.set(event, []);
  }

  // Wrapper decoder so global listener hook callers get clean decoded formats automatically
  const internalDecoder = (rawBytes: Uint8Array) => {
    try {
      callback(fromBinary(schema, rawBytes));
    } catch (e) {
      console.error(`[WS] Failed to parse global event ${event}:`, e);
    }
  };

  eventListeners.get(event)?.push(internalDecoder);

  return () => {
    const listeners = eventListeners.get(event);
    if (listeners) {
      const idx = listeners.indexOf(internalDecoder);
      if (idx !== -1) listeners.splice(idx, 1);
    }
  };
}

// Define the tuple types for clean Go-style error handling
export type Tuple<T> = [data: T, error: null] | [data: null, error: Error];

/**
 * Modern fully asynchronous wrapper utility.
 * Returns an execution chain offering .one() and .many() Go-style variants.
 */
export function wsCall<T extends DescMessage, R extends DescMessage[]>(
  path: string,
  schema?: T | null | any,
  body?: MessageInitShape<T> | null | any,
  responseSchemas?: R,
): any {
  const id = "req_" + Math.random().toString(36).substring(2, 11);

  // Binary serializing handled immediately at call point using provided schema context
  let bodyBytes = new Uint8Array();
  if (body && schema) {
    const fullMessage = create(schema, body);
    bodyBytes = toBinary(schema, fullMessage);
  }

  // Return the helper execution object
  return {
    /**
     * Resolves exactly one response payload as a tuple.
     * Automatically cleans up and unregisters the request after receipt or failure.
     */
    async one(): Promise<Tuple<MessageShape<R[number]>>> {
      return new Promise<Tuple<MessageShape<R[number]>>>((resolve) => {
        let hasResolved = false;

        const timeoutId = setTimeout(() => {
          if (!hasResolved) {
            hasResolved = true;
            requests.delete(id);
            resolve([
              null,
              new Error(`Request to ${path} timed out after 15000ms`),
            ]);
          }
        }, 15000);

        sendRequest({
          id,
          path,
          bodyBytes,
          responseSchemas,
          onNext: (message) => {
            if (!hasResolved) {
              hasResolved = true;
              clearTimeout(timeoutId);
              requests.delete(id); // Clean up request track immediately
              resolve([message, null]);
            }
          },
          onDone: () => {
            if (!hasResolved) {
              hasResolved = true;
              clearTimeout(timeoutId);
              requests.delete(id);
              resolve([
                null,
                new Error("Stream closed by server before delivering data"),
              ]);
            }
          },
          onError: (err) => {
            if (!hasResolved) {
              hasResolved = true;
              clearTimeout(timeoutId);
              requests.delete(id);
              resolve([null, err]);
            }
          },
        });
      });
    },

    /**
     * Returns an AsyncIterableIterator that yields Go-style tuples for every incoming chunk.
     */
    async *many(): AsyncIterableIterator<Tuple<MessageShape<R[number]>>> {
      const incomingBuffer: MessageShape<R[number]>[] = [];
      let isDone = false;
      let streamError: Error | null = null;
      let notifyNext: (() => void) | null = null;

      sendRequest({
        id,
        path,
        bodyBytes,
        responseSchemas,
        onNext: (message) => {
          incomingBuffer.push(message);
          if (notifyNext) notifyNext();
        },
        onDone: () => {
          isDone = true;
          if (notifyNext) notifyNext();
        },
        onError: (err) => {
          streamError = err;
          if (notifyNext) notifyNext();
        },
      });

      while (!isDone || incomingBuffer.length > 0) {
        if (streamError) {
          yield [null, streamError];
          return; // Break the generator loop on error
        }

        if (incomingBuffer.length > 0) {
          yield [incomingBuffer.shift()!, null];
        } else {
          await new Promise<void>((resolve) => {
            notifyNext = resolve;
          });
        }
      }
    },
  };
}

export function initWebSocketFetch() {
  if (typeof window === "undefined") return;
  if ((window as any).__ws_fetch_initialized) return;
  (window as any).__ws_fetch_initialized = true;
  connect();
}

export function checkWebSocketSession(): Promise<null> {
  connect();
  return new Promise<null>((resolve) => setTimeout(() => resolve(null), 3000));
}

export function disconnectWebSocket() {
  if (ws) {
    ws.close();
    ws = null;
    connectionPromise = null;
  }

  activeToken = null;
}

export function reconnectWebSocket() {
  disconnectWebSocket();
  connect();
}

export function tryProtoParse<T extends DescMessage>(
  schema: T,
  binaryData: Uint8Array,
): MessageShape<T> | null {
  try {
    return fromBinary(schema, binaryData);
  } catch {
    return null;
  }
}
