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

