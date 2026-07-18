const fs = require("fs");
const path = require("path");
const { execSync } = require("child_process");
const vm = require("vm");
const { encode, decode } = require("@msgpack/msgpack");

const executionEngineDir = __dirname;

// Ensure initial sandbox engines and gRPC packages are installed in node_modules
function checkInitialDependencies() {
  const requiredDeps = [
    "esbuild",
    "tsx",
    "typescript",
    "@types/node",
    "@grpc/grpc-js",
    "@grpc/proto-loader",
    "@msgpack/msgpack",
  ];
  let needsInstall = false;
  for (const dep of requiredDeps) {
    if (!fs.existsSync(path.join(executionEngineDir, "node_modules", dep))) {
      needsInstall = true;
      break;
    }
  }
  if (needsInstall) {
    console.log(
      "[Daemon] Installing required sandbox engines (esbuild, tsx, typescript, gRPC loaders, msgpack)...",
    );
    try {
      execSync(
        "npm install --no-audit --no-fund --silent esbuild tsx typescript @types/node @grpc/grpc-js @grpc/proto-loader @msgpack/msgpack",
        { cwd: executionEngineDir },
      );
      console.log("[Daemon] Sandbox engines successfully installed.");
    } catch (err) {
      console.error("[Daemon] Failed to install initial engines:", err.message);
    }
  }
}

checkInitialDependencies();

const esbuild = require("esbuild");
const grpc = require("@grpc/grpc-js");
const protoLoader = require("@grpc/proto-loader");

// In-memory compilation cache for plans
const compiledPlans = new Map(); // maps planId -> { script, code }

const PROTO_PATH = path.join(executionEngineDir, "proto", "execution.proto");
const packageDefinition = protoLoader.loadSync(PROTO_PATH, {
  keepCase: true,
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
});
const executionProto = grpc.loadPackageDefinition(packageDefinition).execution;

// 1. Health check ping endpoint
function ping(call, callback) {
  callback(null, { status: "healthy" });
}

// 2. High-speed gRPC execute plan endpoint
async function execute(call, callback) {
  try {
    const { plan_id, code, state_msgpack, secrets_msgpack, trigger_msgpack } =
      call.request;

    // Parse in-memory state objects from MsgPack binary
    const state = state_msgpack
      ? decode(state_msgpack)
      : { incomes: [], assets: [], loans: [] };
    const secrets = secrets_msgpack ? decode(secrets_msgpack) : {};
    const trigger = trigger_msgpack
      ? decode(trigger_msgpack)
      : { type: "CRON", data: {} };

    let cached = plan_id ? compiledPlans.get(plan_id) : null;

    // If code is not sent and we have nothing cached, return not_cached: true
    if (!code && !cached) {
      callback(null, {
        success: false,
        stdout: "",
        stderr: `Plan ${plan_id} is not cached in daemon memory. Please re-send with code.`,
        exit_code: 1,
        not_cached: true,
      });
      return;
    }

    let script;
    // Compile only if code is provided and either we have no cache or the code has changed
    if (code && (!cached || cached.code !== code)) {
      // A. Dynamically install missing packages inside execution_engine directory
      const depRegex =
        /\/\*\*\s*depend\s+on\s+([a-zA-Z0-9_\-]+):([0-9\.]+)\s*\*\*\//g;
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
          console.log(
            `[Daemon] Installing missing dependency: ${pkg}@${ver}...`,
          );
          try {
            execSync(
              `npm install --no-audit --no-fund --silent ${pkg}@${ver}`,
              { cwd: executionEngineDir },
            );
          } catch (err) {
            callback(null, {
              success: false,
              stdout: "",
              stderr: `[Daemon Error] Failed to install dependency ${pkg}@${ver}: ${err.message}`,
              exit_code: 1,
              not_cached: false,
            });
            return;
          }
        }
      }

      // B. Transpile TypeScript to JavaScript in-memory using esbuild
      let compiled;
      try {
        compiled = esbuild.transformSync(code, {
          loader: "ts",
          format: "cjs",
          target: "node16",
        });
      } catch (err) {
        callback(null, {
          success: false,
          stdout: "",
          stderr: `[Compiler Error] Failed to compile script:\n${err.message}`,
          exit_code: 1,
          not_cached: false,
        });
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
          compiledPlans.set(plan_id, { script, code });
          console.log(
            `[Daemon] Successfully compiled and cached plan ${plan_id} (${code.length} bytes).`,
          );
        }
      } catch (err) {
        callback(null, {
          success: false,
          stdout: "",
          stderr: `[Compiler Error] Failed to parse script in VM context:\n${err.stack || err.message || String(err)}`,
          exit_code: 1,
          not_cached: false,
        });
        return;
      }
    } else {
      // Retrieve the pre-compiled script from the cache
      script = cached.script;
    }

    // C. Execute using Node.js 'vm' module
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

    const sandboxRequire = (moduleName) => {
      if (moduleName === "wealthengine") {
        const { WealthEngine } = require("./wealthengine_sandbox");
        // Inject the request-specific state into the WealthEngine sandbox module
        return {
          WealthEngine: class extends WealthEngine {
            constructor() {
              super(state);
            }
          },
        };
      }
      return require(moduleName);
    };

    const context = vm.createContext({
      console: customConsole,
      secrets: secrets,
      trigger: trigger,
      require: sandboxRequire,
      process: {
        env: { ...process.env },
      },
      setTimeout,
      clearTimeout,
      setInterval,
      clearInterval,
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
    }

    callback(null, {
      success: success,
      stdout: stdoutLogs.join("\n"),
      stderr: stderrLogs.join("\n"),
      exit_code: exitCode,
      not_cached: false,
    });
  } catch (err) {
    callback(null, {
      success: false,
      stdout: "",
      stderr: `[Internal Daemon Error]: ${err.stack || err.message || String(err)}`,
      exit_code: 1,
      not_cached: false,
    });
  }
}

function startServer() {
  const server = new grpc.Server();
  server.addService(executionProto.ExecutionEngine.service, {
    ping: ping,
    execute: execute,
  });

  const PORT = process.env.WEALTHENGINE_GRPC_PORT || 50051;
  server.bindAsync(
    `0.0.0.0:${PORT}`,
    grpc.ServerCredentials.createInsecure(),
    (err, port) => {
      if (err) {
        console.error(`[Daemon] Failed to start gRPC server: ${err.message}`);
        process.exit(1);
      }
      server.start();
      console.log(
        `[Daemon] WealthEngine script gRPC daemon listening on port ${port}`,
      );
    },
  );
}

startServer();
