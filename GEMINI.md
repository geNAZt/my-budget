> [!CRITICAL]
> **EXECUTIVE AGENTIC DIRECTIVES (ZERO-TOLERANCE COMPLIANCE):**
> 1. **Zero-HTTP Application Logic:** All frontend-backend state must use **Protobuf over WebSockets**. No standard REST/Echo routes, no MsgPack, no JSON over WebSockets.
> 2. **No Massive Refactors:** Do not rewrite existing logic/components. Layer new features cleanly alongside them.
> 3. **No JSON-Bridging:** Convert models to Protobuf via direct field-by-field assignments. Do not convert through intermediate JSON strings.
> 4. **Idempotent DB Migrations:** Schema changes must use `information_schema` checks. `TEXT` columns must always have `DEFAULT ''`.
> 5. **No Absolute Paths:** Use relative or dynamically computed paths exclusively.
> 6. **Code Integrity:** Adhere strictly to the *Code Integrity (Tool vs. Chat Separation)* standard detailed in Section 1. Never use `...` or omission placeholders when executing background tools to modify codebase files on disk.
# Internal Wealth Engine: Agentic Workflow & Ruleset

This document serves as the foundational mandate for all AI agents and developers working on this project. Adherence to these rules is mandatory.

## 1. Core Principles
*   **Purpose:** This is a high-utility internal tool for deterministic financial engineering. No marketing fluff, no slogans.
*   **Security First:** Passkey (WebAuthn) is the only authentication method. No passwords.
*   **Immutable Versioning (The Standard):**
    *   **Edits:** Every change to a financial entity (Loan, Account, Income) MUST create a new immutable version record. Never overwrite current values.
    *   **Default View:** Controllers/API MUST only return the latest version of an entity by default.
    *   **Deletion Workflow:** Users must be offered a choice:
        *   **Revert:** Delete only the latest version (effectively rolling back to the previous state).
        *   **Full Archive:** Soft-delete the entire entity (mark as deleted, hiding all versions).
*   **UX > Data Density:** Avoid "Endless Tables." Use visual hierarchies, charts, and progressive disclosure.
*   **Resilient Evolution (Migrations):** 
    *   When introducing new architectural concepts or breaking changes, an **automatic migration** (DB or Config) MUST be implemented to ensure existing user flows remain operational.
    *   If an automatic migration is technically impossible, a **guided user flow** MUST be generated to walk the user through the required manual migration steps. 
    *   **Zero-Downtime Assumption:** Never break a user's access or history during an update.
* **Unified Diff Standard (Chat Only):** In the chat window response, the agent MUST ONLY display standard unified diff blocks (`+++` / `---`) for file modifications. Printing unchanged sections of code in the chat UI is strictly a code-review failure.
* **Atomic Tool Operations:** The 'No Code Omissions' rule applies strictly to file edits written to disk via system tools. The agent must pass the fully constructed payload to the file-writing tool directly while keeping the text response in the chat bubble restricted to a tiny summary or diff.
* **One-and-Done Verification:** Once a file has been successfully edited and verified via compilation scripts (`go test`, `npm run check`), the agent is forbidden from re-reading or displaying the newly saved file back into the chat window to confirm it worked. Trust the exit codes of the validation pipeline. 
* **Code Integrity (Tool vs. Chat Separation):** * **File Modifications (Disk):** When executing file-writing or editing tools to modify the codebase, the agent MUST inject the complete, syntactically correct code blocks. Using `...` or omission placeholders inside live source code files is strictly prohibited and constitutes an automatic compilation failure.
    * **Chat Responses (UI):** To maximize token efficiency, the agent MUST NOT print these complete files into the chat dialogue window. The chat interface is strictly reserved for standard unified diffs (`+++`/`---`) summarizing the changes made by the tools.
    
## 2. Design Identity (Mandatory Visual Rules)
Everything we design MUST be coherent with the already existing UI look and feel. Do not invent new UI patterns unless the existing ones are insufficient.

