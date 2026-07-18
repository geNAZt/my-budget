# Budget Web Design Document

## 1. Versioning & Comparison Engine (The "Scenario" System)

To support the requirement of "comparing two financial plans," we will implement a **Scenario-based Versioning System**.

### Data Model
*   **Entity Table (e.g., `loans`):** Stores identity and metadata (ID, UserID, Name, CreatedAt).
*   **Version Table (e.g., `loan_versions`):** Stores the actual values (Principal, InterestRate, Runtime). Each row has a `version_number` and is immutable.
*   **Scenario Table:** A named collection of specific entity versions (e.g., "Current Plan", "Faster Mortgage Payoff").
*   **Scenario_Entities Table:** A mapping of `ScenarioID` -> `EntityID` -> `VersionID`.

### Comparison UX
The UI will allow a "Split View" or "Overlay View":
*   **Scenario A (Baseline):** The primary plan.
*   **Scenario B (Experiment):** A forked plan where one or more entity versions are different.
*   **Delta Visualization:** Charts will show two lines (e.g., Net Worth over 20 years) to immediately visualize the impact of the change.

## 2. Passkey Authentication (WebAuthn)

We will use the **Passkey** flow to eliminate passwords:
1.  **Registration:** The user enters a username. The backend generates a challenge. The browser uses `navigator.credentials.create()` to sign it with biometric/hardware keys. The public key is stored in the database.
2.  **Authentication:** The browser uses `navigator.credentials.get()` to sign a login challenge. The backend verifies the signature against the stored public key.

## 3. UX Guidelines

*   **Progressive Disclosure:** Don't show the full 30-year table by default. Show a summary card with "Key Stats" (Total Interest Paid, Payoff Date, Net Worth at Retirement). Clicking the card expands into the detailed chart and table.
*   **Actionable Editing:** When editing a value, show a "Live Preview" sparkline of how that specific change affects the long-term goal.
*   **Terminology:** Use "Human" terms. Instead of "Amortization Schedule," use "Payment History."

## 4. ETF Interest Accumulation & Virtual Account State Carry-Over

To ensure that ETF assets correctly accumulate interest/growth and that virtual accounts persist their balances correctly over the projection timeline, we implement a unified state initialization and propagation model:

### State Initialization
*   **Virtual Account Balances**: On projection startup, we extract the `StartingBalance` for all active virtual accounts and save them in a running balance map (`vaRunningBalances`).
*   **Asset Initial Balances**: The starting balance of each asset is computed dynamically as the sum of the starting balances of its linked virtual accounts.
*   **ETF Lot Seeding**: If the asset is an ETF and has a non-zero initial balance, we seed `state.lots` with a single initial lot having both `principal` and `currentValue` set to the initial balance. The initial tracker balances (`state.trackerBalances`) are also initialized proportionally based on their target ETF config percentages.

### Month-over-Month State Propagation
*   **Virtual Account Carry-Over**: At the start of each projection month, virtual account starting balances are initialized to their latest running balance (`vaRunningBalances`). At the end of each projection month, the ending balance (`StartingBalance + Inflow - Outflow`) is computed and saved back to `vaRunningBalances` to cleanly propagate the state to the next month.
*   **ETF Compound Growth**: Since initial ETF lots are seeded correctly, the compounding interest/growth loop iterates over all lots, multiplying their values by the simulated monthly rate. The resulting growth is credited to the asset balance and distributed across any active sub-assets.

## 5. Database Schema Normalization & Migration

To eliminate JSON fields from the database, we decompose them into normalized relational tables.

### Normalized Tables
1. **`asset_version_etf_configs`**: Represents ETF tracker allocations.
   * `id`: `TEXT PRIMARY KEY` (UUID)
   * `asset_version_id`: `TEXT NOT NULL` (Foreign Key referencing `asset_versions(id)`)
   * `tracker`, `historical_tracker`, `conversion_tracker`, `history_provider`: `TEXT DEFAULT ''`
   * `percentage`, `ter`: `DOUBLE PRECISION DEFAULT 0.0`
2. **`asset_version_penalties`**: Represents withdrawal and interest penalties.
   * `id`: `TEXT PRIMARY KEY` (UUID)
   * `asset_version_id`: `TEXT NOT NULL` (Foreign Key referencing `asset_versions(id)`)
   * `name`, `trigger_type`: `TEXT DEFAULT ''`
   * `percentage`: `DOUBLE PRECISION DEFAULT 0.0`
3. **`asset_version_sub_assets`**: Represents sub-assets under an asset version.
   * `id`: `TEXT PRIMARY KEY` (Sub-Asset ID)
   * `asset_version_id`: `TEXT NOT NULL` (Foreign Key referencing `asset_versions(id)`)
   * `name`, `target_value`: `TEXT DEFAULT ''`
   * `amount_per_month`: `DOUBLE PRECISION DEFAULT 0.0`
   * `is_remainder_consumer`: `BOOLEAN DEFAULT FALSE`
   * `remainder_start_date`: `TIMESTAMP`
   * `dumping_loan_id`: `TEXT` (Foreign Key referencing `loans(id)`)
   * `start_date`: `TIMESTAMP NOT NULL`
   * `end_date`, `earliest_dump_date`: `TIMESTAMP`
4. **`scenario_remainder_orders`**: Represents the ordered list of EntityIDs for the scenario remainder flow.
   * `scenario_id`: `TEXT NOT NULL` (Foreign Key referencing `scenarios(id)`)
   * `entity_id`: `TEXT NOT NULL`
   * `position`: `INTEGER NOT NULL`
   * `PRIMARY KEY(scenario_id, position)`
