const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

// 1. Get database URL from environment or fallback
const dbUrl = process.env.DATABASE_URL || "postgres://budget:budgetpass@db:5432/budget?sslmode=disable";

// 2. Fetch all external_id, correlation_id, and id from database using psql
console.log("Fetching transactions from database...");
let dbTxs = [];
try {
    const query = "SELECT t.id, t.external_id, t.correlation_id, t.created_at, t.account_id, i.name FROM bank_transactions t JOIN integrations i ON t.integration_id = i.id WHERE i.service_type IN ('GOCARDLESS', 'ENABLEBANKING');";
    const output = execSync(`psql -d "${dbUrl}" -t -A -F ',' -c "${query}"`, { encoding: 'utf8' });
    const lines = output.trim().split('\n');
    for (const line of lines) {
        if (!line) continue;
        const [id, externalId, correlationId, createdAt, accountId, integrationName] = line.split(',');
        dbTxs.push({ id, externalId, correlationId, createdAt, accountId, integrationName });
    }
    console.log(`Found ${dbTxs.length} transactions in database.`);
} catch (err) {
    console.error("Failed to query database:", err.message);
    process.exit(1);
}

// 3. Scan logs directory recursively for *_resp.json files
const logsDir = "/app/logs/sync_runs";
console.log(`Scanning logs in ${logsDir}...`);

function getFiles(dir) {
    let results = [];
    if (!fs.existsSync(dir)) return results;
    const list = fs.readdirSync(dir);
    list.forEach(file => {
        const filePath = path.join(dir, file);
        const stat = fs.statSync(filePath);
        if (stat && stat.isDirectory()) {
            results = results.concat(getFiles(filePath));
        } else if (file.endsWith('_resp.json')) {
            results.push(filePath);
        }
    });
    return results;
}

const files = getFiles(logsDir);
console.log(`Found ${files.length} response log files.`);

// 4. Parse transaction details from response logs
const logTxs = new Map(); // key: external_id -> details
const detailsToIds = new Map(); // key: compKey (date|amount|desc|peer) -> Set of external_ids

files.forEach(file => {
    try {
        const content = fs.readFileSync(file, 'utf8');
        const data = JSON.parse(content);
        if (!data || !data.body) return;

        const body = data.body;
        const filename = path.basename(file);

        if (filename.includes('gocardless') && body.transactions && Array.isArray(body.transactions.booked)) {
            body.transactions.booked.forEach(t => {
                const id = t.transactionId || t.internalTransactionId;
                if (!id) return;

                const amount = parseFloat(t.transactionAmount?.amount || "0");
                const date = t.bookingDate || t.bookingDateTime || "";
                const dateStr = date.substring(0, 10);
                const desc = t.remittanceInformationUnstructured || "";
                const peer = t.creditorName || t.debtorName || "";
                const compKey = `${dateStr}|${amount.toFixed(2)}|${desc}|${peer}`;

                const txInfo = { id, amount, date: dateStr, desc, peer, compKey, provider: 'gocardless' };
                logTxs.set(id, txInfo);

                if (!detailsToIds.has(compKey)) detailsToIds.set(compKey, new Set());
                detailsToIds.get(compKey).add(id);
            });
        } else if (filename.includes('enablebanking') && Array.isArray(body.transactions)) {
            body.transactions.forEach(t => {
                const id = t.entry_reference || t.transaction_id;
                if (!id) return;

                const amount = parseFloat(t.transaction_amount?.amount || "0");
                const date = t.booking_date || "";
                const dateStr = date.substring(0, 10);
                const desc = Array.isArray(t.remittance_information) ? t.remittance_information.join(" ") : (t.remittance_information || "");
                const peer = t.creditor?.name || t.debtor?.name || "";
                const compKey = `${dateStr}|${amount.toFixed(2)}|${desc}|${peer}`;

                const txInfo = { id, amount, date: dateStr, desc, peer, compKey, provider: 'enablebanking' };
                logTxs.set(id, txInfo);

                if (!detailsToIds.has(compKey)) detailsToIds.set(compKey, new Set());
                detailsToIds.get(compKey).add(id);
            });
        }
    } catch (err) {
        // Ignore malformed files
    }
});

console.log(`Extracted ${logTxs.size} unique transaction IDs from logs.`);
console.log(`Found ${detailsToIds.size} unique transaction detail signatures.`);

// 5. Diff database transactions against logs
let inDbAndLogs = 0;
let inDbNotLogs = 0;
let inLogsNotDb = 0;