### 2.1 Visual Style Guide Reference Matrix
| Element | Tailwind/CSS Classes | Rules / Constraints |
| :--- | :--- | :--- |
| **Containers** | `.glass-card`, `bg-white/70`, `backdrop-blur-md`, `border-white/40` | Main content cards |
| **Surfaces** | `bg-white`, `border-slate-200`, `rounded-xl` to `rounded-2xl` | Interactive inputs / inner nested cards. **Never use slate-50 here.** |
| **Main Headings**| `font-black`, `tracking-tight`, `text-slate-900`, `text-3xl` to `text-5xl` | Clear visual hierarchy |
| **Field Labels** | `text-[10px]`, `font-black`, `uppercase`, `tracking-[0.2em]`, `text-slate-400` | Append `ml-1 mb-1` inside modal inputs |
| **Primary Action**| `.btn-primary`, `bg-indigo-600`, heavy shadow, white text | Standard submission button |
| **Danger Action** | `bg-rose-50`, `text-rose-600` | Destructive events/deletions |

*   **Containers:** Use `.glass-card` for all content blocks (White 70%, `backdrop-blur-md`, `border-white/40`).
*   **Surfaces:** Use pure White (`#ffffff`) for interactive inputs and nested cards inside the main Slate (`#f8fafc`) background. Avoid grey (`slate-50`) backgrounds for inputs.
*   **Typography Hierarchy:**
    *   **Page Titles:** Use the "Wealth Nodes." style: `text-5xl font-black tracking-tight text-slate-900` with the last word wrapped in `<span class="gradient-text">...</span>` and immediately followed by a dot (`.`). Do not use small icon-based subtitles above the main title. The main layout component (`+layout.svelte`) already enforces a padded, constrained viewport (`max-w-[1600px] py-12 px-4`). Page components MUST NOT implement their own outer padding/width wrappers (e.g. no `<div class="max-w-[1440px] p-8">`). Instead, root pages MUST start with a `<div class="space-y-12">` container followed by a `<header class="flex flex-col lg:flex-row lg:items-end justify-between gap-8">`.
    *   **Main Headings:** `font-black`, `tracking-tight`, `text-slate-900`, `text-3xl` to `text-5xl`.
    *   **Subheadings:** `font-black`, `uppercase`, `tracking-widest`, `text-xs` to `text-sm`.
    *   **Field Labels:** `font-black`, `uppercase`, `tracking-[0.2em]`, `text-[10px]`, `text-slate-400`.
    *   **Body Data:** `font-bold` or `font-black`, `text-slate-700`.
*   **Interactive Elements:**
    *   **Inputs/Selects:** White background, `border-slate-200`, `rounded-xl` to `rounded-2xl`.
    *   **Searchable Dropdowns:** Every dropdown selector MUST use the custom `SearchableDropdown.svelte` implementation. Native `<select>` elements are prohibited for major data selection (Accounts, Pools, Assets). Dropdowns must support live filtering via an integrated search input. The minimum width of the search dropbox needs to be at least 200px.
    *   **Focus State:** `ring-4`, `ring-indigo-500/10`, `border-indigo-500`.
    *   **Primary Action:** `.btn-primary` (Indigo 600, White text, heavy shadow).
    *   **Danger Action:** Rose 50 background, Rose 600 text.
*   **Gradients:** Use the standard `from-indigo-600 via-purple-600 to-pink-500` for primary accents (the "Stream" effect). Use subtle Indigo gradients for top-borders of modals.
*   **Spacing:** Use generous padding (`p-10` for main cards, `p-5` for items) and gaps (`gap-8` or `gap-12`) to maintain a "Premium" feel.

### 2.2 WealthEngine Editor Specification (Mandatory)
All "Refine" or "Modify" modals MUST adhere to the following layout and styling standard:

*   **Modal Container:**
    *   **Backdrop:** `bg-slate-900/40 backdrop-blur-sm`.
    *   **Main Card:** Pure White (`bg-white`), generous padding (`p-10`), rounded corners (`rounded-[30px]`), and a heavy shadow (`shadow-2xl`).
    *   **Top Gradient Border:** Use a subtle Indigo/Purple gradient top-border for premium depth.
*   **Header Section:**
    *   **Title:** Large, black, tight tracking (`text-2xl font-black text-slate-900 tracking-tight`).
    *   **Description:** Medium-weight slate text below the title (`text-slate-500 font-medium text-sm`).
*   **Form Layout:**
    *   **Gaps:** Use `space-y-8` for the main form flow.
    *   **Grids:** Use `grid-cols-2 gap-6` for related small inputs.