5. **`scenario_etf_params`**: Represents scenario-specific ETF parameter overrides.
   * `scenario_id`: `TEXT NOT NULL` (Foreign Key referencing `scenarios(id)`)
   * `ticker`: `TEXT NOT NULL DEFAULT ''`
   * `simulations`, `sim_years`, `lookback_years`: `INTEGER DEFAULT 0`
   * `sim_percent`: `DOUBLE PRECISION DEFAULT 0.0`
   * `PRIMARY KEY(scenario_id, ticker)`

### Migration & Cleanup Strategy
1. **Idempotent Setup & Migration**: On database initialization (`InitDB` / `migrate`), we verify the presence of the original JSON columns using `information_schema`.
2. **Data Extraction**: If the JSON column exists, we read all records containing non-empty JSON data, deserialize them in Go, and insert them into the new relational tables.
3. **Column Dropping**: Once the data is successfully migrated, we drop the JSON columns. This ensures zero data loss and leaves the schema completely clean.

## 6. Scenario Modification Resolution

To ensure that modifications (such as SWR/passive income withdrawals or debt repayment automations) are correctly processed when a scenario is scoped/filtered, the `ProjectionService` resolves modifications dynamically:
*   **Agnostic Frontend Configuration**: Since the frontend does not explicitly link or display modifications in the scenario scoping view, we determine active modifications based on their target entities.
*   **Target Activation Rule**: A modification is resolved and included in the simulation if:
    1.  The scenario is unscoped (`len(scenario.Entities) == 0`), OR
    2.  The modification is explicitly linked to the scenario as an entity (for backward/future compatibility), OR
    3.  The modification's target asset (single `TargetID` or any `TargetIDs`) is active in the scenario's scoped entities, OR
    4.  The modification's target loan (`TargetID`) is active in the scenario's scoped entities.

### Frontend Scoping Integration
To allow users to explicitly enable or disable modifications for a given scenario:
- **Modifications Tab**: Add a "Modifications" tab to the Logic Scope Editor Modal.
- **State Loading**: Fetch the modifications list (`modifications::list`) on mount and store them in `allModifications`.
- **Scoping Operations**: Integrate modification selections into `getAllEntities()`, `selectAllOfType()`, and `toggleEntity()`.

### Interval-Aware SWR Threshold Comparison
For SWR/passive income modifications (`WithdrawalPercentage > 0`), the threshold target check evaluates against:
- **Monthly SWR Withdrawal**: If `IntervalMonths == 1` (Monthly), the threshold `Amount` is compared to the monthly SWR withdrawal (`totalBalance * (WithdrawalPercentage / 100.0 / 12.0)`).
- **Annual SWR Withdrawal**: If `IntervalMonths` is any other value (e.g., `12` or `0`), the threshold `Amount` is compared to the annual SWR withdrawal (`totalBalance * (WithdrawalPercentage / 100.0)`).

## 7. Explicit Sub-Asset Target Expense Searchable Dropdown

To adhere to the user interface rules defined in `web/GEMINI.md` (Section 2.1), native select elements are prohibited for major data selection, and dropdowns must support live filtering via an integrated search input with a minimum width of at least 200px.

### Implementation Details
*   **SearchableDropdown Integration**: Replace the native `<select>` element for selecting the target expense in the sub-assets section of `AssetManager.svelte` with the `<SearchableDropdown>` component.
*   **Derived Option Mapping**: Map the reactive `expenses` array to the format `{ id: string, label: string }` via a derived `expenseOptions` state:
    ```typescript
    const expenseOptions = $derived([
        ...(expenses || []).map((e) => ({
            id: e.id,
            label: e.name,
        })),
    ]);
    ```
- **Value Binding**: Bind the `SearchableDropdown` component directly to `target.expenseId` to synchronize updates seamlessly with the frontend model.

## 9. Realtime Tracker Account Backoff Countdown Timer

To improve visibility into the next available sync window, the static "backoff until" timestamp in the "Realtime -> Chains" view will be replaced with a live countdown timer showing the ETA (e.g., "in 5 minutes").

### Implementation Details
*   **Reactive Clock State**: Introduce a reactive `$state(new Date())` variable named `now` in `+page.svelte`.
*   **Live Clock Update**: Use `setInterval` within `onMount` to update the `now` state every second. Ensure the interval is cleared on component unmount.
*   **Countdown Logic**: Update the `formatTimeRemaining` function to calculate the difference between the `backoffUntil` timestamp and the current `now` state.
*   **Visual Representation**:
    *   If the backoff time has passed, display "Sync Ready."
    *   Otherwise, display the remaining time in an H:M:S format (e.g., "Backoff: 02m 45s").
    *   The Svelte reactivity system will ensure the countdown updates in real-time as `now` changes.


## 8. Occurrence-Based Transaction Deduplication for Bank Integrations

To prevent duplicate transactions when banking APIs (such as the GoCardless/Nordigen sandbox or unstable real bank feeds) return changing transaction IDs or unstable external IDs across different sync runs, we implement a robust occurrence-based deduplication mechanism.

### Design Details
1. **Composite Key**: We define a unique transaction details signature: `date | amount | description | peer`.
2. **Decrypt Existing Transactions**: During the sync process, the integration provider decrypts the payload of existing transactions to count and track how many instances of a transaction with the same signature already exist in the database.
3. **Deduplication Check**:
   - For each incoming transaction, we first check if the `external_id` matches an existing transaction.
   - If not, we check the composite key. We maintain a count of how many times we've seen this composite key in the current sync loop.
   - If the current loop count is less than the count of existing identical transactions in the database, we assume this is a matching transaction whose ID changed. We optionally update its external ID to keep it in sync and skip inserting a duplicate.
   - If the current loop count is greater than or equal to the count of matching database transactions, we treat it as a new transaction and insert it.

