const fs = require("fs");
const path = require("path");
const { exec } = require("child_process");
const util = require("util");
const execAsync = util.promisify(exec);
const vm = require("vm");
const protobuf = require("protobufjs");

const executionEngineDir = __dirname;

// Load Protobuf definitions
const root = protobuf.loadSync(path.join(executionEngineDir, "proto", "execution.proto"));
const StdioFrame = root.lookupType("execution.StdioFrame");

// In-memory persistent cache for compiled VM scripts
const compiledPlans = new Map(); // maps plan_id -> { script, code_hash }

// Stdin buffer chunk accumulator
let buffer = Buffer.alloc(0);

process.stdin.on("data", (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  processBuffer();
});

process.stdin.on("end", () => {
  // Daemon standard stdio closed - exit
  process.exit(0);
});

async function processBuffer() {
  try {
    while (buffer.length >= 4) {
      // Read 4-byte big-endian length prefix
      const length = buffer.readUInt32BE(0);
      if (buffer.length >= 4 + length) {
        // Slice the complete frame
        const frameBytes = buffer.subarray(4, 4 + length);
        buffer = buffer.subarray(4 + length);

        // Handle request frame concurrently
        handleFrame(frameBytes);
      } else {
        break;
      }
    }
  } catch (err) {
    sendErrorResponse(
      null,
      `[Daemon Stream Error] Stream processing failed: ${err.message}`,
    );
  }
}