*   **Input Components:**
    *   **Background:** ALWAYS pure white (`bg-white`). Never use `slate-50` inside a white modal.
    *   **Borders:** Subtle slate (`border-slate-200`).
    *   **Labels:** Use the "Small Upper" style: `text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1`.
    *   **Focus:** `focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500`.
*   **Grouped Sections (Cards):**
    *   Group logical features (e.g., "Loan Dumping" or "Sub-Assets") into nested cards with `bg-white`, `border-slate-100`, `rounded-2xl`, and a subtle `shadow-sm`.
*   **Action Buttons:**
    *   **Small Actions:** (e.g., "Recalculate") Use `text-[9px] font-black uppercase underline` with a matching icon.
    *   **Main Primary:** `.btn-primary` with `bg-indigo-600` and heavy shadow.
    *   **Delete/Danger:** `.btn-secondary` or a soft rose style (`bg-rose-50 text-rose-600`).

## 3. Localization Mandates (Germany/International)
*   **UI Language:** The user interface MUST remain in **English**.
*   **Currency:** Primary currency is Euro (€).
*   **Formatting:** Use German numeric formatting: Comma (`,`) for decimals, Dot (`.`) for thousands.
*   **Budget Alignment:** All financial displays MUST follow the "Budget Alignment" standard:
    *   Currency symbol (€) MUST be left-aligned within its container.
    *   Numeric amount MUST be right-aligned within the same container.
    *   MUST use tabular numbers (`tabular-nums`) to ensure perfect vertical alignment across rows.
*   **Input Support:** All financial inputs MUST support cent-precision using the comma delimiter (e.g., `1021,55`).
*   **Date Standards:** Display dates in European format (e.g., `MM/YYYY` or `DD.MM.YYYY`).

## 4. Backend (Go) Standards
*   **Directory Structure:**
    *   `backend/`: Go (Echo/DDD).
    *   `backend/cmd/`: Entry points.
    *   `backend/internal/domain/`: Pure business logic.
    *   `backend/internal/api/`: Handlers and routing.
    *   `backend/internal/db/`: Persistence layer.
*   **Immutable Data:** Use a `versions` table for every major entity. 
    *   Example: `loan` table (metadata) + `loan_versions` table (values like interest, principal).
*   **Passkey Integration:** Use a standard library like `github.com/go-webauthn/webauthn`.

## 5. Frontend (Svelte) Standards
*   **Directory Structure:** `frontend/`: SvelteKit + TailwindCSS 4.
*   **Component-Driven:** Keep components small and focused.
*   **State Management:** Use Svelte stores for financial projection state.
*   **Visuals:** Use LayerCake or Chart.js for financial projections. 
*   **No Magic Numbers:** Every number displayed should have a tooltip or a breakdown of its calculation.

## 6. Observability & Debugging Mandates
Adherence to these standards is required for all backend and frontend development to ensure system visibility.

### 6.3. WebSocket & API Transparency
- **WebSocket Gateway**: Log every incoming and outgoing frame ID, Method, Path, and Status.
- **Echo Middleware**: Ensure the standard logger is active and captures all internal redirects from the WebSocket gateway.

### 6.4. Empirical Debugging
- NEVER guess why a request failed (e.g., a 404 or 500).
- ALWAYS check the logs for the specific `req.ID` or `correlation_id`.
- If logs are insufficient to explain a failure, your first task is to ADD the necessary logging before attempting a fix.

## 7. Versioning & Comparison Logic
*   The system must support "Scenarios." A Scenario is a collection of specific entity versions.
*   User should be able to "Fork" a scenario to see how a change (e.g., "What if I pay off the car earlier?") impacts the 30-year outlook.

## 8. Development Workflow (The Agentic Loop)
Every task iteration MUST cycle through these exact states in order:

1. **STATE: RESEARCH** * Call `grep_search` on the target directory before reading files (Keep reads under 150 lines per call).
2. **STATE: PLAN** * Modify/Write to `docs/DESIGN.md` explaining the intended architecture. 
   * **Self-Correction Check:** Does this plan violate the Zero-HTTP, Zero-MsgPack, or No-Massive-Refactor rules?
3. **STATE: IMPLEMENT**
   * Surgical updates only. Provide full, un-omitted structural files.
4. **STATE: VALIDATE**
   * Run compilation loops: Backend (`go test ./...`), Frontend (`npm run lint` & `npm run check`).
   * Assert numerical parity between old JS engines and new Go mathematical operations.