## 10. Container Log Streaming & Sysadmin Diagnostics Selector

To enable live diagnostic log monitoring for all running Docker containers in the application's stack (e.g. backend, db, frontend, tunnel, watchtower), we implement a container selector and live log streamer using the Docker Unix socket.

### Backend Implementation
1. **Docker Unix Socket Access**: Connect directly to `/var/run/docker.sock` via a Unix domain socket HTTP client.
2. **Container Retrieval (`system::containers`)**:
   - Query `GET /containers/json` to fetch all containers.
   - Parse results into a list of container details: `ContainerInfo` containing `ID`, `Name`, `State`, and `Status`.
3. **Multi-Source Log Streaming (`system::logs`)**:
   - Accept a `SystemLogRequest` containing `container_id`.
   - If `container_id` is empty or `"current"`, fall back to streaming the local Go backend process logs captured via `LogService`.
   - Otherwise, stream logs from the selected container using `GET /containers/{id}/logs?stdout=1&stderr=1&follow=1&tail=200`.
   - Demultiplex the Docker log frame format (8-byte header: byte 0 = stream type, bytes 4-7 = message length) to stream text lines cleanly.

### Frontend Implementation
1. **Container Selector**:
   - Use the `SearchableDropdown` component to allow users to select which container logs they want to view.
   - Include a default option for "Current Process" to preserve local/standard diagnostics logs.
2. **Dynamic Streaming Reconnection**:
   - Re-establish the websocket log stream (`system::logs`) with the new `container_id` when the user changes the selected container.

## 11. Transaction Status Validation for UPCT Pending Rejection

To prevent booked (finalized/posted) bank transactions that happen to contain the `UPCT` (Unrealised Payment Card Transaction) subcode from being incorrectly marked as pending (hold/blocked funds) and subsequently deleted/expired, the EnableBanking integration provider must check the transaction status.

### Logic Details
1. **Status Check**: When parsing transactions, the provider checks the `status` field.
2. **Pending Rejection Rule**: A transaction with the `UPCT` sub-code is only marked as `PENDING_REJECTION` if its status is **not** `"BOOK"` (or `"BOOKED"`). If the status is `"BOOK"`, it is treated as a standard finalized booked transaction and not as a pending hold.
3. **Fallback Struct Extension**: Add the `Status *string` field to the minimal fallback unmarshal structure to ensure it is always captured.
4. **Data Recovery Migration**: On backend startup, we run an automatic DB query to restore any transactions that were previously wrongly soft-deleted and marked as `EXPIRED_REJECTION` due to the bug:
   ```sql
   UPDATE bank_transactions SET is_deleted = FALSE, internal_status = '' WHERE is_deleted = TRUE AND internal_status = 'EXPIRED_REJECTION';
   ```

## 12. Enhanced Alias-Based Auto-Linking for Internal Transfers

To ensure transactions representing internal transfers between a user's accounts are correctly linked when one side contains an empty or differing description, we implement an alias-based matching strategy.

### Design Details
1. **Alias Map Generation**: On auto-linking, we parse the `AccountsMetadata` from decrypted integration configs to construct a map of `AccountID` to `Alias` (account name).
2. **Description Heuristics**: Two candidate transactions with opposite amounts and within a 96-hour window are matched if:
   - Their descriptions are exactly equal (case-insensitive, trimmed).
   - OR, one transaction description contains the alias of the other transaction's account (case-insensitive, requiring alias length >= 3).
3. **Execution**: The auto-linking logic updates both transactions' `linked_transaction_id`, sets `is_link_confirmed = TRUE`, and assigns `source_account_id` and `destination_account_id` respectively.

## 13. Virtual Account Outstanding Booking Dates & Sorting

To improve visibility into when outstanding virtual account items are expected to be booked and organize the virtual account breakdown:
1. **Protobuf Schema Extensions**: Add `previous_booking_date` and `booking_date` fields to `EntryBreakdown` in `api.proto`.
2. **Backend Booking Date Calculation**:
   - For each simulated month, retrieve decrypted transactions for each pool.
   - For each breakdown entry, associate it with its pool's transactions.
   - `booking_date`: The latest transaction in the pool that falls in the current simulation month.
   - `previous_booking_date`: The latest transaction in the pool that falls in a previous simulation month.
3. **Frontend Integration & Display**:
   - In Svelte, display the day of the previous booking (e.g. `(normally 15.)` using German formatting) next to outstanding items.
   - Sort the virtual account's mapped items by category and date:
     - **Outstanding Bills & Expenses**: Sorted by `booking day - current day`, putting 0 (due today) on top, followed by future days, and past days.
     - **Booked Bills & Expenses**: Sorted by actual booking day in the current month, latest first.
     - **Booked Incomes**: Sorted by actual booking day in the current month, latest first.

## 14. Sync Run Log Parser & Transaction Viewer

To allow administrators to audit bank/integration synchronization processes and trace transaction histories:
1. **Sync Run Metadata Tracking**:
   - When initiating a sync via `SyncIntegration`, create and save a `metadata.json` file inside the correlation ID directory.
   - Metadata contains the integration ID, integration name, service type, user ID, and start timestamp.