async function handleFrame(frameBytes) {
  let correlation_id = null;
  try {
    const frame = StdioFrame.decode(frameBytes);
    
    // Handle RPC response from Go backend for asynchronous calls
    if (frame.rpc_response) {
      const resp = frame.rpc_response;
      const { resolveAsyncRpc } = require("./wealthengine_sandbox");
      const result = resp.result_json ? JSON.parse(Buffer.from(resp.result_json).toString("utf8")) : null;
      resolveAsyncRpc(resp.msg_id, resp.success, result, resp.error);
      return;
    }

    // Handle check_compiled RPC request from Go backend
    if (frame.rpc_request && frame.rpc_request.method === "check_compiled") {
      const req = frame.rpc_request;
      const params = req.params_json ? JSON.parse(Buffer.from(req.params_json).toString("utf8")) : {};
      const { plan_id, code_hash } = params;
      const cached = plan_id ? compiledPlans.get(plan_id) : null;
      const compiled = !!(cached && cached.code_hash === code_hash);
      
      const responseFrame = StdioFrame.create({
        rpc_response: {
          correlation_id: req.correlation_id,
          msg_id: req.msg_id,
          success: true,
          result_json: Buffer.from(JSON.stringify({ compiled }), "utf8")
        }
      });
      writeFrame(StdioFrame.encode(responseFrame).finish());
      return;
    }

    if (!frame.execute_request) {
      // Ignore other frames or unknown messages
      return;
    }

    const request = frame.execute_request;
    correlation_id = request.correlation_id;
    const plan_id = request.plan_id;
    const code = request.code;
    const code_hash = request.code_hash;
    const state = request.state_json ? JSON.parse(Buffer.from(request.state_json).toString("utf8")) : {};
    const secrets = request.secrets || {};
    const trigger = request.trigger ? {
      type: request.trigger.type,
      data: request.trigger.data_json ? JSON.parse(Buffer.from(request.trigger.data_json).toString("utf8")) : {}
    } : { type: "CRON", data: {} };

    // Check persistent in-memory compilation cache
    let cached = plan_id ? compiledPlans.get(plan_id) : null;
    let script;

    // Compile only if we don't have a cache or if code has changed
    if (!cached || cached.code_hash !== code_hash) {
      if (!code) {
        sendErrorResponse(
          correlation_id,
          `[Runner Error] Script execution failed: No pre-compiled script cached for plan ${plan_id} and no code provided.`,
        );
        return;
      }

      // Ensure esbuild and dependency files are fully available
      await ensureDependenciesInstalled();

      const esbuild = require("esbuild");

      // A. Dynamically install missing packages inside execution_engine directory
      const depRegex =
        /\/\*\*\s*depend\s+ on\s+([a-zA-Z0-9_\-]+):([0-9\.]+)\s*\*\*\//g;
      let match;
      const deps = [];
      while ((match = depRegex.exec(code)) !== null) {
        const pkg = match[1];
        const ver = match[2];
        if (pkg !== "wealthengine") {
          deps.push({ pkg, ver });
        }
      }

      for (const { pkg, ver } of deps) {
        let isInstalled = false;
        try {
          const pkgPath = path.join(
            executionEngineDir,
            "node_modules",
            pkg,
            "package.json",
          );
          if (fs.existsSync(pkgPath)) {
            const pkgMetaRaw = fs.readFileSync(pkgPath, "utf8");
            const versionMatch = pkgMetaRaw.match(/"version":\s*"([^"]+)"/);
            if (versionMatch && versionMatch[1] === ver) {
              isInstalled = true;
            }
          }
        } catch (e) {}

        if (!isInstalled) {
          try {
            await execAsync(
              `npm install --no-audit --no-fund --silent ${pkg}@${ver}`,
              { cwd: executionEngineDir },
            );
          } catch (err) {
            sendErrorResponse(
              correlation_id,
              `[Runner Error] Failed to install dependency ${pkg}@${ver}: ${err.message}`,
            );
            return;
          }
        }
      }

      // B. Transpile TypeScript to JavaScript in-memory using esbuild
      let compiled;
      try {
        compiled = await esbuild.transform(code, {
          loader: "ts",
          format: "cjs",
          target: "node16",
        });
      } catch (err) {
        sendErrorResponse(
          correlation_id,
          `[Compiler Error] Failed to compile script:\n${err.message}`,
        );
        return;
      }

      // Wrap in an IIFE so async/await works seamlessly at top-level
      const wrappedCode = `
            (async () => {
                try {
                    ${compiled.code}
                } catch (innerErr) {
                    console.error(innerErr.stack || innerErr.message || String(innerErr));
                    throw innerErr;
                }
            })()
            `;

      try {
        script = new vm.Script(wrappedCode, {
          filename: `plan_${plan_id || "temp"}.ts`,
        });
        if (plan_id) {
          compiledPlans.set(plan_id, { script, code_hash });
        }
      } catch (err) {
        sendErrorResponse(
          correlation_id,
          `[Compiler Error] Failed to parse script in VM context:\n${err.stack || err.message || String(err)}`,
        );
        return;
      }
    } else {
      // Retrieve compilation cache
      script = cached.script;
    }

    // 3. Capture stdout / stderr logs
    const stdoutLogs = [];
    const stderrLogs = [];

    const customConsole = {
      log: (...args) => stdoutLogs.push(args.map((a) => String(a)).join(" ")),
      error: (...args) => stderrLogs.push(args.map((a) => String(a)).join(" ")),
      warn: (...args) =>
        stdoutLogs.push("[WARN] " + args.map((a) => String(a)).join(" ")),
      info: (...args) =>
        stdoutLogs.push("[INFO] " + args.map((a) => String(a)).join(" ")),
    };

    const executionContextState = {
      correlation_id,
      execution_token: state.execution_token,
      api_port: state.api_port || "8080",
      incomes: state.incomes || [],
      assets: state.assets || [],
      loans: state.loans || [],
      realtime_accounts: state.realtime_accounts || {}
    };

    const sandboxRequire = (moduleName) => {
      if (moduleName === "wealthengine") {
        const { WealthEngine } = require("./wealthengine_sandbox");
        return {
          WealthEngine: class extends WealthEngine {
            constructor() {
              super(executionContextState);
            }
          },
        };
      }
      return require(moduleName);
    };

    const activeTimeouts = new Set();
    const activeIntervals = new Set();

    const safeSetTimeout = (...args) => {
      const id = setTimeout(...args);
      activeTimeouts.add(id);
      return id;
    };
    const safeClearTimeout = (id) => {
      clearTimeout(id);
      activeTimeouts.delete(id);
    };
    const safeSetInterval = (...args) => {
      const id = setInterval(...args);
      activeIntervals.add(id);
      return id;
    };
    const safeClearInterval = (id) => {
      clearInterval(id);
      activeIntervals.delete(id);
    };

    const context = vm.createContext({
      console: customConsole,
      secrets: secrets,
      trigger: trigger,
      require: sandboxRequire,
      process: {
        env: { ...process.env },
      },
      setTimeout: safeSetTimeout,
      clearTimeout: safeClearTimeout,
      setInterval: safeSetInterval,
      clearInterval: safeClearInterval,
    });

    let success = true;
    let exitCode = 0;

    try {
      const runPromise = script.runInContext(context);
      if (runPromise && typeof runPromise.then === "function") {
        // Impose a strict 10 second timeout using a promise race
        const timeoutPromise = new Promise((_, reject) =>
          setTimeout(
            () =>
              reject(new Error("Script execution timed out after 10 seconds")),
            10000,
          ),
        );
        await Promise.race([runPromise, timeoutPromise]);
      }
    } catch (err) {
      success = false;
      exitCode = 1;
      const errStr = err.stack || err.message || String(err);
      if (!stderrLogs.includes(errStr)) {
        stderrLogs.push(errStr);
      }
    } finally {
      // Forcefully clear any remaining active timers to prevent dangling events
      for (const id of activeTimeouts) clearTimeout(id);
      for (const id of activeIntervals) clearInterval(id);
      activeTimeouts.add = () => {}; // Prevent new timers during cleanup
      activeIntervals.add = () => {};
      activeTimeouts.clear();
      activeIntervals.clear();
    }

    // Return multiplexed response frame
    const responseFrame = StdioFrame.create({
      execute_response: {
        correlation_id,
        success,
        stdout: stdoutLogs.join("\n"),
        stderr: stderrLogs.join("\n"),
        exit_code: exitCode
      }
    });
    writeFrame(StdioFrame.encode(responseFrame).finish());
  } catch (err) {
    sendErrorResponse(
      correlation_id,
      `[Internal Runner Error]: ${err.stack || err.message || String(err)}`,
    );
  }
}

function sendErrorResponse(correlation_id, stderrMsg) {
  try {
    const responseFrame = StdioFrame.create({
      execute_response: {
        correlation_id,
        success: false,
        stdout: "",
        stderr: stderrMsg,
        exit_code: 1
      }
    });
    writeFrame(StdioFrame.encode(responseFrame).finish());
  } catch (err) {
    // Fallback safety
  }
}

function writeFrame(responseBytes) {
  const lengthBytes = Buffer.alloc(4);
  lengthBytes.writeUInt32BE(responseBytes.length, 0);
  process.stdout.write(Buffer.concat([lengthBytes, responseBytes]));
}

// Ensure required packages are installed inside node_modules inside the execution engine directory
async function ensureDependenciesInstalled() {
  const requiredDeps = [
    "esbuild",
    "tsx",
    "typescript",
    "@types/node",
    "protobufjs"
  ];
  let needsInstall = false;
  for (const dep of requiredDeps) {
    if (!fs.existsSync(path.join(executionEngineDir, "node_modules", dep))) {
      needsInstall = true;
      break;
    }
  }
  if (needsInstall) {
    try {
      await execAsync(
        "npm install --no-audit --no-fund --silent esbuild tsx typescript @types/node protobufjs",
        { cwd: executionEngineDir },
      );
    } catch (err) {
      // Log fallback
    }
  }
}
