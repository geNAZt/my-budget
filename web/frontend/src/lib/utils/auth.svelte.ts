import {
  startRegistration,
  startAuthentication,
} from "@simplewebauthn/browser";

import * as api from "$lib/gen/api_pb.js";

import { auth } from "../stores/auth.svelte";
import { checkWebSocketSession, reconnectWebSocket, wsCall } from "./ws_fetch";

export async function register(username: string) {
  try {
    const [beginResp, error] = await wsCall(
      "auth::register::begin",
      api.AuthBeginRequestSchema,
      { username },
      [api.AuthBeginResponseSchema],
    ).one();

    if (error) {
      console.log("[WS] Register begin failed:", error);
      return;
    }

    // The backend sends raw JSON bytes for WebAuthn options
    const options = JSON.parse(new TextDecoder().decode(beginResp.options));
    console.log("Registration Options from Server:", options);

    // WebAuthn library expects the inner publicKey object
    const attResp = await startRegistration({
      optionsJSON: options.publicKey ? options.publicKey : options,
    });

    const [finishResp, finishError] = await wsCall(
      "auth::register::finish",
      api.AuthFinishRequestSchema,
      {
        username: username,
        userId: beginResp.userId,
        webauthnPayload: new TextEncoder().encode(JSON.stringify(attResp)),
      },
      [api.AuthSuccessResponseSchema],
    ).one();

    if (finishError) {
      console.log("[WS] Register finish failed:", finishError);
      return;
    }

    if (finishResp.status !== "success") {
      throw new Error("Failed to finish registration");
    }

    auth.login(finishResp);
    return finishResp;
  } catch (error: any) {
    console.error("Registration error:", error);
    throw new Error(error.message || "Registration failed");
  }
}

export async function login(username: string) {
  try {
    const [beginResp, beginError] = await wsCall(
      "auth::login::begin",
      api.AuthBeginRequestSchema,
      { username },
      [api.AuthBeginResponseSchema],
    ).one();

    if (beginError) {
      console.log("[WS] Login begin failed:", beginError);
      throw new Error(beginError.message || "User not found");
    }

    console.log("Login Options from Server:", beginResp);

    const options = JSON.parse(new TextDecoder().decode(beginResp.options));
    const asResp = await startAuthentication({
      optionsJSON: options.publicKey ? options.publicKey : options,
    });

    const [finishResp, finishError] = await wsCall(
      "auth::login::finish",
      api.AuthFinishRequestSchema,
      {
        username: username,
        webauthnPayload: new TextEncoder().encode(JSON.stringify(asResp)),
      },
      [api.AuthSuccessResponseSchema],
    ).one();

    if (finishError) {
      console.log("[WS] Login finish failed:", finishError);
      throw new Error(finishError.message || "Authentication failed");
    }

    if (finishResp.status !== "success") {
      throw new Error("Failed to finish authentication");
    }

    auth.login(finishResp);
    return finishResp;
  } catch (error: any) {
    console.error("Login error:", error);
    throw new Error(error.message || "Login failed");
  }
}

export async function recoveryLogin(username: string, token: string) {
  try {
    const [resp, error] = await wsCall(
      "auth::recovery::login",
      api.AuthRecoveryRequestSchema,
      { username, token },
      [api.AuthSuccessResponseSchema],
    ).one();

    if (error) {
      console.log("[WS] Recovery login failed:", error);
      return;
    }

    if (resp.status !== "success") {
      throw new Error("Recovery login failed");
    }

    auth.login(resp);
    return resp;
  } catch (error: any) {
    console.error("Recovery login error:", error);
    throw new Error(error.message || "Recovery login failed");
  }
}

export async function upgradeSecurityKey() {
  try {
    const username = auth.user?.username;
    if (!username) throw new Error("Not logged in");

    const [beginResp, error] = await wsCall(
      "auth::register::begin_add",
      api.AuthBeginRequestSchema,
      { username },
      [api.AuthBeginResponseSchema],
    ).one();

    if (error) {
      console.log("[WS] Register begin_add failed:", error);
      return;
    }

    const options = JSON.parse(new TextDecoder().decode(beginResp.options));
    const attResp = await startRegistration({
      optionsJSON: options.publicKey ? options.publicKey : options,
    });

    const [finishResp, finishError] = await wsCall(
      "auth::register::finish_add",
      api.AuthFinishRequestSchema,
      {
        username: username,
        userId: beginResp.userId,
        webauthnPayload: new TextEncoder().encode(JSON.stringify(attResp)),
      },
      [api.AuthSuccessResponseSchema],
    ).one();

    if (finishError) {
      console.log("[WS] Register finish_add failed:", finishError);
      return;
    }

    if (finishResp.status !== "success") {
      throw new Error("Failed to finish key upgrade");
    }

    auth.login(finishResp);
    return finishResp;
  } catch (error: any) {
    console.error("Upgrade key error:", error);
    throw new Error(error.message || "Failed to upgrade key");
  }
}

export async function checkSession() {
  try {
    return await checkWebSocketSession();
  } catch {
    return null;
  }
}