2. **Sync Log Parsing**:
   - The Go backend will expose two new WebSocket handlers:
     - `system::sync_runs`: Lists all sync runs (both from metadata files and database-mapped correlation IDs), sorted by date (newest first). Supports integration filtering.
     - `system::sync_run_details`: Returns details of a specific sync run, including parsed transactions found in `*_resp.json` files and raw logs.
3. **Transaction Extraction & Parsing**:
   - Scan all `*_resp.json` log files for transaction arrays based on provider format (GoCardless: `transactions.booked`/`pending`, EnableBanking: `transactions`, Trading212: raw array).
   - Extract unique transaction IDs, amount, currency, description, peer, and raw JSON representation.
4. **Transaction Cross-Linking**:
   - When viewing details of a transaction, find all other sync runs containing the same transaction (external ID) by scanning historical logs or querying the database.
5. **Frontend UI (`/sysadmin/sync-logs`)**:
   - A modern, high-density dashboard following the project's glassmorphism style.
   - Filters runs by integration, lists them with counts/timestamps, parses transactions, and offers raw JSON inspect/linkage views.

## 15. Modal Escape Key Dismissal & UPCT/POSD Transaction Deduplication

### Modal Escape Key Closure
- In all Svelte 5 components implementing modal dialogs, we will register a global keydown handler using `<svelte:window onkeydown={...} />`.
- When the "Escape" key is pressed, we will dismiss any active modal by setting the respective `$state` visibility flags (e.g. `showAddModal`, `showDeleteConfirm`, etc.) to `false`.

### UPCT/POSD Transaction Deduplication & Linking
- **EnableBanking Provider mapping update**: In `web/backend/internal/integration/enablebanking/provider.go`, we will classify all transactions with the sub-code `"UPCT"` as pending unconditionally, setting `InternalStatus = "PENDING_REJECTION"`.
- **Deduplication Reconcile Logic update**: In `web/backend/internal/service/sync_service.go`, the reconciliation flow will bypass the `fetchedExternalIDs` check when a matching finalized transaction is found. This ensures that the unconfirmed (UPCT) duplicate is immediately soft-deleted upon the arrival of its finalized counterpart.
- **Database Migration for Existing Transactions**: We will write a new database migration (`024_fix_upct_posd_transitions_from_all_logs`) in `postgres.go` to scan all historical sync logs under `logs/sync_runs`, identify any transactions that were originally marked as `"UPCT"`, update their database status to `"PENDING_REJECTION"`, and run a reconciliation pass to soft-delete them if a matching finalized version already exists.

## 16. Past Month Scenario Visibility & Current Month Highlighting

### Backend Start Date Resolution
- Currently, when running scenario projections, if the configured `scenario.StartDate` is in the past, the backend forces the projection starting date `now` to be the current system time `time.Now()`. This prevents users from viewing historical or already gone months that are part of the projection period.
- We will modify `web/backend/internal/service/projection_service.go` in `RunWithLimit` to use `scenario.StartDate.UTC()` as the projection base date, regardless of whether it is in the past or future. This ensures that the simulation runs from the configured start date onwards.

### Frontend Current Month Highlighting
- In the scenario projection view (`web/frontend/src/routes/scenarios/+page.svelte`), each month returned by the simulation is rendered as a row in the months table.
- We will implement an `isCurrentMonth` checker that takes the `month` object and checks if the current real-world date (`new Date()`) falls between its `periodStart` and `periodEnd` bounds. This naturally respects the configured start day (e.g. 26th).
- For the row representing the current month, we will apply premium highlighting styles coherent with the theme: an orange background tint (e.g. `bg-orange-500/10` or `bg-orange-500/15` with matching left border and text color) to visually set it apart.

### Dashboard Current Month Resolution
- The main dashboard loads the active scenario's current budget sheet by running a projection limit of 1 month. Since the projection starting date can now be in the past (based on `scenario.StartDate`), a limit of 1 would return the historical starting month instead of the current month.
- We modified `web/frontend/src/routes/dashboard/+page.svelte` to dynamically compute the number of projection months required from the scenario's start date to the current month, passing the correct `projectionMonths` to the WebSocket projection service call and selecting the current month sheet out of the returned stream.

## 17. Day Header Transaction Balance & Account Balance History

### 1. Day Header Transaction Balance
- We will introduce a derived map `dayBalances` in `+page.svelte` that groups `filteredTransactions` by their formatted German date strings and sums their transaction amounts.
- In the day header template (when `showDateSeparator` is true), we will display the sum of transactions for that day. We will style it as a badge using `tabular-nums` formatting and colors matching the balance (emerald for positive/zero, rose for negative).

### 2. Account Balance History
- **Database Schema**: A new migration `025_create_account_balance_history` will create `account_balance_history` to store account balances snapshot records with fields `id`, `user_id`, `integration_id`, `account_id`, `balance`, and `recorded_at`. We will create an index on `(account_id, recorded_at)`.
- **Balance Snapshot on Sync**: Inside the Go backend's `finalizeSync` method, when an integration successfully completes synchronization, we will fetch the latest accounts using the provider and save snapshot records for each enabled account.

### 3. Debit Status & Rebalancing Transactions
- **Protobuf Extensions**: We will extend the `IntegrationAccount` message in `api.proto` with `was_in_debit_last_month` and `rebalancing_transactions`.
- **Backend Resolution**: We will implement `ListAccountBalanceHistory` in `TransactionRepository` to query all historical balance snapshots for an account.
- **Debit/Rebalance Logic**:
  - We will check if the account's balance fell below zero at any point during the previous calendar month solely based on the balance history records (checking the starting balance before last month and any recorded balance snapshot during last month).
  - We will query all transactions for the account, and identify rebalancing transactions as incoming transactions (amount > 0) that occurred in last month or later, where the balance immediately before that transaction (resolved from the most recent balance history snapshot) was negative.
