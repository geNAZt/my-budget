const fs = require("fs");
const path = require("path");
const { encode, decode } = require("@msgpack/msgpack");

const stateFilePath =
  process.env.WEALTHENGINE_STATE_FILE || path.join(__dirname, "state.msgpack");

let state = { incomes: [], assets: [], loans: [] };
try {
  if (fs.existsSync(stateFilePath)) {
    state = decode(fs.readFileSync(stateFilePath));
  }
} catch (e) {
  // Fallback during static checks/loading
}

class Income {
  constructor(data) {
    this.data = data || {};
  }
  plannedValue() {
    if (this.data.active_version) {
      return this.data.active_version.amount || 0;
    }
    return this.data.amount || 0;
  }
}

class BudgetSheet {
  constructor(state) {
    this.state = state;
  }
  income(name) {
    const inc = this.state.incomes.find((i) => i.name === name);
    return new Income(inc);
  }
}

class RealtimeAccount {
  constructor(data, state) {
    this.data = data || {};
    this.state = state || {};
  }
  balance() {
    return this.data.balance || 0;
  }
  async sync() {
    const integrationId = this.data.integration_id;
    const token = this.state.execution_token;
    const apiPort = this.state.api_port || "8080";
    if (!integrationId) {
      throw new Error("No integration associated with this account.");
    }
    if (!token) {
      console.log(
        `[Offline Mode] Simulated sync for integration ${integrationId}`,
      );
      return;
    }

    const http = require("http");
    return new Promise((resolve, reject) => {
      const reqData = encode({ integration_id: integrationId, token: token });
      const req = http.request(
        {
          hostname: "localhost",
          port: parseInt(apiPort),
          path: "/api/internal/sync",
          method: "POST",
          headers: {
            "Content-Type": "application/msgpack",
            "Content-Length": reqData.length,
          },
        },
        (res) => {
          let chunks = [];
          res.on("data", (chunk) => chunks.push(chunk));
          res.on("end", () => {
            if (res.statusCode >= 200 && res.statusCode < 300) {
              resolve();
            } else {
              const body = Buffer.concat(chunks).toString();
              reject(
                new Error(
                  `Sync failed with status code ${res.statusCode}: ${body}`,
                ),
              );
            }
          });
        },
      );
      req.on("error", (err) => reject(err));
      req.write(reqData);
      req.end();
    });
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
  currentBudgetSheet() {
    return new BudgetSheet(state);
  }
  realtime() {
    return new Realtime(state);
  }
}

module.exports = { WealthEngine };