const dbExternalIds = new Set(dbTxs.map(t => t.externalId).filter(id => id));

// Transactions in logs but not in DB
const missingTxs = [];
logTxs.forEach((tx, id) => {
    if (dbExternalIds.has(id)) {
        inDbAndLogs++;
    } else {
        // Check if there is another transaction in DB with the exact same details (but a different external ID)
        let hasDetailMatchInDb = false;
        const siblingIds = detailsToIds.get(tx.compKey);
        if (siblingIds) {
            for (const sibId of siblingIds) {
                if (dbExternalIds.has(sibId)) {
                    hasDetailMatchInDb = true;
                    break;
                }
            }
        }

        if (!hasDetailMatchInDb) {
            inLogsNotDb++;
            missingTxs.push(tx);
        }
    }
});

// Transactions in DB but not in logs (could be older than 30 days log history, or manual, or deleted/other integration)
const dbTxsNotInLogs = [];
dbTxs.forEach(tx => {
    if (tx.externalId && !logTxs.has(tx.externalId)) {
        inDbNotLogs++;
        dbTxsNotInLogs.push(tx);
    }
});

// Identify duplicates in DB (multiple DB transactions sharing the same details signature, determined via log mapping)
const detailsInDb = new Map(); // compKey -> list of db transactions
dbTxs.forEach(tx => {
    if (!tx.externalId) return;
    const logTx = logTxs.get(tx.externalId);
    if (logTx) {
        if (!detailsInDb.has(logTx.compKey)) detailsInDb.set(logTx.compKey, []);
        detailsInDb.get(logTx.compKey).push(tx);
    }
});

let duplicateGroups = 0;
let totalDuplicateTxs = 0;
const duplicateTxsList = [];

const duplicateGroupsByProvider = {};
const duplicateRecordsByProvider = {};

detailsInDb.forEach((txs, compKey) => {
    if (txs.length > 1) {
        duplicateGroups++;
        totalDuplicateTxs += (txs.length - 1);
        const details = logTxs.get(txs[0].externalId);
        const provider = details.provider || 'unknown';

        duplicateGroupsByProvider[provider] = (duplicateGroupsByProvider[provider] || 0) + 1;
        duplicateRecordsByProvider[provider] = (duplicateRecordsByProvider[provider] || 0) + (txs.length - 1);

        duplicateTxsList.push({
            details,
            dbRecords: txs
        });
    }
});

console.log("\n=================== DIFF RESULTS ===================");
console.log(`Matched (In DB & In Logs):   ${inDbAndLogs}`);
console.log(`Missing (In Logs & Not DB):  ${inLogsNotDb}`);
console.log(`Not in Logs (In DB only):     ${inDbNotLogs} (could be manual, old history, or deleted)`);
console.log(`Duplicate Groups in DB:      ${duplicateGroups}`);
console.log(`Total Duplicate Records:     ${totalDuplicateTxs}`);

console.log("\n--- Duplicates by Provider ---");
const providers = Array.from(new Set([...Object.keys(duplicateGroupsByProvider), ...Object.keys(duplicateRecordsByProvider)]));
providers.forEach(p => {
    console.log(`${p}:`);
    console.log(`  Duplicate Groups:  ${duplicateGroupsByProvider[p] || 0}`);
    console.log(`  Duplicate Records: ${duplicateRecordsByProvider[p] || 0}`);
});

if (dbTxsNotInLogs.length > 0) {
    console.log("\n--- Sample Transactions in DB but NOT in Logs ---");
    dbTxsNotInLogs.slice(0, 15).forEach((tx, index) => {
        console.log(`\nSample ${index + 1}:`);
        console.log(`  - DB ID:        ${tx.id}`);
        console.log(`  - ExternalID:   ${tx.externalId}`);
        console.log(`  - CreatedAt:    ${tx.createdAt}`);
        console.log(`  - AccountID:    ${tx.accountId}`);
        console.log(`  - Integration:  ${tx.integrationName}`);
        console.log(`  - CorrelationID: ${tx.correlationId}`);
    });
}

if (duplicateGroups > 0) {
    console.log("\n--- Sample Duplicate Groups ---");
    duplicateTxsList.slice(0, 15).forEach((group, index) => {
        console.log(`\nGroup ${index + 1}: ${group.details.date} | ${group.details.amount.toFixed(2)} EUR | ${group.details.peer} | ${group.details.desc}`);
        group.dbRecords.forEach(r => {
            console.log(`  - DB ID: ${r.id} | ExternalID: ${r.externalId} | CorrelationID: ${r.correlationId}`);
        });
    });
}