- **Frontend Display**:
  - In the realtime page's account list, if an account went in debit last month, we will display an eye-catching warning badge.
  - Hovering or clicking the warning will reveal a premium popover showing the rebalancing transactions, helping the user audit how the account was rebalanced.

## 18. Flexible Remainder-Funded Expenses

We introduce the ability to configure an expense to be funded dynamically using scenario remainder cash flow, which shifts the target date of the expense to whenever the sub-asset accumulates enough funds.

### Design Details
1. **Frontend Configuration ("Fund Later")**:
   - In the expense details modal, the "Fund Later" tab is enhanced with a checkbox: "Fund via Asset Remainder".
   - When checked, the "Monthly Savings Required" display shows €0,00 (no fixed rate required).
   - Creating the funding plan creates a sub-asset with `isRemainderConsumer: true`, `amountPerMonth: 0`, and `expenseId: selectedExpenseObj.id`.
   - The expense itself is automatically renamed to include ` (Flexible)` to ensure the target date is treated as flexible.
2. **Backend Projection Engine Integration**:
   - We check if an expense is flexible (name contains ` (Flexible)` or `[Flex]`) and is linked to an active remainder-consumer sub-asset.
   - If so, the expense's default `DueDate` is ignored.
   - During the projection months loop, if the associated sub-asset's `currentBalance` has accumulated enough funds to meet or exceed the expense's `amount`:
     - The expense is triggered in that month (added to `month.Expenses`).
     - The sub-asset is closed, and its balance is paid out to offset the expense (`month.Income += netPayout`).
     - This preserves a clean vertical cash flow balance in the projection dashboard.
   - In the regular sub-asset payout loop, remainder consumer sub-assets linked to flexible expenses are ignored to prevent premature payouts on their `endDate`.
3. **Sub-Asset Mappings**:
   - Ensure all sub-asset mappings (such as in `updateExpenseDetails` and asset saving) include the `expenseId` field.
4. **Timeline Rendering Payout Stop**:
   - In `+page.svelte`, the `getActualEndIndex` function is updated to also check `m.breakdown.incomes` in addition to `m.breakdown.assets` to find non-loan-dumping sub-asset payouts. This ensures remainder-consumer sub-assets correctly stop rendering their timeline tracks in the month they pay out.

## 19. Sub-Asset Remainder Priorities

To prevent large or long-term remainder-consumer sub-assets from blocking other sub-assets from receiving remainder allocations, we introduce configurable sub-asset remainder priorities.

### Design Details
1. **Model & Schema Changes**:
   - We extend `SubAsset` in the Protobuf schema (`api.proto`) and domain package with a `remainder_priority` field (integer).
   - We execute an idempotent DB migration to add the `remainder_priority` column to `asset_version_sub_assets`.
2. **Frontend UI**:
   - In the edit asset modal, if a sub-asset has "Enable Remainder Consumption" checked, we display a numeric priority input field ("Prio") next to it.
   - Lower numbers indicate higher priority (i.e. processed first).
3. **Backend Projection Waterfall**:
   - Within each parent asset's remainder distribution, we group active remainder-consuming sub-assets by their `remainderPriority`.
   - We sort these priority groups in ascending order.
   - We iteratively distribute remainder to the highest priority group (using the even split logic if there are multiple sub-assets at the same priority). Any remaining funds cascade to the next priority groups.

## 20. Asset Duplication in Dashboard

To allow users to clone existing complex assets easily, we implement a single-click asset duplication feature in the dashboard asset manager.

### Design Details
1. **Frontend Action Button**:
   - We add a "Duplicate" action button (using the Copy icon) alongside the edit/delete controls in the asset list card layout.
2. **Duplication Business Logic**:
   - A confirmation dialog is shown to verify intent.
   - When confirmed, the client-side asset structure is decoded and deep-copied.
   - The primary ID of the parent asset is replaced with a fresh client-side UUID, and ` (Copy)` is appended to its name.
   - Each sub-asset listed under `activeVersion.subAssets` is also assigned a brand-new unique ID to prevent database constraint conflicts.
   - We trigger the standard `assets::save` WebSocket API with the newly cloned object. The backend handles auto-generating a new version ID and writes the cloned penalties, ETF configs, and stitching segments to the database.
   - The UI automatically refreshes to display the cloned asset.

## 21. Configurable Passive Income Assets

Instead of assuming all ETF based assets are used for passive income and auto-stopping them, we introduce a configurable `use_for_passive_income` property to allow granular selection of which assets qualify.

### Design Details
1. **Model & Schema Changes**:
   - We add `bool use_for_passive_income` to the `AssetVersion` Protobuf model.
   - We run a DB migration to add `use_for_passive_income` (BOOLEAN DEFAULT FALSE) to `asset_versions`.
2. **Frontend Configuration UI**:
   - In the edit asset modal, when the asset type is "ETF", we render a new checkbox: "Use for Passive Income".
   - This sets `useForPassiveIncome` to true/false in the state.
3. **Backend Projection Engine Integration**:
   - During the monthly simulation loop, we compute `etfWorth` (used to calculate the passive income milestone) by only summing ETF assets that have `UseForPassiveIncome` set to true.

## 22. Scenario-Aware Calendar Selector Ranges

To align the "This Month" and "Prev Month" calendar filters with the budget cycle, the date calculation must account for the `monthStartDay` of the current active simulation (scenario).