### 8.1 Agentic Safeguards (Bug Prevention)
*   **Database Evolution:** NEVER depend on the state of the database to verify schema changes. All database changes MUST be implemented as idempotent migrations in `backend/internal/db/postgres.go` using `information_schema` checks. The local database is not used on the server; code is the only source of truth for the schema. Do NOT use JSON/JSONB columns (e.g. TEXT columns storing marshalled JSON arrays) for nesting structured sub-entities or configurations; always use proper relational tables with foreign keys and correct order/sort fields.
*   **External APIs:** All external API access must be made using OpenAPI-generated clients. Manual struct definitions and HTTP call logic for external providers are prohibited. Use the definitions in `backend/apis/` to generate clients.
*   **Pathing Integrity:** NEVER use hardcoded absolute system paths (e.g. `/Users/fabian/...`) in source code or configurations. Always use relative paths (e.g. relative to the project root, app data directory, or working directory) or compute them dynamically at runtime (e.g., using `os.Executable`, `filepath.Dir`, or environment variables) to ensure portability across different host environments.
*   **Integration Providers Decoupling (Strict Encapsulation):** All integration-specific logic, data parsing, ticker symbol mappings, pending/active order calculations, and arithmetic side-matching (e.g., BUY orders mapped to negative amounts) MUST be strictly encapsulated within their respective provider packages (e.g., `backend/internal/integration/trading212/`). The core integration engine, database syncing systems, and core API layers (e.g., `backend/internal/api/integration.go`) MUST be completely agnostic of provider-specific exception handling. Every provider MUST map both completed and pending/active transaction payloads to the unified `domain.GenericTransaction` structure before encrypting and persisting them in `bank_transactions.encrypted_data`.
*   **Production-Grade Docker Mandate:** All Docker images MUST be strictly production-grade, minimal, and idempotent units. Installing development servers, live-reloading runtimes (such as `air`), or development-specific dependencies inside production images is strictly prohibited. Image build times should be accelerated solely using optimized caching layers (e.g., isolating production package installs like `npm ci --omit=dev`) and BuildKit cache mounts (`--mount=type=cache`), ensuring fast rebuild loops without introducing hot-reloading code or dev tools inside the final release containers. NEVER use `containrrr/watchtower:latest` as it is unmaintained and archived. Use the community-maintained fork `nickfedor/watchtower` for automated image updates.
*   **Persistent Stdio Multiplexed Protobuf Daemon Mandate:** The rule execution engine (`runner.js`) MUST operate as a single, long-running persistent child process spawned by the Go backend on startup. It MUST communicate exclusively over stdio pipes using length-prefixed Protocol Buffer (Protobuf) messages. To allow concurrent/parallel execution of plans without data corruption or interleaving, each frame MUST be multiplexed with a unique `correlation_id` (matched by Go pending response channels). Any modifications reverting this high-performance stdio IPC pattern to network-based protocols (like HTTP or gRPC) or one-shot command-spawn loops are strictly prohibited to secure maximum reliability, compile/dependency caching, and zero-network overhead.
*   **Zero-HTTP Unified WebSocket & Streaming Mandate:** The entire frontend-to-backend communication (including chunked scenario projection streaming) MUST run exclusively over the single shared persistent WebSocket connection using Protocol Buffers (Protobuf) binary serialization. Standard REST/Echo HTTP handlers are strictly prohibited for application logic; all API operations MUST be implemented as stateful WebSocket handlers registered via the `WSRegistry` and identified by logical namespaced paths (e.g., `assets::list`, `incomes::save`). Mocking chunked Server-Sent Events (SSE) streaming over the WebSocket is handled dynamically via a virtualized `ReadableStream` builder inside the frontend `ws_fetch` interceptor, pushing raw stream chunks seamlessly to Svelte component readers. Direct HTTP/HTTPS API polling or SSE stream fallback is strictly prohibited.
*   **Protocol Buffers (Protobuf) Type-Safety & Zero-MsgPack Mandate:** To achieve maximum type-safety, robust schema evolution, and minimal byte footprints, all client-server WebSocket interfaces MUST utilize statically compiled Protocol Buffers (Protobuf) binary serialization. MessagePack (MsgPack) is completely banned and eliminated. Over the WebSocket, only pure Protobuf binary frames MUST be spoken, with no JSON strings or JSON-hacking inside the Protobuf envelopes. The use of "JSON-bridging" (serializing a Go struct to JSON then deserializing it into a Protobuf struct, or vice versa) is strictly prohibited as a workaround for mapping logic. All mappings between domain entities and Protobuf messages MUST be implemented as direct field-by-field assignments or dedicated mapping functions. For internal local data storage (encrypted configurations and encrypted transactions), standard JSON MUST be used to preserve historical data compatibility. Stdio daemon communication between Go and the Node.js runner process MUST use Protocol Buffer (Protobuf) frames.

