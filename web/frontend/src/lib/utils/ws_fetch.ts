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
          console.log(`[WS] Received Response for ID: ${streamMsg.id}, path: ${req?.path || "unknown"}, done: ${streamMsg.done}, bytes: ${streamMsg.data ? streamMsg.data.length : 0}`);
          if (req) {
            if (streamMsg.data && streamMsg.data.length > 0) {
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

  console.log(`[WS] Sending Request to path: ${req.path}, ID: ${req.id}, bytes: ${req.bodyBytes.length}`);

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

export interface WsCallConfig {
  timeout?: number;
}

/**
 * Modern fully asynchronous wrapper utility.
 * Returns an execution chain offering .one() and .many() Go-style variants.
 */
export function wsCall<T extends DescMessage, R extends DescMessage[]>(
  path: string,
  schema?: T | null | any,
  body?: MessageInitShape<T> | null | any,
  responseSchemas?: R,
  config?: WsCallConfig,
): any {
  const id = "req_" + Math.random().toString(36).substring(2, 11);
  const timeoutMs = config?.timeout ?? 15000;

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
              new Error(`Request to ${path} timed out after ${timeoutMs}ms`),
            ]);
          }
        }, timeoutMs);

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

export function decode(obj: any): any {
  if (obj === null || obj === undefined) return obj;

  if (typeof obj === "object" && "$typeName" in obj) {
    try {
      const typeName = obj.$typeName.split(".").pop();
      const schemaName = typeName + "Schema";
      const schema = (api as any)[schemaName];
      if (schema) {
        return toJson(schema, obj);
      }
    } catch (e) {
      console.warn("[decode] Protobuf toJson fallback failed, falling back to JSON copy:", e);
    }
  }

  if (Array.isArray(obj)) {
    return obj.map(decode);
  }

  if (typeof obj === "object") {
    try {
      return structuredClone(obj);
    } catch {
      return JSON.parse(JSON.stringify(obj));
    }
  }

  return obj;
}