### Design Details
1. **Scenario List Retrieval**:
   - We update `realtime/+page.svelte` to fetch the scenarios list on mount via `scenarios::list` WebSocket API.
   - We store the scenarios in Svelte state.
2. **Active Scenario Identification**:
   - The active scenario is defined as the scenario where `isActive` is true.
   - We extract `monthStartDay = activeScenario?.monthStartDay || 1`.
3. **Date Boundary Calculation**:
   - When the user selects "This Month" or "Prev Month", we calculate the boundaries based on `monthStartDay`.
   - If `monthStartDay <= 1`:
     - "This Month": Start on the 1st of the current month, end on the last day of the current month.
     - "Prev Month": Start on the 1st of the previous month, end on the last day of the previous month.
   - If `monthStartDay > 1`:
     - Capped at 28 (`monthStartDay = Math.min(monthStartDay, 28)`).
     - "This Month":
       - If today's day of the month is `< monthStartDay`, the current budget period starts on `monthStartDay` of the previous month, and ends on `monthStartDay - 1` of the current month.
       - If today's day of the month is `>= monthStartDay`, the current budget period starts on `monthStartDay` of the current month, and ends on `monthStartDay - 1` of the next month.
     - "Prev Month" is calculated similarly as the period immediately preceding "This Month" (i.e. using a date offset by 1 day before the start of the current period).
4. **Timezone Safety**:
   - We format dates using local date components (`getFullYear()`, `getMonth()`, `getDate()`) to generate `YYYY-MM-DD` strings, avoiding timezone shifts inherent to `toISOString()`.
## 23. Target Expense Funding & Payout Attribution

We introduce support for non-remainder-consumer sub-assets (target savings plans) to automatically fund their linked expenses when the sub-asset reaches its payout month (end date). We also implement proper virtual account attribution for sub-asset payouts.

### Design Details
1. **Backend Projection Engine Integration**:
   - In `projection_service.go`, inside the monthly simulation loop (Step 2: Bills and Expenses), we check if an expense is linked to a non-remainder-consumer sub-asset whose end date (payout month) is reached in the current simulation month.
   - If `sa.expenseID != nil` matches the expense ID, and `!sa.isRemainderConsumer`, and the current month matches `sa.endDate`, the expense is triggered.
   - Triggering the expense adds its amount (`v.Amount`) to `month.Expenses` and appends it to `month.Breakdown.Expenses`.
   - We ensure the expense is triggered only once (either by its own due date or by the linked sub-asset end date, using a logical OR condition).

2. **Virtual Account Payout Attribution**:
   - When a sub-asset payout occurs:
      - If the payout is **linked** to an expense (i.e. `sa.expenseID != nil`), the payout's income entry is attributed to the linked expense's `AccountIDs` so they offset each other in the same virtual accounts.
     - If the payout is **non-linked** (i.e. `sa.expenseID == nil`), the payout's income entry is attributed to `nil` (`unassigned`), dumping the content of the sub-asset directly onto the budget sheet's general pool so it is available to be spent/allocated.

## 24. Realtime Tracker Compact Transaction Detail View & Single Tag Cloud Selection

To improve the user experience on the realtime transaction tracker, we simplify and optimize the detail transaction modification panel:
1. **Remove Associated Transactions (Chain)**: Eliminate this section entirely from the transaction edit view, as it is not useful in its current state.
2. **Compact Metadata View**: Display the transaction Amount, Peer Name, and Peer IBAN as styled, read-only metadata cards instead of bulky text inputs. These fields are no longer editable in the frontend.
3. **Hotkey Database Payload Toggle**:
   - The database payload JSON remains hidden by default.
   - We implement global keydown event handlers (`Alt + D` / `Alt + P` case-insensitive) to toggle the visibility of the raw payload while the transaction edit modal is active.
4. **Single-Tag Cloud Selection**:
   - A transaction is restricted to at most one tag.
   - We replace the plain comma-separated text input with a beautiful, search-enabled tag cloud selector.
   - Available tags are dynamically built from default values, user-defined custom tags, and existing transaction tags.
   - If the user types a tag that does not exist in the available set, a "+ Add" button is rendered. Clicking this adds the new tag to a persisted custom tag list in `localStorage`, selects it, and clears the query.

## 25. Realtime Transaction Detail Wizard for Pool & Expense Creation

To streamline budgeting directly from the transaction feed, we introduce a wizard button in the transaction edit modal to dynamically generate a pool, attach the current transaction to that pool via an explicit transaction ID rule, create a corresponding expense linked to that pool, and link it to the active scenario.

### Design Details
1. **Rule Engine Field Expansion**:
   - We extend `evaluateRule` and `ProcessTransaction` in `rule_service.go` to accept the transaction ID.
   - We add a new rule matching field: `"TRANSACTION_ID"`. When evaluated, it compares the transaction ID to the rule regex.
   - All rule processing calls in `sync_service.go`, `integrations.go`, and provider packages are updated to pass the transaction ID.

