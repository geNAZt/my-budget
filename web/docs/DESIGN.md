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