### 8.2 Prohibited Anti-Patterns (Automatic Code Review Failure)
* **DO NOT** import standard `net/http` or use Echo's `e.GET/e.POST` for standard data transactions. Everything goes through `WSRegistry`.
* **DO NOT** drop native `<select>` dropdowns for critical architectural units. Use `SearchableDropdown.svelte`.
* **DO NOT** write `db.Exec("ALTER TABLE ...")` without checking `pragma_table_info` in Go first. 
* **DO NOT** leave a Go string field scanning into a nullable database column without providing a `DEFAULT ''` constraint.
* **DO NOT** use JSON/JSONB columns or TEXT columns containing JSON string representations to store relational data or list configurations. Always create proper relational tables.

## 9. Token-Saving Guidelines (Agentic Context Optimization)
To ensure maximum speed, lower latency, and highly efficient token consumption, all AI agents MUST strictly adhere to the following memory optimization constraints:
*   **Targeted Code Reading:** NEVER read an entire file if it exceeds 300 lines. Use `grep_search` to isolate coordinates, and specify narrow `StartLine` and `EndLine` bounds when calling `view_file` (ideally less than 150 lines per call).
*   **Compact Artifact Retainment:** Keep active artifacts (`implementation_plan.md`, `task.md`, `walkthrough.md`) extremely focused. Once a sub-task or milestone is completed, condense its details into a brief summary paragraph rather than carrying over large historical diffs or extensive command logs.
*   **Avoid Redundant Invocations:** Do not re-fetch file contents or run lookup searches for files or endpoints that have already been retrieved or explained in the current active session memory.
*   **Encourage Compaction and Session Resets:** For extensive features or sequential debugging sessions, proactively suggest session summaries and resets to clear accumulated tool execution traces.
*   **Strict Output Minimization:** The agent MUST bypass long conversational explanations or step-by-step summaries of what it plans to do. 
*   **Diffs Only for Drafts:** When verifying a solution before writing it to disk, the agent should only output the modified lines or raw functions, never the surrounding untouched code blocks.

## 10. Production Environment & Deployment
The application uses an automated **CI/CD pipeline** for all production deployments. Manual source-code syncing or building on the server via SSH is strictly prohibited.

### 10.1 Deployment Workflow
*   **Trigger:** Every push to the `main` branch triggers a GitHub Action (`.github/workflows/deploy.yml`).
*   **Build:** Docker images for `backend` and `frontend` are built and pushed to the GitHub Container Registry (GHCR).
*   **Rollout:** The production server runs **Watchtower**, which polls GHCR every 60 seconds. It automatically pulls new images and performs a rolling restart of the services.
*   **Zero-Downtime:** The stack is managed via `docker-compose.prod.yml` with `pull_policy: always` to ensure immediate consistency.

### 10.2 Monitoring & Logs
*   **Primary Method:** Use the internal **Diagnostics** page (`/sysadmin`) to view live, streamed backend logs. This is the preferred way to monitor system health and debug runtime issues.
*   **Persistent Logs:** Scenario-specific logs are stored in `logs/scenarios/*.log` on the server volume.

### 10.3 SSH Access (Emergency/Debugging Only)
SSH access is reserved strictly for environment-specific debugging that cannot be performed via the Diagnostics page or local replication.

*   **Host:** `vm@vm-host.lan`
*   **Password:** `<REDACTED_SENSITIVE_DATA>`
*   **Project Folder:** `~/wealthengine/`
*   **Usage:** Tailing persistent logs, inspecting the database via `docker exec`, or restarting the Docker daemon.
*   **Agent Operational Rule:** The agent is **FORBIDDEN** from using SSH for any deployment-related tasks (e.g., `git pull`, `docker compose build`). Deployment is handled exclusively by pushing code to GitHub. SSH commands should only be issued for diagnostic purposes or database inspection.

