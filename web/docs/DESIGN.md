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
