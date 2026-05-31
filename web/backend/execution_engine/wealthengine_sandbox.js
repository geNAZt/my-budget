const fs = require("fs");

const pendingAsyncRequests = new Map(); // msg_id -> { resolve, reject }

function callRpcAsync(correlationId, method, params) {
  return new Promise((resolve, reject) => {
    if (!correlationId) {
      return reject(new Error("No active correlation ID for RPC execution context."));
    }
    const msg_id = Math.random().toString(36).substring(2);
    const request = {
      correlation_id: correlationId,
      type: "RPC_REQUEST",
      msg_id: msg_id,
      method: method,
      params: params
    };
    const requestBytes = Buffer.from(JSON.stringify(request), "utf8");
    const lengthBytes = Buffer.alloc(4);
    lengthBytes.writeUInt32BE(requestBytes.length, 0);
    
    // Register the promise
    pendingAsyncRequests.set(msg_id, { resolve, reject });
    
    // Write request to stdout
    process.stdout.write(Buffer.concat([lengthBytes, requestBytes]));
  });
}

function resolveAsyncRpc(msg_id, success, result, error) {
  const pending = pendingAsyncRequests.get(msg_id);
  if (pending) {
    pendingAsyncRequests.delete(msg_id);
    if (success) {
      pending.resolve(result);
    } else {
      pending.reject(new Error(error || "Asynchronous RPC call failed"));
    }
  }
}

class Income {
  constructor(data) {
    this.data = data || {};
  }
  plannedValue() {
    if (this.data.active_version) return this.data.active_version.amount || 0;
    if (this.data.ActiveVersion) return this.data.ActiveVersion.Amount || 0;
    return this.data.amount || this.data.Amount || 0;
  }
}

class BudgetSheet {
  constructor(state) {
    this.state = state || { incomes: [], assets: [], loans: [] };
  }
  income(name) {
    const inc = (this.state.incomes || []).find((i) => i.name === name);
    return new Income(inc);
  }
  asset(name) {
    return (this.state.assets || []).find((a) => a.Name === name || a.name === name);
  }
  loan(name) {
    return (this.state.loans || []).find((l) => l.Name === name || l.name === name);
  }
}

class RealtimeAccount {
  constructor(data, state) {
    this.data = data || {};
    this.state = state || {};
  }
  balance() {
    return this.data.amount || this.data.Amount || this.data.balance || this.data.Balance || 0;
  }
  async sync() {
    const integrationId = this.data.integration_id || this.data.IntegrationID;
    if (!integrationId) {
      throw new Error("No integration associated with this account.");
    }
    
    // Perform asynchronous RPC call to Go backend
    const res = await callRpcAsync(this.state.correlation_id, "sync", { integration_id: integrationId });
    if (res && res.realtime_accounts) {
      // Update the realtime_accounts cache in our state!
      this.state.realtime_accounts = res.realtime_accounts;
      // Also update our own data balance!
      const myKey = this.data.id || this.data.alias || this.data.name;
      if (myKey && res.realtime_accounts[myKey]) {
        this.data = res.realtime_accounts[myKey];
      }
    }
    return res;
  }
}

class Realtime {
  constructor(state) {
    this.state = state || {};
    this.accounts = this.state.realtime_accounts || {};
  }
  account(key) {
    const accData = this.accounts[key];
    return new RealtimeAccount(accData, this.state);
  }
}

class WealthEngine {
  constructor(state) {
    this.state = state || {};
  }
  currentBudgetSheet() {
    return new BudgetSheet(this.state);
  }
  realtime() {
    return new Realtime(this.state);
  }
}

module.exports = { WealthEngine, callRpcAsync, resolveAsyncRpc };