2. **Frontend Wizard Component**:
   - We implement a new wizard modal or embedded wizard form in `realtime/+page.svelte`.
   - The wizard takes the current transaction's details:
     - Default pool/expense name is the transaction's receiver/description.
     - The user can modify the pool name.
     - Budget date is set to the start date of the active period containing the transaction's book date (calculated via `getPeriodBoundsForDate(new Date(tx.createdAt), monthStartDay).start`).
   - The wizard performs the following actions:
     - Creates a new pool via `pools::save` WebSocket API.
     - Creates a transaction rule matching the explicit transaction ID using `rules::save` (assigning the rule field to `"TRANSACTION_ID"` and regex to the transaction's ID, targeting the newly created pool ID).
     - Creates a corresponding expense with the pool name via `expenses::save` WebSocket API (amount is set to the absolute transaction amount, pool ID is the new pool ID, due date is set to the active month start day, and it is linked to the current active scenario).
     - Closes the detail view and prompts a refresh of the page state.

## 26. Dashboard Table Lists Redesign

To improve UI usability and density, we convert the entity managers (Assets, Incomes, Expenses, Bills, Loans, Modifications, and Virtual Accounts) from card grid views to compact, clean proper table views.

### Design Details
1. **Sorted Lists**:
   - Every list is sorted client-side before rendering.
   - Sorting order: Primary: Start/Book date (ascending order). Secondary: Name (alphabetical/ascending order).
   - If an entity does not have a start/book date (such as virtual accounts), we sort it alphabetically by name.
   - For `incomes`, `bills`, `loans`, `assets`, `modifications`, the date field is `activeVersion.startDate`.
   - For `expenses`, the date field is `activeVersion.dueDate`.
   - For `modifications`, the secondary sort field is `description` since they do not have a `name` field.
2. **Table UI Layout**:
   - Replace the grid layout `<div class="grid ... gap-6">` with a responsive table wrapped in a `.glass-card` container.
   - Clean, modern table headers with small uppercase labels: `text-[10px] font-black uppercase tracking-[0.2em] text-slate-400`.
   - Table rows with generous horizontal and vertical padding for premium feel.
3. **Rightmost Action Column**:
   - The rightmost column of the table row is reserved for primary lifecycle actions: Edit (pencil icon) and Delete (trash icon).
   - For `AssetManager.svelte`, we also place the duplicate button (copy icon) in the rightmost actions column next to edit/delete.
4. **Header Add Button**:
   - Ensure the "Add" button remains easily accessible in the header of each page/component.

## 27. Asset Balance Payout & Analytics Chart Bug Fixes

To resolve the issue where assets do not properly reduce their balances in the analytics charts or monthly breakdown displays when a payout happens, we implement the following design:

### 1. Backend Projection Service Updates
- In `web/backend/internal/service/projection_service.go`, when an asset or sub-asset payout/withdrawal occurs (e.g., in sub-asset end dates, final dump, orphaned dump, or remainder payouts), we must:
  - Subtract the net payout/withdrawn amount from `month.Assets` to reflect the asset delta of that month.
  - Append a breakdown entry to `month.Breakdown.Assets` using `buildAssetBreakdownEntry` with the negative withdrawn amount and the corresponding penalty paid.
- Identify all places where payouts happen:
  - Flexible remainder sub-asset payouts.
  - Regular sub-asset end date payouts.
  - Active sub-asset final payouts at asset end date.
  - Leftover/remainder parent asset payouts at asset end date.
  - Orphaned dumping sub-asset payouts.
  - Orphaned dumping parent asset payouts.
  - Leftover releases after aggregate loan dumps.

### 2. Frontend Analytics Chart Updates
- In `web/frontend/src/routes/analytics/+page.svelte`, when updating the `currentBalance` in `assetChartData`, allow `e.balance` to be 0 or any other valid non-negative value (instead of checking `e.balance > 0`). Change the check `e.balance !== undefined && e.balance > 0` to `e.balance !== undefined && e.balance !== null`. This ensures that when an asset is fully paid out and its balance becomes 0, the chart correctly reflects the 0 balance instead of carrying over the last active balance.

## 28. High GPU Usage Diagnosis & Optimization

To address high GPU usage when the application is open (even when idle), we implement the following optimizations to minimize rendering/compositing overhead:

### 1. Monaco Editor Resize Optimization (Eliminating Polling)
- **Problem**: Monaco Editor is configured with `automaticLayout: true`, which spawns a continuous polling loop (using `requestAnimationFrame` or `setInterval`) to check the container size. This keeps both CPU and GPU active constantly even when the editor is idle.
- **Solution**: Set `automaticLayout: false` in `MonacoEditor.svelte` and register a native browser `ResizeObserver` on the editor's container. The observer calls `editor.layout()` only when container bounds actually change, and is cleanly disconnected in `onDestroy()`.

### 2. GPU Rendering Optimizations (Removing Backdrop Blur & Animations)
- **Problem**: Elements using CSS `backdrop-filter: blur(...)` (such as `.glass-card`, `.glass-nav`, and modal backdrop overlays) require the browser's graphics engine to perform expensive copy-blur-composite operations. When page content scrolls or updates, the GPU repaints the blurred regions continuously, causing high GPU utilization. Additionally, hover-based duplicate transaction highlighting and animated warning status elements trigger continuous paint cycles.
- **Solution**: Remove all `backdrop-blur` class references across the frontend layout and components. Replace glassmorphic layers with clean solid or semi-opaque flat backgrounds, and remove GPU layer promotion overrides (`transform: translateZ(0)`). Completely remove duplicate warnings, sibling highlighting, and animated warning/copy icons displayed when hovering over transactions in the realtime view. Remove unnecessary `animate-pulse` classes from transaction warning badges to prevent idle repaints.

## 29. Linking Realtime Accounts to Virtual Accounts

To allow linking realtime (integrated bank) accounts to virtual ones, we dynamically override the starting balance of the virtual account in projections with the latest balance fetched from the linked realtime account.

### 1. Database Schema & Migrations
- **Table**: `virtual_account_versions`
  - Add column `realtime_account_id TEXT DEFAULT ''` to keep track of the linked realtime account in each immutable version.
- **Migration**: Add idempotent migration `029_virtual_account_versions_realtime_account_id` checking `information_schema` and running the `ALTER TABLE` statement.

### 2. Domain & Protobuf Definition
- **Go Domain**: `VirtualAccountVersion` in `web/backend/internal/domain/virtual_account.go` gets `RealtimeAccountID string`.
- **API Protobuf**: `VirtualAccountVersion` in `web/proto/api.proto` gets `string realtime_account_id = 7;`.

### 3. Repository & Handler Mapping
- **Repository**: Update `VirtualAccountRepository.List` and `Save` to include the `realtime_account_id` field.
- **Handler**: Update mapping functions in `virtual_accounts.go` handler to bridge `RealtimeAccountID` between domain models and protobuf.

### 4. Projection Engine Execution
- During scenario initialization in `ProjectionService.RunWithLimit` (in `projection_runner.go`), load the realtime account balance map (by decrypting active integration configs).
- When initializing `vaRunningBalances`, if a virtual account's active version is linked to a realtime account ID, use the realtime account's balance instead of the static version `StartingBalance`.

### 5. Frontend UI Integration
- In `VirtualAccountManager.svelte`, load the realtime accounts via `integrations::accounts::list`.
- Add a custom `SearchableDropdown` to link the virtual account to a realtime account.
- When linked, disable the `StartingBalance` input and display the latest realtime balance.
- Pass the selected `realtimeAccountId` to the `virtualaccounts::save` request.

### 6. Outstanding & Booked Balance Tracking
- If a virtual account has a linked realtime account:
  - The starting balance of the virtual account tracks with the realtime account balance.
  - Inflows and outflows are only booked (applied) if they are **outstanding** and their scheduled day/date is **greater than or equal to the current day** of the month.
  - This prevents double-counting transactions that have already cleared and are included in the realtime balance.
- If not linked:
  - The starting balance is initialized to `0` at the start of each month.
  - All inflows and outflows of the month are fully booked to reveal the final net monthly change.

## 7. Realtime Category Sums & Diff Detection
To support realtime diff detection, we calculate and display a realtime sum for each financial category (Incomes, Bills, Events, Loans, Assets) alongside its planned sum in `BudgetSheet.svelte`.

### Computation Rules
1. **Real/Realtime Amount per Entry**:
   - If an entry has a synced realtime balance (`entry.realtimeBalance !== undefined && entry.realtimeBalance !== null`), we use `entry.realtimeBalance` as its real amount.
   - Otherwise (the entry is outstanding/unbooked), we use its planned `entry.amount`.
2. **Category Sums**:
   - The planned sum of a category is the sum of planned `amount` for all entries in that category.
   - The real sum of a category is the sum of real/realtime amounts for all entries in that category.
3. **Remainder Sum**:
   - `realRemainder = realIncome - realBills - realExpenses - realLoans - realAssets` (adhering to sign conventions).
4. **Presence Check**:
   - The realtime sum of a category is displayed if at least one entry in that category has a synced realtime balance.

### Display Format
- We reuse the outstanding/parentheses format of the virtual account: `Planned (Real)`.
- **Planned Component**: `€ {formatCurrency(planned)}` (existing layout).
- **Real Component**: `(€ {formatCurrency(real)})` styled with `text-[9px] font-bold text-slate-400 dark:text-slate-500 tabular-nums ml-1` (visible only when realtime balances are present in the category).
- This format is applied to both the top summary bar and the individual category table headers.

## 8. Root-Level Folder Structure Refactoring (No `web/` Folder)

To simplify the repository structure and conform to the project evolution guidelines, the contents of the legacy `web/` folder are promoted to the project root:
*   `web/backend/` -> `backend/`
*   `web/frontend/` -> `frontend/`
*   `web/data/` -> `data/`
*   `web/docs/` -> `docs/`
*   `web/proto/` -> `proto/`
*   `web/GEMINI.md` -> `GEMINI.md`
*   `web/.dockerignore` -> `.dockerignore`

### Impacted Subsystems & Configuration Adjustments
1.  **Go Module Path**: The module name in `backend/go.mod` is updated to `github.com/genazt/my-budget-script/backend`. All package imports across the `backend/` codebase are refactored to replace `github.com/genazt/my-budget-script/backend` with `github.com/genazt/my-budget-script/backend`.
2.  **Dockerfiles**:
    *   `backend/Dockerfile` and `backend/Dockerfile.prod` contexts and COPY statements are updated to reflect the new root-level structure.
    *   `frontend/Dockerfile` and `frontend/Dockerfile.prod` copy directives are updated to point to paths relative to the new root build context `./`.
3.  **Docker Compose**:
    *   Build contexts in `docker-compose.yml` are changed from `./web` to `./`.
    *   Volume mounts (e.g. `./web/data`) are updated to `./data`.
    *   Watch paths for hot reloading are updated.
4.  **GitHub Actions Workflow (`.github/workflows/deploy.yml`)**:
    *   Caching dependency paths (`web/backend/go.sum`, etc.) are updated.
    *   Build steps execution directories (e.g. `cd web/backend`) are changed.
    *   Context paths for the docker-build-push actions are adjusted to `./`.
5.  **Protobuf Generation Configuration (`buf.gen.execution.yaml` and Protobuf files)**:
    *   Go package options and module parameters referencing `web/backend` are updated.
6.  **Git Configuration (`.gitignore`)**:
    *   Paths starting with `web/` are updated to match the new root paths.
