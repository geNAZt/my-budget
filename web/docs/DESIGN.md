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
1.  **Registration:** The user enters a username. The backend generates a challenge. The browser uses `navigator.credentials.create()` to sign it with biometric/hardware keys. The public key is stored in SQLite.
2.  **Authentication:** The browser uses `navigator.credentials.get()` to sign a login challenge. The backend verifies the signature against the stored public key.

## 3. SQLite Schema Draft

```sql
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT UNIQUE,
    created_at DATETIME
);

CREATE TABLE authenticators (
    id BLOB PRIMARY KEY,
    user_id TEXT,
    public_key BLOB,
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE accounts (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    name TEXT,
    type TEXT, -- 'ASSET', 'LOAN', etc.
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE account_versions (
    id TEXT PRIMARY KEY,
    account_id TEXT,
    version_number INTEGER,
    annual_interest REAL,
    target_goal REAL,
    -- ... other fields from GAS classes
    created_at DATETIME,
    FOREIGN KEY(account_id) REFERENCES accounts(id)
);

CREATE TABLE scenarios (
    id TEXT PRIMARY KEY,
    user_id TEXT,
    name TEXT,
    is_active BOOLEAN,
    FOREIGN KEY(user_id) REFERENCES users(id)
);

CREATE TABLE scenario_versions (
    scenario_id TEXT,
    account_version_id TEXT,
    PRIMARY KEY(scenario_id, account_version_id),
    FOREIGN KEY(scenario_id) REFERENCES scenarios(id),
    FOREIGN KEY(account_version_id) REFERENCES account_versions(id)
);
```

## 4. UX Guidelines

*   **Progressive Disclosure:** Don't show the full 30-year table by default. Show a summary card with "Key Stats" (Total Interest Paid, Payoff Date, Net Worth at Retirement). Clicking the card expands into the detailed chart and table.
*   **Actionable Editing:** When editing a value, show a "Live Preview" sparkline of how that specific change affects the long-term goal.
*   **Terminology:** Use "Human" terms. Instead of "Amortization Schedule," use "Payment History."

## 5. PayPal NVP/SOAP API Integration

To support historical PayPal transaction syncing, we implement a custom, highly secure integration with the legacy PayPal Name-Value Pair (NVP) / SOAP APIs.

### Authentication & Security
*   **Credentials**: The user supplies an API Username (`paypal_user`), API Password (`paypal_password`), and an API Certificate Key (`paypal_certificate`).
*   **Storage**: Credentials are fully encrypted using the Master Integration Key (MIK) and stored securely inside the database `encrypted_config` field.
*   **mTLS Connection**: Mutual TLS (mTLS) is established dynamically using Go's `crypto/tls` package. The PEM certificate key (which contains both the client certificate and the private key) is loaded dynamically via `tls.X509KeyPair` to avoid writing plaintext keys to the filesystem.

### API Workflows
1.  **Balance Sync (`GetBalance` NVP Method)**:
    *   Fetches current PayPal balance(s) with `RETURNALLCURRENCIES=1`.
    *   Priced in primary currency Euro (`EUR`). If `EUR` is not found, falls back to the first available currency index.
    *   Updates the `cached_balance` field of the `Integration` entity.
2.  **Transaction Sync (`TransactionSearch` NVP Method)**:
    *   Searches recent transactions with a configurable start date (incremental delta search).
    *   Returns indexed parameters: `L_TRANSACTIONIDn`, `L_TIMESTAMPn`, `L_TYPEn`, `L_AMTn`, `L_CURRENCYCODEn`, `L_STATUSn`, `L_EMAILn`, `L_NAMEn`.
    *   Generates a stable `ExternalID` for deduplication and self-healing.
    *   Saves serialized JSON metadata into the transaction's encrypted payload for auditability and compliance.

## 6. ETF Interest Accumulation & Virtual Account State Carry-Over

To ensure that ETF assets correctly accumulate interest/growth and that virtual accounts persist their balances correctly over the projection timeline, we implement a unified state initialization and propagation model:

### State Initialization
*   **Virtual Account Balances**: On projection startup, we extract the `StartingBalance` for all active virtual accounts and save them in a running balance map (`vaRunningBalances`).
*   **Asset Initial Balances**: The starting balance of each asset is computed dynamically as the sum of the starting balances of its linked virtual accounts.
*   **ETF Lot Seeding**: If the asset is an ETF and has a non-zero initial balance, we seed `state.lots` with a single initial lot having both `principal` and `currentValue` set to the initial balance. The initial tracker balances (`state.trackerBalances`) are also initialized proportionally based on their target ETF config percentages.

### Month-over-Month State Propagation
*   **Virtual Account Carry-Over**: At the start of each projection month, virtual account starting balances are initialized to their latest running balance (`vaRunningBalances`). At the end of each projection month, the ending balance (`StartingBalance + Inflow - Outflow`) is computed and saved back to `vaRunningBalances` to cleanly propagate the state to the next month.
*   **ETF Compound Growth**: Since initial ETF lots are seeded correctly, the compounding interest/growth loop iterates over all lots, multiplying their values by the simulated monthly rate. The resulting growth is credited to the asset balance and distributed across any active sub-assets.
