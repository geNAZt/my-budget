<script lang="ts">
    import { wsCall, onWsEvent } from "$lib/utils/ws_fetch";
    import {
        DiscoveredTransactionListSchema,
        IntegrationListSchema,
        TransactionPoolListSchema,
        TransactionRuleListSchema,
        IntegrationAccountListSchema,
        IntegrationSyncRequestSchema,
        GenericIDSchema,
        TransactionDeleteRequestSchema,
        TransactionUnlinkRequestSchema,
        TransactionLinkRequestSchema,
        TransactionDuplicateRequestSchema,
        TransactionSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    const decode = (obj: any) => JSON.parse(JSON.stringify(obj));

    import { onMount } from "svelte";
    import { auth } from "$lib/stores/auth.svelte";
    import {
        ShieldCheck,
        RefreshCw,
        Loader2,
        CheckCircle2,
        Clock,
        History,
        Search,
        Plus,
        LayoutList,
        LayoutGrid,
        Settings,
        Filter,
        Activity,
        Tags,
        Calendar,
        ChevronLeft,
        ChevronRight,
        X,
        ArrowRight,
        Trash2,
        Check,
        AlertTriangle,
        Zap,
        Copy,
        Pencil,
        GripVertical,
        TrendingUp,
        Waves,
        Upload,
        Euro,
        User,
        CreditCard,
        Hash,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import BudgetSheet from "$lib/components/BudgetSheet.svelte";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import RuleArchitect from "./RuleArchitect.svelte";
    import IntegrationWizard from "./IntegrationWizard.svelte";
    import ChainAccountEditor from "./ChainAccountEditor.svelte";

    function getStored(key: string, def: any) {
        if (typeof localStorage === "undefined") return def;
        const val = localStorage.getItem(key);
        if (val === null || val === "") return def;
        if (Array.isArray(def)) {
            return val.split(",").filter((v) => v !== "");
        }
        if (typeof def === "boolean") {
            return val === "true";
        }
        if (
            typeof def === "number" ||
            (def === null && key.includes("AmountValue"))
        ) {
            const num = Number(val);
            return isNaN(num) ? def : num;
        }
        return val;
    }

    let transactions = $state<any[]>([]);
    let integrations = $state<any[]>([]);
    let pools = $state<any[]>([]);
    let rules = $state<any[]>([]);
    let allAccountsRaw = $state<any[]>([]);
    let mappedAccounts = $state<Record<string, any>>({});
    const allAccounts = $derived(allAccountsRaw);
    let isLoading = $state(true);
    let syncingMap = $state<Record<string, boolean>>({});
    let viewMode = $state<"LEDGER" | "GROUPED" | "CHAINS" | "CONFIG">(
        getStored("realtime_viewMode", "LEDGER"),
    );
    let activeIntegrationIDs = $state<string[]>(
        getStored("realtime_activeIntegrationIDs", []),
    );

    // Filters
    let filterStartDate = $state(getStored("realtime_filterStartDate", ""));
    let filterEndDate = $state(getStored("realtime_filterEndDate", ""));
    let showUnmatchedOnly = $state(
        getStored("realtime_showUnmatchedOnly", false),
    );
    let showDuplicatesOnly = $state(
        getStored("realtime_showDuplicatesOnly", false),
    );
    let showLinkedTransactions = $state(
        getStored("realtime_showLinkedTransactions", false),
    );
    let hoveredTxId = $state<string | null>(null);
    let selectedPoolIDs = $state<string[]>(
        getStored("realtime_selectedPoolIDs", []),
    );
    let selectedAccountID = $state(getStored("realtime_selectedAccountID", ""));
    let txSearchQuery = $state(getStored("realtime_txSearchQuery", ""));
    let filterAmountValue = $state<number | null>(
        getStored("realtime_filterAmountValue", null),
    );
    let filterAmountOperator = $state<string>(
        getStored("realtime_filterAmountOperator", ">="),
    );
    let showDatePopover = $state(false);
    let showPoolsPopover = $state(false);
    let showChainsPopover = $state(false);
    let showAmountPopover = $state(false);

    // Edit Transaction Modal
    let showTransactionEdit = $state(false);
    let transactionToEdit = $state<any>(null);
    let editTagsInput = $state("");
    let editAmountInput = $state<number>(0);
    let editReceiverInput = $state("");
    let editReceiverIbanInput = $state("");
    let editDescriptionInput = $state("");

    // Chain Editor
    let showChainEditor = $state(false);
    let selectedIntegration = $state<any>(null);

    // Calendar State
    let calendarMonth = $state(new Date().getMonth());
    let calendarYear = $state(new Date().getFullYear());
    const monthNames = [
        "January",
        "February",
        "March",
        "April",
        "May",
        "June",
        "July",
        "August",
        "September",
        "October",
        "November",
        "December",
    ];
    const dayNames = ["Mo", "Tu", "We", "Th", "Fr", "Sa", "Su"];

    // Rule Architect State
    let showRuleArchitect = $state(false);
    let showIntegrationWizard = $state(false);

    // Derived States
    const filteredTransactions = $derived.by(() => {
        let list = [...transactions];

        if (txSearchQuery) {
            const q = txSearchQuery.toLowerCase();
            list = list.filter(
                (t) =>
                    getTxDescription(t).toLowerCase().includes(q) ||
                    getTxPeer(t).toLowerCase().includes(q) ||
                    getTxPeerIban(t).toLowerCase().includes(q),
            );
        }

        if (selectedAccountID) {
            list = list.filter((t) => t.accountId === selectedAccountID);
        }

        if (activeIntegrationIDs && activeIntegrationIDs.length > 0) {
            list = list.filter((t) =>
                activeIntegrationIDs.includes(t.integrationId),
            );
        }

        if (selectedPoolIDs.length > 0) {
            list = list.filter((t) => {
                if (selectedPoolIDs.includes("uncategorized") && !t.poolId)
                    return true;
                return selectedPoolIDs.includes(t.poolId);
            });
        }

        if (filterStartDate) {
            list = list.filter(
                (t) =>
                    new Date(t.createdAt).toISOString().split("T")[0] >=
                    filterStartDate,
            );
        }
        if (filterEndDate) {
            list = list.filter(
                (t) =>
                    new Date(t.createdAt).toISOString().split("T")[0] <=
                    filterEndDate,
            );
        }

        if (filterAmountValue !== null) {
            const limit = filterAmountValue;
            list = list.filter((t) => {
                const amt = Math.abs(getTxAmount(t));
                switch (filterAmountOperator) {
                    case ">":
                        return amt > limit;
                    case "<":
                        return amt < limit;
                    case "=":
                        return Math.abs(amt - limit) < 0.01;
                    case ">=":
                        return amt >= limit;
                    case "<=":
                        return amt <= limit;
                    default:
                        return true;
                }
            });
        }

        if (showUnmatchedOnly) {
            list = list.filter((t) => !t.poolId);
        }

        if (showDuplicatesOnly) {
            list = list.filter((t) => t.isPotentialDuplicate);
        }

        if (showLinkedTransactions) {
            list = list.filter((t) => t.isLinkConfirmed || t.potentialLinkId);
        } else {
            list = list.filter((t) => !t.isLinkConfirmed);
        }

        return [...list].sort(
            (a, b) =>
                new Date(b.createdAt).getTime() -
                new Date(a.createdAt).getTime(),
        );
    });

    const filteredTransactionsBalance = $derived(
        filteredTransactions.reduce((acc, t) => acc + getTxAmount(t), 0),
    );

    const groupedTransactions = $derived.by(() => {
        const groups: Record<
            string,
            { name: string; color: string; total: number; count: number }
        > = {};

        filteredTransactions.forEach((t) => {
            const pool = pools.find((p) => p.id === t.poolId);
            const name = pool?.name || "Uncategorized";
            const color = pool?.color || "#cbd5e1";

            if (!groups[name]) {
                groups[name] = { name, color, total: 0, count: 0 };
            }
            groups[name].total += getTxAmount(t);
            groups[name].count++;
        });

        return Object.values(groups).sort((a, b) => b.total - a.total);
    });

    const accountFilterOptions = $derived([
        { id: "", label: "All Accounts" },
        ...allAccounts.map((a) => ({
            id: a.id,
            label: `${a.name} (${a.iban || "No IBAN"})`,
        })),
    ]);

    const accountOptions = $derived(
        allAccounts.map((a) => ({
            id: a.id,
            label: `${a.name} (${a.iban || "No IBAN"})`,
        })),
    );

    const displayIntegrations = $derived(integrations);

    const calendarDays = $derived.by(() => {
        const days = [];
        const firstDay = new Date(calendarYear, calendarMonth, 1);
        const lastDay = new Date(calendarYear, calendarMonth + 1, 0);

        let startOffset = firstDay.getDay() - 1;
        if (startOffset < 0) startOffset = 6;

        // Prev month
        const prevLastDay = new Date(calendarYear, calendarMonth, 0);
        for (let i = startOffset - 1; i >= 0; i--) {
            const d = new Date(
                calendarYear,
                calendarMonth - 1,
                prevLastDay.getDate() - i,
            );
            days.push({
                day: d.getDate(),
                dateStr: d.toISOString().split("T")[0],
                isCurrentMonth: false,
            });
        }

        // Current month
        for (let i = 1; i <= lastDay.getDate(); i++) {
            const d = new Date(calendarYear, calendarMonth, i);
            days.push({
                day: i,
                dateStr: d.toISOString().split("T")[0],
                isCurrentMonth: true,
            });
        }

        // Next month
        const remaining = 42 - days.length;
        for (let i = 1; i <= remaining; i++) {
            const d = new Date(calendarYear, calendarMonth + 1, i);
            days.push({
                day: i,
                dateStr: d.toISOString().split("T")[0],
                isCurrentMonth: false,
            });
        }

        return days;
    });

    const hoveredTargetId = $derived(
        hoveredTxId &&
            transactions.find((t) => t.id === hoveredTxId)?.potentialLinkId,
    );
    const hoveredDuplicateKey = $derived(
        hoveredTxId &&
            transactions.find((t) => t.id === hoveredTxId)?.duplicateKey,
    );

    const deniedTransactions = $derived(
        transactionToEdit && transactionToEdit.deniedDuplicateIds
            ? transactionToEdit.deniedDuplicateIds
                  .split(",")
                  .map((id: string) =>
                      transactions.find((t: any) => t.id === id.trim()),
                  )
                  .filter((t: any) => t !== undefined)
            : [],
    );

    // Effects
    $effect(() => {
        if (typeof localStorage !== "undefined") {
            localStorage.setItem("realtime_viewMode", viewMode);
            localStorage.setItem("realtime_filterStartDate", filterStartDate);
            localStorage.setItem("realtime_filterEndDate", filterEndDate);
            localStorage.setItem(
                "realtime_showUnmatchedOnly",
                String(showUnmatchedOnly),
            );
            localStorage.setItem(
                "realtime_showDuplicatesOnly",
                String(showDuplicatesOnly),
            );
            localStorage.setItem(
                "realtime_showLinkedTransactions",
                String(showLinkedTransactions),
            );
            localStorage.setItem(
                "realtime_selectedPoolIDs",
                selectedPoolIDs.join(","),
            );
            localStorage.setItem(
                "realtime_selectedAccountID",
                selectedAccountID || "",
            );
            localStorage.setItem("realtime_txSearchQuery", txSearchQuery);
            localStorage.setItem(
                "realtime_activeIntegrationIDs",
                activeIntegrationIDs.join(","),
            );
            localStorage.setItem(
                "realtime_filterAmountValue",
                filterAmountValue !== null ? String(filterAmountValue) : "",
            );
            localStorage.setItem(
                "realtime_filterAmountOperator",
                filterAmountOperator,
            );
        }
    });

    // Functions
    async function fetchData(silent = false) {
        if (!silent) isLoading = true;
        console.log("[REALTIME] Starting fetchData...");
        try {
            const [txResp, intResp, poolResp, ruleResp, accResp] =
                await Promise.all([
                    wsCall("integrations::transactions::list", null, null, [
                        DiscoveredTransactionListSchema,
                    ]).one(),
                    wsCall("integrations::list", null, null, [
                        IntegrationListSchema,
                    ]).one(),
                    wsCall("pools::list", null, null, [
                        TransactionPoolListSchema,
                    ]).one(),
                    wsCall("rules::list", null, null, [
                        TransactionRuleListSchema,
                    ]).one(),
                    wsCall("integrations::accounts::list", null, null, [
                        IntegrationAccountListSchema,
                    ]).one(),
                ]);

            console.log("[REALTIME] Received responses:", {
                transactions: txResp[0]?.transactions?.length,
                integrations: intResp[0]?.integrations?.length,
                pools: poolResp[0]?.pools?.length,
                rules: ruleResp[0]?.rules?.length,
                accounts: accResp[0]?.accounts?.length,
            });

            if (
                txResp[1] ||
                intResp[1] ||
                poolResp[1] ||
                ruleResp[1] ||
                accResp[1]
            ) {
                console.warn("[REALTIME] One or more requests had errors:", {
                    txErr: txResp[1],
                    intErr: intResp[1],
                    poolErr: poolResp[1],
                    ruleErr: ruleResp[1],
                    accErr: accResp[1],
                });
            }

            transactions = txResp[0] ? txResp[0].transactions : [];
            integrations = intResp[0] ? intResp[0].integrations : [];
            pools = poolResp[0] ? poolResp[0].pools : [];
            rules = ruleResp[0] ? ruleResp[0].rules : [];

            console.log(
                "[REALTIME] First Integration Object:",
                integrations[0],
            );
            console.log(
                "[REALTIME] First Transaction Object:",
                transactions[0],
            );

            const rawAccounts = accResp[0] ? accResp[0].accounts : [];

            // Deduplicate accounts using a composite key to prevent UI issues if the same account
            // is returned multiple times or linked via multiple integrations.
            const uniqueAccountsMap = new Map();
            for (const acc of rawAccounts) {
                const key = `${acc.integrationId}:${acc.id}`;
                uniqueAccountsMap.set(key, acc);
            }
            allAccountsRaw = Array.from(uniqueAccountsMap.values());

            const accountsArr = allAccountsRaw.map((acc: any) => {
                if (!acc.iban) {
                    acc.iban = acc.id;
                }
                return acc;
            });

            mappedAccounts = accountsArr.reduce((acc: any, curr: any) => {
                const key = `${curr.integrationId}:${curr.id}`;
                acc[key] = curr;
                // Fallback for simple ID lookup (first one wins)
                if (!acc[curr.id]) acc[curr.id] = curr;
                return acc;
            }, {});

            console.log(
                "[REALTIME] State updated. Transactions count:",
                transactions.length,
            );
        } catch (e) {
            console.error("[REALTIME] fetchData failed with exception:", e);
        } finally {
            isLoading = false;
        }
    }

    async function triggerManualSync(id: string) {
        if (syncingMap[id]) return;
        syncingMap[id] = true;
        try {
            const [, err] = await wsCall(
                "integrations::sync",
                IntegrationSyncRequestSchema,
                { id, force: true },
                [GenericIDSchema],
            ).one();
            if (err) throw err;
            await fetchData(true);
        } catch (e: any) {
            console.error(e);
            alert(
                `Network error: Failed to trigger synchronization. ${e.message || ""}`,
            );
        } finally {
            syncingMap[id] = false;
        }
    }

    function getTxDescription(tx: any) {
        if (tx.description) return tx.description;
        if (!tx || !tx.data) return "External Transaction";
        if (tx.data.remittanceInformationUnstructured)
            return tx.data.remittanceInformationUnstructured;
        if (
            tx.data.remittance_information &&
            Array.isArray(tx.data.remittance_information) &&
            tx.data.remittance_information.length > 0
        ) {
            return tx.data.remittance_information.join(" ");
        }
        if (tx.data.reference)
            return `${tx.data.type || ""} ${tx.data.reference}`;
        return tx.data.type || "External Transaction";
    }

    function getTxPeer(tx: any) {
        if (tx.receiver) return tx.receiver;
        if (tx.peer) return tx.peer;
        if (!tx || !tx.data) return "External Peer";
        if (tx.data.debtorName || tx.data.creditorName)
            return tx.data.debtorName || tx.data.creditorName;
        if (tx.data.creditor?.name || tx.data.debtor?.name)
            return tx.data.creditor?.name || tx.data.debtor?.name;
        if (tx.data.reference) return tx.data.reference;
        return "External Peer";
    }

    function getTxPeerIban(tx: any) {
        if (tx.receiverIban) return tx.receiverIban;
        if (!tx || !tx.data) return "";
        return (
            tx.data.creditorAccount?.iban ||
            tx.data.creditor_account?.iban ||
            tx.data.debtorAccount?.iban ||
            tx.data.debtor_account?.iban ||
            tx.data.creditor?.account?.iban ||
            tx.data.debtor?.account?.iban ||
            ""
        );
    }

    function getTxAmount(tx: any) {
        if (tx.amount !== undefined) return tx.amount;
        if (!tx || !tx.data) return 0;
        let amt = 0;
        if (tx.data.transactionAmount)
            amt = parseFloat(tx.data.transactionAmount.amount || 0);
        else if (tx.data.transaction_amount)
            amt = parseFloat(tx.data.transaction_amount.amount || 0);
        else if (tx.data.amount !== undefined) amt = tx.data.amount;

        const desc = getTxDescription(tx);
        if (desc.includes("WITHDRAW")) amt = -Math.abs(amt);
        return amt;
    }

    function getTxAccountName(tx: any) {
        const key = `${tx.integrationId}:${tx.accountId}`;
        return (
            mappedAccounts[key]?.name ||
            mappedAccounts[tx.accountId]?.name ||
            "Unknown Account"
        );
    }

    function getAccountName(id: string) {
        return mappedAccounts[id]?.name || "";
    }

    function openTransactionEdit(tx: any) {
        transactionToEdit = decode(tx);
        editTagsInput = tx.tags || "";
        editAmountInput = getTxAmount(tx);
        editReceiverInput = getTxPeer(tx);
        editReceiverIbanInput = getTxPeerIban(tx);
        editDescriptionInput = getTxDescription(tx);
        showTransactionEdit = true;
    }

    async function saveTransactionEdit() {
        try {
            const [, err] = await wsCall(
                "integrations::transactions::update",
                TransactionSchema,
                {
                    id: transactionToEdit.id,
                    integrationId: transactionToEdit.integrationId || "",
                    accountId: transactionToEdit.accountId || "",
                    amount: editAmountInput,
                    receiver: editReceiverInput,
                    receiverIban: editReceiverIbanInput,
                    description: editDescriptionInput,
                    createdAt: transactionToEdit.createdAt || "",
                    tags: editTagsInput || "",
                    sourceAccountId: transactionToEdit.sourceAccountId || "",
                    destinationAccountId:
                        transactionToEdit.destinationAccountId || "",
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showTransactionEdit = false;
            await fetchData(true);
        } catch (e) {
            console.error(e);
        }
    }

    async function deleteTransaction() {
        if (!transactionToEdit) return;
        if (!confirm("Delete this transaction permanently?")) return;
        try {
            const [, err] = await wsCall(
                "integrations::transactions::delete",
                TransactionDeleteRequestSchema,
                {
                    id: transactionToEdit.id,
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showTransactionEdit = false;
            await fetchData(true);
        } catch (e) {
            console.error(e);
        }
    }

    async function unlinkTransaction(id: string) {
        try {
            const [, err] = await wsCall(
                "integrations::transactions::unlink",
                TransactionUnlinkRequestSchema,
                { id },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showTransactionEdit = false;
            await fetchData(true);
        } catch (e) {
            console.error(e);
        }
    }

    async function confirmLink(id: string, targetId: string) {
        try {
            const [, err] = await wsCall(
                "integrations::transactions::link",
                TransactionLinkRequestSchema,
                {
                    id,
                    targetId: targetId,
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData(true);
        } catch (e) {
            console.error(e);
        }
    }

    async function markAsNotDuplicate() {
        try {
            const [, err] = await wsCall(
                "integrations::transactions::update",
                TransactionSchema,
                {
                    id: transactionToEdit.id,
                    integrationId: transactionToEdit.integrationId || "",
                    accountId: transactionToEdit.accountId || "",
                    amount: transactionToEdit.amount || 0,
                    receiver: transactionToEdit.receiver || "",
                    description: transactionToEdit.description || "",
                    createdAt: transactionToEdit.createdAt || "",
                    tags: transactionToEdit.tags || "",
                    sourceAccountId: transactionToEdit.sourceAccountId || "",
                    destinationAccountId:
                        transactionToEdit.destinationAccountId || "",
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showTransactionEdit = false;
            await fetchData(true);
        } catch (e) {
            console.error(e);
        }
    }

    async function allowDuplicate(deniedId: string) {
        try {
            const [, err] = await wsCall(
                "integrations::transactions::allow_duplicate",
                TransactionDuplicateRequestSchema,
                {
                    id: transactionToEdit.id,
                    deniedId: deniedId,
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData(true);
        } catch (e) {
            console.error(e);
        }
    }

    function formatTimeRemaining(backoffUntil: string | null) {
        if (!backoffUntil) return "Sync Ready";
        const until = new Date(backoffUntil);
        if (until <= new Date()) return "Sync Ready";
        return `Backoff: ${until.toLocaleTimeString()}`;
    }

    function prevMonth() {
        if (calendarMonth === 0) {
            calendarMonth = 11;
            calendarYear--;
        } else {
            calendarMonth--;
        }
    }
    function nextMonth() {
        if (calendarMonth === 11) {
            calendarMonth = 0;
            calendarYear++;
        } else {
            calendarMonth++;
        }
    }
    function selectCalendarDay(dateStr: string) {
        if (!filterStartDate || (filterStartDate && filterEndDate)) {
            filterStartDate = dateStr;
            filterEndDate = "";
        } else {
            if (dateStr < filterStartDate) {
                filterEndDate = filterStartDate;
                filterStartDate = dateStr;
            } else {
                filterEndDate = dateStr;
            }
        }
    }
    function clearDateFilter() {
        filterStartDate = "";
        filterEndDate = "";
    }
    function setLast30Days() {
        const end = new Date();
        const start = new Date();
        start.setDate(end.getDate() - 30);
        filterStartDate = start.toISOString().split("T")[0];
        filterEndDate = end.toISOString().split("T")[0];
    }
    function setLast90Days() {
        const end = new Date();
        const start = new Date();
        start.setDate(end.getDate() - 90);
        filterStartDate = start.toISOString().split("T")[0];
        filterEndDate = end.toISOString().split("T")[0];
    }
    function setThisMonth() {
        const d = new Date();
        filterStartDate = new Date(d.getFullYear(), d.getMonth(), 1)
            .toISOString()
            .split("T")[0];
        filterEndDate = new Date(d.getFullYear(), d.getMonth() + 1, 0)
            .toISOString()
            .split("T")[0];
    }
    function setPreviousMonth() {
        const d = new Date();
        filterStartDate = new Date(d.getFullYear(), d.getMonth() - 1, 1)
            .toISOString()
            .split("T")[0];
        filterEndDate = new Date(d.getFullYear(), d.getMonth(), 0)
            .toISOString()
            .split("T")[0];
    }
    function setThisYear() {
        const d = new Date();
        filterStartDate = new Date(d.getFullYear(), 0, 1)
            .toISOString()
            .split("T")[0];
        filterEndDate = new Date(d.getFullYear(), 11, 31)
            .toISOString()
            .split("T")[0];
    }
    function setPreviousYear() {
        const d = new Date();
        filterStartDate = new Date(d.getFullYear() - 1, 0, 1)
            .toISOString()
            .split("T")[0];
        filterEndDate = new Date(d.getFullYear() - 1, 11, 31)
            .toISOString()
            .split("T")[0];
    }

    onMount(() => {
        fetchData();
        const interval = setInterval(() => fetchData(true), 30000);
        const unsubSync = onWsEvent("sync.finished", (data) => {
            console.log("[WS-EVENT] Sync finished in real time:", data);
            fetchData(true);
        });

        return () => {
            clearInterval(interval);
            unsubSync();
        };
    });
</script>

<svelte:head>
    <title>Realtime Tracker — BudgetScript</title>
</svelte:head>

<div class="space-y-12">
    <!-- Fluid Premium Header -->
    <header
        class="flex flex-col lg:flex-row lg:items-end justify-between gap-8"
    >
        <div class="space-y-3">
            <h1 class="text-5xl font-black tracking-tight text-slate-900">
                Realtime <span class="gradient-text">Stream</span>.
            </h1>
            <p class="text-slate-500 font-medium text-lg">
                Cross-server encrypted background PSD2 & Equity ledger.
            </p>
        </div>
        <div class="flex bg-slate-100 p-1.5 rounded-2xl">
            <button
                onclick={() => (viewMode = "LEDGER")}
                class="flex items-center gap-2 px-5 py-2.5 {viewMode ===
                'LEDGER'
                    ? 'bg-white shadow-sm text-indigo-600'
                    : 'text-slate-500'} rounded-xl text-[10px] font-black uppercase tracking-[0.2em] transition-all"
                ><LayoutList class="w-3.5 h-3.5" /> Transactions</button
            >
            <button
                onclick={() => (viewMode = "GROUPED")}
                class="flex items-center gap-2 px-5 py-2.5 {viewMode ===
                'GROUPED'
                    ? 'bg-white shadow-sm text-indigo-600'
                    : 'text-slate-500'} rounded-xl text-[10px] font-black uppercase tracking-[0.2em] transition-all"
                ><LayoutGrid class="w-3.5 h-3.5" /> Grouped</button
            >
            <button
                onclick={() => (viewMode = "CHAINS")}
                class="flex items-center gap-2 px-5 py-2.5 {viewMode ===
                'CHAINS'
                    ? 'bg-white shadow-sm text-indigo-600'
                    : 'text-slate-500'} rounded-xl text-[10px] font-black uppercase tracking-[0.2em] transition-all"
                ><Activity class="w-3.5 h-3.5" /> Chains</button
            >
            <button
                onclick={() => (viewMode = "CONFIG")}
                class="flex items-center gap-2 px-5 py-2.5 {viewMode ===
                'CONFIG'
                    ? 'bg-white shadow-sm text-indigo-600'
                    : 'text-slate-500'} rounded-xl text-[10px] font-black uppercase tracking-[0.2em] transition-all"
                ><Settings class="w-3.5 h-3.5" /> Config</button
            >
        </div>
    </header>

    <div class="w-full">
        {#if viewMode === "LEDGER" || viewMode === "GROUPED"}
            <div class="space-y-8" transition:fade>
                <!-- Filter Bar (Shared for Ledger & Grouped) -->
                <div
                    class="flex flex-col md:flex-row md:items-center justify-between gap-4 p-4 bg-slate-50 border border-slate-100 rounded-3xl relative"
                >
                    <div class="flex flex-wrap items-center gap-2">
                        <div class="relative w-full md:w-52 shrink-0">
                            <Search
                                class="w-3.5 h-3.5 text-slate-400 absolute left-3.5 top-1/2 -translate-y-1/2"
                            />
                            <input
                                type="text"
                                bind:value={txSearchQuery}
                                placeholder="Search transactions..."
                                class="w-full pl-9 pr-8 py-2 bg-white border border-slate-200 focus:border-indigo-500 rounded-xl font-bold text-[10px] outline-none focus:ring-4 focus:ring-indigo-500/10 transition-all placeholder:text-slate-400"
                            />
                            {#if txSearchQuery}
                                <button
                                    onclick={() => (txSearchQuery = "")}
                                    class="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 hover:text-slate-600"
                                    ><X class="w-3 h-3" /></button
                                >
                            {/if}
                        </div>

                        <div class="w-full md:w-64 shrink-0 -mt-2">
                            <SearchableDropdown
                                options={accountFilterOptions}
                                bind:value={selectedAccountID}
                                placeholder="All Accounts"
                            />
                        </div>

                        {#if showDatePopover}<div
                                role="button"
                                aria-label="Close date popover"
                                tabindex="-1"
                                class="fixed inset-0 z-40"
                                onclick={() => (showDatePopover = false)}
                                onkeydown={() => (showDatePopover = false)}
                            ></div>{/if}
                        {#if showPoolsPopover}<div
                                role="button"
                                aria-label="Close category popover"
                                tabindex="-1"
                                class="fixed inset-0 z-40"
                                onclick={() => (showPoolsPopover = false)}
                                onkeydown={() => (showPoolsPopover = false)}
                            ></div>{/if}
                        {#if showChainsPopover}<div
                                role="button"
                                aria-label="Close integration popover"
                                tabindex="-1"
                                class="fixed inset-0 z-40"
                                onclick={() => (showChainsPopover = false)}
                                onkeydown={() => (showChainsPopover = false)}
                            ></div>{/if}
                        {#if showAmountPopover}<div
                                role="button"
                                aria-label="Close amount popover"
                                tabindex="-1"
                                class="fixed inset-0 z-40"
                                onclick={() => (showAmountPopover = false)}
                                onkeydown={() => (showAmountPopover = false)}
                            ></div>{/if}

                        <div class="relative inline-block">
                            <button
                                onclick={() => {
                                    showAmountPopover = !showAmountPopover;
                                    showDatePopover = false;
                                    showPoolsPopover = false;
                                    showChainsPopover = false;
                                }}
                                class="flex items-center gap-2 px-3 py-2 bg-white border {filterAmountValue !==
                                null
                                    ? 'border-indigo-500 text-indigo-600 font-black'
                                    : 'border-slate-200 text-slate-500'} rounded-xl text-[9px] font-black uppercase tracking-wider hover:border-indigo-600 hover:text-indigo-600 transition-all shadow-sm shrink-0"
                            >
                                <Filter
                                    class="w-3.5 h-3.5 {filterAmountValue !==
                                    null
                                        ? 'text-indigo-600'
                                        : 'text-slate-400'}"
                                />
                                <span
                                    >{filterAmountValue !== null &&
                                    !isNaN(filterAmountValue)
                                        ? `${filterAmountOperator} ${filterAmountValue.toLocaleString(
                                              "de-DE",
                                              {
                                                  minimumFractionDigits: 2,
                                                  maximumFractionDigits: 2,
                                              },
                                          )} €`
                                        : "Any Amount"}</span
                                >
                            </button>
                            {#if showAmountPopover}
                                <div
                                    class="absolute top-full left-0 mt-2 w-[240px] bg-white border border-slate-100 rounded-[30px] shadow-2xl p-5 z-50 space-y-4"
                                    transition:fade
                                >
                                    <div
                                        class="flex items-center justify-between border-b border-slate-50 pb-2"
                                    >
                                        <span
                                            class="text-[9px] font-black uppercase text-slate-400"
                                            >Amount Filter</span
                                        >
                                        {#if filterAmountValue !== null}<button
                                                onclick={() =>
                                                    (filterAmountValue = null)}
                                                class="text-[8px] font-black text-red-500 uppercase hover:underline"
                                                >Clear</button
                                            >{/if}
                                    </div>
                                    <div class="grid grid-cols-5 gap-1.5">
                                        {#each [">", "<", "=", ">=", "<="] as op}
                                            <button
                                                onclick={() =>
                                                    (filterAmountOperator = op)}
                                                class="px-2 py-2 rounded-lg border text-[10px] font-black transition-all {filterAmountOperator ===
                                                op
                                                    ? 'bg-indigo-600 border-indigo-600 text-white shadow-lg shadow-indigo-100'
                                                    : 'bg-white border-slate-100 text-slate-500 hover:border-indigo-200 hover:text-indigo-600'}"
                                            >
                                                {op}
                                            </button>
                                        {/each}
                                    </div>
                                    <div class="relative">
                                        <input
                                            type="number"
                                            step="0.01"
                                            bind:value={filterAmountValue}
                                            placeholder="0,00"
                                            class="w-full px-4 py-2 bg-slate-50 border border-slate-100 rounded-xl font-bold text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 transition-all placeholder:text-slate-300"
                                        />
                                    </div>
                                </div>
                            {/if}
                        </div>

                        <div class="relative inline-block">
                            <button
                                onclick={() => {
                                    showDatePopover = !showDatePopover;
                                    showPoolsPopover = false;
                                    showChainsPopover = false;
                                    showAmountPopover = false;
                                }}
                                class="flex items-center gap-2 px-3 py-2 bg-white border {filterStartDate ||
                                filterEndDate
                                    ? 'border-indigo-500 text-indigo-600 font-black'
                                    : 'border-slate-200 text-slate-500'} rounded-xl text-[9px] font-black uppercase tracking-wider hover:border-indigo-600 hover:text-indigo-600 transition-all shadow-sm shrink-0"
                            >
                                <Calendar
                                    class="w-3.5 h-3.5 {filterStartDate ||
                                    filterEndDate
                                        ? 'text-indigo-600'
                                        : 'text-slate-400'}"
                                />
                                <span
                                    >{filterStartDate &&
                                    !isNaN(new Date(filterStartDate).getTime())
                                        ? filterEndDate &&
                                          !isNaN(
                                              new Date(filterEndDate).getTime(),
                                          )
                                            ? `${new Date(filterStartDate).toLocaleDateString("de-DE", { day: "numeric", month: "short" })} - ${new Date(filterEndDate).toLocaleDateString("de-DE", { day: "numeric", month: "short" })}`
                                            : `${new Date(filterStartDate).toLocaleDateString("de-DE", { day: "numeric", month: "short" })}...`
                                        : "All Horizon"}</span
                                >
                            </button>
                            {#if showDatePopover}
                                <div
                                    class="absolute top-full left-0 mt-2 w-[280px] bg-white border border-slate-100 rounded-[30px] shadow-2xl p-5 z-50 space-y-4"
                                    transition:fade
                                >
                                    <div
                                        class="flex items-center justify-between border-b border-slate-50 pb-2"
                                    >
                                        <span
                                            class="text-[9px] font-black uppercase text-slate-400"
                                            >Select Range</span
                                        >
                                        {#if filterStartDate || filterEndDate}<button
                                                onclick={clearDateFilter}
                                                class="text-[8px] font-black text-red-500 uppercase hover:underline"
                                                >Clear</button
                                            >{/if}
                                    </div>
                                    <div
                                        class="flex items-center justify-between bg-slate-50 border border-slate-100 p-2 rounded-lg"
                                    >
                                        <button
                                            onclick={prevMonth}
                                            class="p-1 hover:bg-slate-200/60 rounded-md text-slate-600 transition-colors"
                                            ><ChevronLeft
                                                class="w-3.5 h-3.5"
                                            /></button
                                        >
                                        <span
                                            class="text-[10px] font-black text-slate-800 uppercase tracking-tight"
                                            >{monthNames[calendarMonth]}
                                            {calendarYear}</span
                                        >
                                        <button
                                            onclick={nextMonth}
                                            class="p-1 hover:bg-slate-200/60 rounded-md text-slate-600 transition-colors"
                                            ><ChevronRight
                                                class="w-3.5 h-3.5"
                                            /></button
                                        >
                                    </div>
                                    <div class="space-y-1.5">
                                        <div
                                            class="grid grid-cols-7 text-center"
                                        >
                                            {#each dayNames as day}<span
                                                    class="text-[8px] font-black uppercase text-slate-400 py-0.5"
                                                    >{day}</span
                                                >{/each}
                                        </div>
                                        <div class="grid grid-cols-7 gap-0.5">
                                            {#each calendarDays as cell (cell.dateStr)}
                                                {@const isStart =
                                                    cell.dateStr ===
                                                    filterStartDate}
                                                {@const isEnd =
                                                    cell.dateStr ===
                                                    filterEndDate}
                                                {@const inRange =
                                                    filterStartDate &&
                                                    filterEndDate &&
                                                    cell.dateStr >
                                                        filterStartDate &&
                                                    cell.dateStr <
                                                        filterEndDate}
                                                <button
                                                    onclick={() =>
                                                        selectCalendarDay(
                                                            cell.dateStr,
                                                        )}
                                                    class="aspect-square flex items-center justify-center text-[9px] font-black rounded transition-all relative {cell.isCurrentMonth
                                                        ? 'text-slate-800'
                                                        : 'text-slate-300'} {isStart ||
                                                    isEnd
                                                        ? 'bg-indigo-600 text-white shadow-sm font-black'
                                                        : ''} {inRange
                                                        ? 'bg-indigo-50 text-indigo-700 rounded-none'
                                                        : ''} {isStart &&
                                                    filterEndDate
                                                        ? 'rounded-r-none'
                                                        : ''} {isEnd &&
                                                    filterStartDate
                                                        ? 'rounded-l-none'
                                                        : ''} {!isStart &&
                                                    !isEnd &&
                                                    !inRange
                                                        ? 'hover:bg-slate-100'
                                                        : ''}"
                                                >
                                                    {cell.day}
                                                </button>
                                            {/each}
                                        </div>
                                    </div>
                                    <div
                                        class="grid grid-cols-2 gap-1.5 pt-2 border-t border-slate-50"
                                    >
                                        <button
                                            onclick={() => {
                                                setLast30Days();
                                                showDatePopover = false;
                                            }}
                                            class="py-1 px-1.5 bg-slate-50 hover:bg-indigo-50 hover:text-indigo-600 rounded-lg font-black text-[8px] uppercase tracking-wider text-slate-500 text-center transition-all"
                                            >30 Days</button
                                        >
                                        <button
                                            onclick={() => {
                                                setLast90Days();
                                                showDatePopover = false;
                                            }}
                                            class="py-1 px-1.5 bg-slate-50 hover:bg-indigo-50 hover:text-indigo-600 rounded-lg font-black text-[8px] uppercase tracking-wider text-slate-500 text-center transition-all"
                                            >90 Days</button
                                        >
                                        <button
                                            onclick={() => {
                                                setThisMonth();
                                                showDatePopover = false;
                                            }}
                                            class="py-1 px-1.5 bg-slate-50 hover:bg-indigo-50 hover:text-indigo-600 rounded-lg font-black text-[8px] uppercase tracking-wider text-slate-500 text-center transition-all"
                                            >This Month</button
                                        >
                                        <button
                                            onclick={() => {
                                                setPreviousMonth();
                                                showDatePopover = false;
                                            }}
                                            class="py-1 px-1.5 bg-slate-50 hover:bg-indigo-50 hover:text-indigo-600 rounded-lg font-black text-[8px] uppercase tracking-wider text-slate-500 text-center transition-all"
                                            >Prev Month</button
                                        >
                                        <button
                                            onclick={() => {
                                                setThisYear();
                                                showDatePopover = false;
                                            }}
                                            class="py-1 px-1.5 bg-slate-50 hover:bg-indigo-50 hover:text-indigo-600 rounded-lg font-black text-[8px] uppercase tracking-wider text-slate-500 text-center transition-all"
                                            >This Year</button
                                        >
                                        <button
                                            onclick={() => {
                                                setPreviousYear();
                                                showDatePopover = false;
                                            }}
                                            class="py-1 px-1.5 bg-slate-50 hover:bg-indigo-50 hover:text-indigo-600 rounded-lg font-black text-[8px] uppercase tracking-wider text-slate-500 text-center transition-all"
                                            >Prev Year</button
                                        >
                                    </div>
                                </div>
                            {/if}
                        </div>

                        <div class="relative inline-block">
                            <button
                                onclick={() => {
                                    showPoolsPopover = !showPoolsPopover;
                                    showDatePopover = false;
                                    showChainsPopover = false;
                                    showAmountPopover = false;
                                }}
                                class="flex items-center gap-2 px-3 py-2 bg-white border {selectedPoolIDs.length >
                                0
                                    ? 'border-indigo-500 text-indigo-600 font-black'
                                    : 'border-slate-200 text-slate-500'} rounded-xl text-[9px] font-black uppercase tracking-wider hover:border-indigo-600 hover:text-indigo-600 transition-all shadow-sm shrink-0"
                            >
                                <Tags
                                    class="w-3.5 h-3.5 {selectedPoolIDs.length >
                                    0
                                        ? 'text-indigo-600'
                                        : 'text-slate-400'}"
                                />
                                <span
                                    >{selectedPoolIDs.length === 0
                                        ? "All Pools"
                                        : `Pools: ${selectedPoolIDs.length}`}</span
                                >
                            </button>
                            {#if showPoolsPopover}
                                <div
                                    class="absolute top-full left-0 mt-2 w-64 bg-white border border-slate-100 rounded-[30px] shadow-2xl p-5 z-50 space-y-4"
                                    transition:fade
                                >
                                    <div
                                        class="flex items-center justify-between border-b border-slate-50 pb-2"
                                    >
                                        <span
                                            class="text-[9px] font-black uppercase text-slate-400"
                                            >Filter by Pools</span
                                        >
                                        {#if selectedPoolIDs.length > 0}<button
                                                onclick={() =>
                                                    (selectedPoolIDs = [])}
                                                class="text-[8px] font-black text-red-500 uppercase hover:underline"
                                                >Clear</button
                                            >{/if}
                                    </div>
                                    <div
                                        class="max-h-60 overflow-y-auto space-y-1 pr-1 custom-scrollbar"
                                    >
                                        <label
                                            class="flex items-center gap-3 p-2 hover:bg-slate-50 rounded-xl cursor-pointer transition-colors"
                                        >
                                            <input
                                                type="checkbox"
                                                checked={selectedPoolIDs.includes(
                                                    "uncategorized",
                                                )}
                                                onchange={(e: any) => {
                                                    if (e.target.checked)
                                                        selectedPoolIDs = [
                                                            ...selectedPoolIDs,
                                                            "uncategorized",
                                                        ];
                                                    else
                                                        selectedPoolIDs =
                                                            selectedPoolIDs.filter(
                                                                (id) =>
                                                                    id !==
                                                                    "uncategorized",
                                                            );
                                                }}
                                                class="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500 w-3.5 h-3.5"
                                            />
                                            <div
                                                class="w-2.5 h-2.5 rounded-full bg-slate-300 shrink-0"
                                            ></div>
                                            <span
                                                class="text-xs font-bold text-slate-700"
                                                >Uncategorized</span
                                            >
                                        </label>
                                        {#each pools || [] as pool}
                                            <label
                                                class="flex items-center gap-3 p-2 hover:bg-slate-50 rounded-xl cursor-pointer transition-colors"
                                            >
                                                <input
                                                    type="checkbox"
                                                    checked={selectedPoolIDs.includes(
                                                        pool.id,
                                                    )}
                                                    onchange={(e: any) => {
                                                        if (e.target.checked)
                                                            selectedPoolIDs = [
                                                                ...selectedPoolIDs,
                                                                pool.id,
                                                            ];
                                                        else
                                                            selectedPoolIDs =
                                                                selectedPoolIDs.filter(
                                                                    (id) =>
                                                                        id !==
                                                                        pool.id,
                                                                );
                                                    }}
                                                    class="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500 w-3.5 h-3.5"
                                                />
                                                <div
                                                    class="w-2.5 h-2.5 rounded-full shrink-0"
                                                    style="background: {pool.color}"
                                                ></div>
                                                <span
                                                    class="text-xs font-bold text-slate-700 truncate"
                                                    >{pool.name}</span
                                                >
                                            </label>
                                        {/each}
                                    </div>
                                </div>
                            {/if}
                        </div>

                        <div class="relative inline-block">
                            <button
                                onclick={() => {
                                    showChainsPopover = !showChainsPopover;
                                    showDatePopover = false;
                                    showPoolsPopover = false;
                                    showAmountPopover = false;
                                }}
                                class="flex items-center gap-2 px-3 py-2 bg-white border {activeIntegrationIDs.length >
                                0
                                    ? 'border-indigo-500 text-indigo-600 font-black'
                                    : 'border-slate-200 text-slate-500'} rounded-xl text-[9px] font-black uppercase tracking-wider hover:border-indigo-600 hover:text-indigo-600 transition-all shadow-sm shrink-0"
                            >
                                <Activity
                                    class="w-3.5 h-3.5 {activeIntegrationIDs.length >
                                    0
                                        ? 'text-indigo-600'
                                        : 'text-slate-400'}"
                                />
                                <span
                                    >{activeIntegrationIDs.length === 0
                                        ? "All Chains"
                                        : `Chains: ${activeIntegrationIDs.length}`}</span
                                >
                            </button>
                            {#if showChainsPopover}
                                <div
                                    class="absolute top-full left-0 mt-2 w-64 bg-white border border-slate-100 rounded-[30px] shadow-2xl p-5 z-50 space-y-4"
                                    transition:fade
                                >
                                    <div
                                        class="flex items-center justify-between border-b border-slate-50 pb-2"
                                    >
                                        <span
                                            class="text-[9px] font-black uppercase text-slate-400"
                                            >Filter by Chains</span
                                        >
                                        {#if activeIntegrationIDs.length > 0}<button
                                                onclick={() =>
                                                    (activeIntegrationIDs = [])}
                                                class="text-[8px] font-black text-red-500 uppercase hover:underline"
                                                >Clear</button
                                            >{/if}
                                    </div>
                                    <div
                                        class="max-h-60 overflow-y-auto space-y-1 pr-1 custom-scrollbar"
                                    >
                                        {#each displayIntegrations as i}
                                            <label
                                                class="flex items-center gap-3 p-2 hover:bg-slate-50 rounded-xl cursor-pointer transition-colors"
                                            >
                                                <input
                                                    type="checkbox"
                                                    checked={activeIntegrationIDs.includes(
                                                        i.integrationId,
                                                    )}
                                                    onchange={(e: any) => {
                                                        if (e.target.checked)
                                                            activeIntegrationIDs =
                                                                [
                                                                    ...activeIntegrationIDs,
                                                                    i.integrationId,
                                                                ];
                                                        else
                                                            activeIntegrationIDs =
                                                                activeIntegrationIDs.filter(
                                                                    (id) =>
                                                                        id !==
                                                                        i.integrationId,
                                                                );
                                                    }}
                                                    class="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500 w-3.5 h-3.5"
                                                />
                                                <div
                                                    class="w-5 h-5 bg-slate-100 rounded flex items-center justify-center shrink-0"
                                                >
                                                    {#if i.serviceType === "TRADING212"}<Zap
                                                            class="w-3 h-3 text-indigo-600"
                                                        />{:else}<ShieldCheck
                                                            class="w-3 h-3 text-slate-400"
                                                        />{/if}
                                                </div>
                                                <span
                                                    class="text-xs font-bold text-slate-700 truncate"
                                                    >{i.integrationName ||
                                                        "Unnamed Chain"}</span
                                                >
                                            </label>
                                        {/each}
                                    </div>
                                </div>
                            {/if}
                        </div>

                        <div
                            class="h-4 w-px bg-slate-200 mx-2 hidden lg:block"
                        ></div>

                        <div class="px-1 hidden md:block">
                            <span
                                class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                            >
                                {filteredTransactions.length} Transactions | €{filteredTransactionsBalance.toLocaleString(
                                    "de-DE",
                                    {
                                        minimumFractionDigits: 2,
                                        maximumFractionDigits: 2,
                                    },
                                )}
                            </span>
                        </div>
                    </div>

                    <div class="flex items-center gap-2 flex-wrap">
                        {#if viewMode === "LEDGER"}
                            <button
                                onclick={() =>
                                    (showUnmatchedOnly = !showUnmatchedOnly)}
                                class="flex items-center gap-1.5 px-3 py-2 border transition-all rounded-xl text-[9px] font-black uppercase tracking-wider shadow-sm {showUnmatchedOnly
                                    ? 'bg-purple-600 border-purple-600 text-white'
                                    : 'bg-white border-slate-200 text-slate-500 hover:border-indigo-600 hover:text-indigo-600'}"
                            >
                                <Filter class="w-3.5 h-3.5" />
                                <span>Unmatched</span>
                            </button>
                            <button
                                onclick={() =>
                                    (showDuplicatesOnly = !showDuplicatesOnly)}
                                class="flex items-center gap-1.5 px-3 py-2 border transition-all rounded-xl text-[9px] font-black uppercase tracking-wider shadow-sm {showDuplicatesOnly
                                    ? 'bg-amber-600 border-amber-600 text-white'
                                    : 'bg-white border-slate-200 text-slate-500 hover:border-indigo-600 hover:text-indigo-600'}"
                            >
                                <AlertTriangle class="w-3.5 h-3.5" />
                                <span>Duplicates</span>
                            </button>
                            <button
                                onclick={() =>
                                    (showLinkedTransactions =
                                        !showLinkedTransactions)}
                                class="flex items-center gap-1.5 px-3 py-2 border transition-all rounded-xl text-[9px] font-black uppercase tracking-wider shadow-sm {showLinkedTransactions
                                    ? 'bg-indigo-600 border-indigo-600 text-white'
                                    : 'bg-white border-slate-200 text-slate-500 hover:border-indigo-600 hover:text-indigo-600'}"
                            >
                                <Activity class="w-3.5 h-3.5" />
                                <span>Linked</span>
                                </button>
                                {/if}

                                {#if filterStartDate || filterEndDate || activeIntegrationIDs.length > 0 || selectedPoolIDs.length > 0 || selectedAccountID || txSearchQuery || showUnmatchedOnly || showDuplicatesOnly || showLinkedTransactions || filterAmountValue !== null}
                            <button
                                onclick={() => {
                                    filterStartDate = "";
                                    filterEndDate = "";
                                    activeIntegrationIDs = [];
                                    selectedPoolIDs = [];
                                    selectedAccountID = "";
                                    txSearchQuery = "";
                                    showUnmatchedOnly = false;
                                    showDuplicatesOnly = false;
                                    showLinkedTransactions = false;
                                    filterAmountValue = null;
                                }}
                                class="px-3 py-2 bg-red-50 hover:bg-red-100 text-red-600 border border-red-200/50 rounded-xl text-[9px] font-black uppercase tracking-wider transition-all"
                                >Reset</button
                            >
                        {/if}
                    </div>
                </div>

                {#if viewMode === "LEDGER"}
                    <div class="glass-card p-10 space-y-8">
                        <div class="space-y-4">
                            {#each filteredTransactions as tx, i (tx.id)}
                                {@const currentTxDate = new Date(
                                    tx.createdAt,
                                ).toLocaleDateString("de-DE", {
                                    year: "numeric",
                                    month: "long",
                                    day: "numeric",
                                })}
                                {@const prevTxDate =
                                    i > 0
                                        ? new Date(
                                              filteredTransactions[i - 1]
                                                  .createdAt,
                                          ).toLocaleDateString("de-DE", {
                                              year: "numeric",
                                              month: "long",
                                              day: "numeric",
                                          })
                                        : null}
                                {@const showDateSeparator =
                                    i === 0 || currentTxDate !== prevTxDate}

                                {#if showDateSeparator}
                                    <div
                                        class="flex items-center gap-4 pt-4 pb-2"
                                    >
                                        <div
                                            class="h-px bg-slate-200 flex-1"
                                        ></div>
                                        <span
                                            class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                                            >{currentTxDate}</span
                                        >
                                        <div
                                            class="h-px bg-slate-200 flex-1"
                                        ></div>
                                    </div>
                                {/if}

                                {@const pool = (pools || []).find(
                                    (p) => p.id === tx.poolId,
                                )}
                                {@const isHovered = tx.id === hoveredTxId}
                                {@const isLinkTarget =
                                    tx.id === hoveredTargetId}
                                {@const isDuplicateSibling =
                                    hoveredDuplicateKey &&
                                    tx.duplicateKey === hoveredDuplicateKey &&
                                    tx.id !== hoveredTxId}

                                <div
                                    onclick={() => openTransactionEdit(tx)}
                                    onmouseenter={() => (hoveredTxId = tx.id)}
                                    onmouseleave={() => (hoveredTxId = null)}
                                    role="button"
                                    tabindex="0"
                                    onkeydown={(e) =>
                                        e.key === "Enter" &&
                                        openTransactionEdit(tx)}
                                    class="w-full flex items-center gap-6 p-5 bg-white border rounded-3xl transition-all group text-left cursor-pointer relative
                                   {isHovered
                                        ? 'z-20 scale-[1.01] shadow-xl shadow-slate-200/50'
                                        : ''}
                                   {isHovered && hoveredTargetId
                                        ? 'border-indigo-600 ring-2 ring-indigo-600/10'
                                        : ''}
                                   {isHovered && hoveredDuplicateKey
                                        ? 'border-amber-500 ring-2 ring-amber-500/10'
                                        : ''}
                                   {isLinkTarget
                                        ? 'border-indigo-600 ring-2 ring-indigo-600/10 shadow-lg shadow-indigo-100/50 z-10 scale-[1.01]'
                                        : ''}
                                   {isDuplicateSibling
                                        ? 'border-amber-500 ring-2 ring-amber-500/10 shadow-lg shadow-amber-100/50 z-10 scale-[1.01]'
                                        : ''}
                                   {!isHovered &&
                                    !isLinkTarget &&
                                    !isDuplicateSibling
                                        ? 'border-slate-100 hover:border-indigo-200'
                                        : ''}"
                                >
                                    {#if isHovered && hoveredTargetId}
                                        <div
                                            class="absolute -left-10 top-1/2 -translate-y-1/2 text-indigo-600 animate-pulse hidden lg:block"
                                        >
                                            <Activity class="w-6 h-6" />
                                        </div>
                                    {/if}
                                    {#if isLinkTarget}
                                        <div
                                            class="absolute -left-10 top-1/2 -translate-y-1/2 text-indigo-500 animate-bounce-x hidden lg:block"
                                        >
                                            <ArrowRight class="w-6 h-6" />
                                        </div>
                                    {/if}
                                    {#if isHovered && hoveredDuplicateKey}
                                        <div
                                            class="absolute -left-10 top-1/2 -translate-y-1/2 text-amber-500 animate-pulse hidden lg:block"
                                        >
                                            <AlertTriangle class="w-6 h-6" />
                                        </div>
                                    {/if}
                                    {#if isDuplicateSibling}
                                        <div
                                            class="absolute -left-10 top-1/2 -translate-y-1/2 text-amber-500 animate-bounce-x hidden lg:block"
                                        >
                                            <Copy class="w-6 h-6" />
                                        </div>
                                    {/if}
                                    <div class="flex-1 min-w-0">
                                        <div
                                            class="flex items-center gap-3 mb-1"
                                        >
                                            <h4
                                                class="font-black text-slate-900 truncate uppercase tracking-tight"
                                            >
                                                {getTxDescription(tx)}
                                            </h4>
                                            {#if pool}<span
                                                    class="px-2 py-0.5 text-[8px] font-black rounded-lg uppercase"
                                                    style="background: {pool.color}15; color: {pool.color}"
                                                    >{pool.name}</span
                                                >{/if}
                                            {#if tx.tags}
                                                {#each tx.tags.split(",") as tag}
                                                    {#if tag.trim()}
                                                        <span
                                                            class="px-2 py-0.5 bg-slate-100 text-slate-500 text-[8px] font-black rounded-lg uppercase"
                                                            >{tag.trim()}</span
                                                        >
                                                    {/if}
                                                {/each}
                                            {/if}
                                            {#if tx.isPotentialDuplicate}
                                                <span
                                                    class="flex items-center gap-1 px-2 py-0.5 bg-amber-50 text-amber-600 border border-amber-200/50 text-[8px] font-black rounded-lg uppercase animate-pulse"
                                                    title="Potential duplicate detected (same day, same amount, same description & accounts)"
                                                >
                                                    <AlertTriangle
                                                        class="w-3 h-3 text-amber-500"
                                                    />
                                                    Duplicate Warning
                                                </span>
                                            {/if}
                                            {#if tx.hasWrongExternalId}
                                                <span
                                                    class="flex items-center gap-1 px-2 py-0.5 bg-rose-50 text-rose-600 border border-rose-200/50 text-[8px] font-black rounded-lg uppercase animate-pulse"
                                                    title="Incorrect external ID in DB: {tx.externalId} (Expected True ID: {tx.correctExternalId})"
                                                >
                                                    <AlertTriangle
                                                        class="w-3 h-3 text-rose-500"
                                                    />
                                                    Invalid Ext-ID
                                                </span>
                                            {/if}
                                            {#if tx.correlationId}
                                                <button
                                                    type="button"
                                                    class="flex items-center gap-1 px-2 py-0.5 bg-slate-100 text-slate-600 border border-slate-200/50 text-[8px] font-mono rounded-lg uppercase cursor-pointer hover:bg-slate-200 hover:text-slate-900 transition-colors"
                                                    title="Sync Correlation ID: {tx.correlationId} (Click to copy)"
                                                    onclick={(e) => {
                                                        e.stopPropagation();
                                                        navigator.clipboard.writeText(
                                                            tx.correlationId,
                                                        );
                                                    }}
                                                >
                                                    <span
                                                        class="w-1.5 h-1.5 rounded-full bg-slate-400"
                                                    ></span>
                                                    Sync: {tx.correlationId.substring(
                                                        0,
                                                        8,
                                                    )}...
                                                </button>
                                            {/if}
                                            {#if tx.isLinkConfirmed}
                                                <span
                                                    class="flex items-center gap-1 px-2 py-0.5 bg-indigo-50 text-indigo-600 border border-indigo-200/50 text-[8px] font-black rounded-lg uppercase"
                                                >
                                                    <Activity
                                                        class="w-3 h-3 text-indigo-500"
                                                    />
                                                    Linked
                                                </span>
                                            {/if}
                                            {#if tx.potentialLinkId && !tx.isLinkConfirmed}
                                                <div
                                                    class="flex items-center gap-2"
                                                >
                                                    <span
                                                        class="flex items-center gap-1 px-2 py-0.5 bg-emerald-50 text-emerald-600 border border-emerald-200/50 text-[8px] font-black rounded-lg uppercase"
                                                    >
                                                        <Zap
                                                            class="w-3 h-3 text-emerald-500"
                                                        />
                                                        Potential Link Detected
                                                    </span>
                                                    <button
                                                        onclick={(e) => {
                                                            e.stopPropagation();
                                                            confirmLink(
                                                                tx.id,
                                                                tx.potentialLinkId,
                                                            );
                                                        }}
                                                        class="px-2 py-0.5 bg-emerald-600 text-white text-[8px] font-black rounded-lg uppercase hover:bg-emerald-700 transition-colors"
                                                        >Confirm Link</button
                                                    >
                                                </div>
                                            {/if}
                                        </div>
                                        <div class="flex items-center gap-2">
                                            <p
                                                class="text-[10px] font-bold text-slate-400 uppercase"
                                            >
                                                {new Date(
                                                    tx.createdAt,
                                                ).toLocaleString("de-DE", {
                                                    year: "numeric",
                                                    month: "2-digit",
                                                    day: "2-digit",
                                                    hour: "2-digit",
                                                    minute: "2-digit",
                                                })}
                                            </p>
                                            <div
                                                class="flex items-center gap-1.5 text-[10px] font-bold uppercase truncate"
                                            >
                                                <span class="text-slate-300"
                                                    >{getTxAccountName(
                                                        tx,
                                                    )}</span
                                                >
                                                {#if getAccountName(tx.destinationAccountId)}
                                                    <ArrowRight
                                                        class="w-3 h-3 text-slate-300"
                                                    />
                                                    <span
                                                        class="text-indigo-400"
                                                        >{getAccountName(
                                                            tx.destinationAccountId,
                                                        )}</span
                                                    >
                                                {/if}
                                            </div>
                                        </div>
                                    </div>
                                    <div class="text-right">
                                        <p
                                            class="text-lg font-black text-slate-900 tabular-nums"
                                        >
                                            €{getTxAmount(tx).toLocaleString(
                                                "de-DE",
                                                {
                                                    minimumFractionDigits: 2,
                                                    maximumFractionDigits: 2,
                                                },
                                            )}
                                        </p>
                                        <p
                                            class="text-[9px] font-bold text-slate-400 uppercase tracking-[0.2em]"
                                        >
                                            {getTxPeer(tx)}
                                        </p>
                                        {#if getTxPeerIban(tx)}
                                            <p
                                                class="text-[8px] font-bold text-slate-300 uppercase tracking-[0.2em] mt-0.5"
                                            >
                                                {getTxPeerIban(tx)}
                                            </p>
                                        {/if}
                                    </div>
                                </div>
                            {/each}
                        </div>
                    </div>
                {:else if viewMode === "GROUPED"}
                    <div
                        class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-8"
                    >
                        {#each groupedTransactions as group}
                            <div
                                role="button"
                                tabindex="0"
                                class="glass-card p-10 space-y-8 group hover:border-indigo-200 transition-all cursor-pointer"
                                onclick={() => {
                                    selectedPoolIDs = [
                                        pools.find((p) => p.name === group.name)
                                            ?.id || "uncategorized",
                                    ];
                                    viewMode = "LEDGER";
                                }}
                                onkeydown={(e) => {
                                    if (e.key === "Enter" || e.key === " ") {
                                        selectedPoolIDs = [
                                            pools.find(
                                                (p) => p.name === group.name,
                                            )?.id || "uncategorized",
                                        ];
                                        viewMode = "LEDGER";
                                    }
                                }}
                            >
                                <div class="flex items-center justify-between">
                                    <div class="flex items-center gap-4">
                                        <div
                                            class="w-3 h-3 rounded-full"
                                            style="background: {group.color}"
                                        ></div>
                                        <h3
                                            class="text-2xl font-black tracking-tight uppercase"
                                        >
                                            {group.name}
                                        </h3>
                                    </div>
                                    <span
                                        class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em] bg-slate-50 px-3 py-1 rounded-full"
                                        >{group.count} Events</span
                                    >
                                </div>
                                <div class="space-y-1">
                                    <p
                                        class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em]"
                                    >
                                        Allocated Volume
                                    </p>
                                    <p
                                        class="text-4xl font-black text-slate-900 tabular-nums"
                                    >
                                        €{group.total.toLocaleString("de-DE", {
                                            minimumFractionDigits: 2,
                                            maximumFractionDigits: 2,
                                        })}
                                    </p>
                                </div>
                                <div
                                    class="pt-6 border-t border-slate-50 flex items-center justify-between text-indigo-600 group-hover:translate-x-2 transition-transform"
                                >
                                    <span
                                        class="text-[10px] font-black uppercase tracking-[0.2em]"
                                        >View Details</span
                                    >
                                    <ArrowRight class="w-4 h-4" />
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}
            </div>
        {:else if viewMode === "CHAINS"}
            <div class="space-y-8" transition:fade>
                <div class="flex items-center justify-between">
                    <div>
                        <h2
                            class="text-3xl font-black text-slate-900 uppercase"
                        >
                            PSD2 Sync Nodes
                        </h2>
                        <p class="text-slate-500 font-medium">
                            Real-time status of your PSD2/Trading212 ingestion
                            chains.
                        </p>
                    </div>
                    <button
                        onclick={() => (showIntegrationWizard = true)}
                        class="btn-primary"
                    >
                        <Plus class="w-4 h-4" />
                        <span>Provision New Node</span>
                    </button>
                </div>
                <div
                    class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6"
                >
                    {#each displayIntegrations as i}
                        <div
                            class="glass-card p-8 border hover:border-indigo-200 transition-all group"
                        >
                            <div class="flex items-center justify-between mb-8">
                                <div
                                    class="w-12 h-12 bg-slate-50 rounded-[20px] flex items-center justify-center group-hover:scale-110 transition-transform"
                                >
                                    {#if i.serviceType === "TRADING212"}<Zap
                                            class="w-6 h-6 text-indigo-600"
                                        />{:else}<ShieldCheck
                                            class="w-6 h-6 text-slate-400"
                                        />{/if}
                                </div>
                                <div class="flex items-center gap-2">
                                    {#if i.status === "ERROR"}
                                        <span
                                            class="px-3 py-1 bg-rose-50 text-rose-600 text-[10px] font-black rounded-full uppercase tracking-[0.2em]"
                                            >Error</span
                                        >
                                    {:else if i.status === "LINKING"}
                                        <span
                                            class="px-3 py-1 bg-amber-50 text-amber-600 text-[10px] font-black rounded-full uppercase tracking-[0.2em]"
                                            >Linking</span
                                        >
                                    {:else}
                                        <span
                                            class="px-3 py-1 bg-emerald-50 text-emerald-600 text-[10px] font-black rounded-full uppercase tracking-[0.2em]"
                                            >Active</span
                                        >
                                    {/if}
                                </div>
                            </div>
                            <h3
                                class="text-xl font-black text-slate-900 mb-2 uppercase tracking-tight"
                            >
                                {i.integrationName || "Unnamed Node"}
                            </h3>
                            <p
                                class="text-[10px] font-black text-slate-400 uppercase tracking-[0.2em] mb-4"
                            >
                                Service: {i.serviceType}
                            </p>

                            {#if i.lastError}
                                <div
                                    class="mb-6 p-3 bg-rose-50 rounded-xl border border-rose-100"
                                >
                                    <p
                                        class="text-[10px] font-bold text-rose-700 leading-relaxed"
                                    >
                                        {i.lastError}
                                    </p>
                                </div>
                            {/if}

                            <div class="space-y-3 mb-6">
                                {#each allAccounts.filter((a) => a.integrationId?.toLowerCase() === i.integrationId?.toLowerCase() && a.enabled) as acc}
                                    <div
                                        class="flex items-center justify-between text-xs p-3 bg-slate-50/50 rounded-xl border border-slate-100"
                                    >
                                        <div
                                            class="flex flex-col gap-1 truncate mr-2"
                                        >
                                            <span
                                                class="font-bold text-slate-800 truncate"
                                                title={acc.name}
                                                >{acc.name}</span
                                            >
                                            <span
                                                class="text-[9px] font-black text-slate-400 uppercase tracking-widest"
                                            >
                                                {#if acc.iban}{acc.iban} •
                                                {/if}{formatTimeRemaining(
                                                    acc.backoffUntil,
                                                )}</span
                                            >
                                        </div>
                                        <div
                                            class="font-black text-slate-900 shrink-0 tabular-nums"
                                        >
                                            €{acc.balance.toLocaleString(
                                                "de-DE",
                                                {
                                                    minimumFractionDigits: 2,
                                                    maximumFractionDigits: 2,
                                                },
                                            )}
                                        </div>
                                    </div>
                                {/each}
                            </div>

                            <div class="flex items-center gap-3">
                                <button
                                    onclick={() => {
                                        selectedIntegration = i;
                                        showChainEditor = true;
                                    }}
                                    class="flex-1 px-4 py-3 bg-white hover:bg-slate-50 text-slate-600 border border-slate-200 font-bold rounded-xl text-[10px] uppercase tracking-[0.2em] transition-all"
                                    >Configure</button
                                >
                                <button
                                    onclick={() =>
                                        triggerManualSync(i.integrationId)}
                                    disabled={syncingMap[i.integrationId]}
                                    class="p-3 bg-indigo-50 text-indigo-600 rounded-xl hover:bg-indigo-100 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
                                    aria-label="Trigger manual sync"
                                >
                                    <RefreshCw
                                        class="w-4 h-4 {syncingMap[
                                            i.integrationId
                                        ]
                                            ? 'animate-spin'
                                            : ''}"
                                    />
                                </button>
                            </div>
                        </div>
                    {/each}
                </div>
            </div>
        {:else if viewMode === "CONFIG"}
            <div class="space-y-12" transition:fade>
                <RuleArchitect
                    bind:pools
                    bind:rules
                    {transactions}
                    onChange={() => fetchData(true)}
                />
            </div>
        {/if}
    </div>
</div>

<!-- Chain Editor Modal -->
{#if showChainEditor}
    <ChainAccountEditor
        integration={selectedIntegration}
        bind:isOpen={showChainEditor}
        onUpdated={() => fetchData(true)}
    />
{/if}

<!-- Transaction Edit Modal -->
{#if showTransactionEdit}
    <div
        class="fixed inset-0 z-[100] flex items-center justify-center p-6 bg-slate-900/40 backdrop-blur-sm"
        transition:fade
    >
        <div
            class="w-full max-w-2xl bg-white rounded-[30px] shadow-2xl relative overflow-hidden"
            transition:slide
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="p-10 space-y-10">
                <div class="flex items-center justify-between">
                    <div class="space-y-1">
                        <h2
                            class="text-2xl font-black text-slate-900 tracking-tight"
                        >
                            Modify Transaction Flow
                        </h2>
                        <p class="text-slate-500 font-medium text-sm">
                            Update account mapping and deterministic metadata.
                        </p>
                    </div>
                    <button
                        onclick={() => (showTransactionEdit = false)}
                        class="p-3 hover:bg-slate-100 rounded-2xl transition-all border border-transparent hover:border-slate-200"
                        ><X class="w-6 h-6 text-slate-400" /></button
                    >
                </div>

                <div class="grid grid-cols-1 md:grid-cols-2 gap-8">
                    <SearchableDropdown
                        label="Source Account"
                        options={accountOptions}
                        bind:value={transactionToEdit.sourceAccountId}
                        placeholder="Select source..."
                    />

                    <SearchableDropdown
                        label="Destination Account"
                        options={accountOptions}
                        bind:value={transactionToEdit.destinationAccountId}
                        placeholder="Select destination..."
                    />
                </div>

                <div class="grid grid-cols-1 md:grid-cols-2 gap-8">
                    <div class="space-y-2">
                        <label
                            for="edit-receiver-input"
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                            >Peer Name</label
                        >
                        <div
                            class="flex items-center gap-3 p-3 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all"
                        >
                            <User class="w-4 h-4 text-slate-400" />
                            <input
                                id="edit-receiver-input"
                                type="text"
                                bind:value={editReceiverInput}
                                placeholder="External Peer"
                                class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                            />
                        </div>
                    </div>

                    <div class="space-y-2">
                        <label
                            for="edit-receiver-iban-input"
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                            >Peer IBAN</label
                        >
                        <div
                            class="flex items-center gap-3 p-3 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all"
                        >
                            <CreditCard class="w-4 h-4 text-slate-400" />
                            <input
                                id="edit-receiver-iban-input"
                                type="text"
                                bind:value={editReceiverIbanInput}
                                placeholder="DE00..."
                                class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                            />
                        </div>
                    </div>
                </div>

                <div class="space-y-2">
                    <label
                        for="edit-description-input"
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                        >Reference / Description</label
                    >
                    <div
                        class="flex items-center gap-3 p-3 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all"
                    >
                        <Hash class="w-4 h-4 text-slate-400" />
                        <input
                            id="edit-description-input"
                            type="text"
                            bind:value={editDescriptionInput}
                            placeholder="..."
                            class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                        />
                    </div>
                </div>

                <div class="space-y-2">
                    <label
                        for="edit-amount-input"
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                        >Transaction Amount (€)</label
                    >
                    <div
                        class="flex items-center gap-3 p-3 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all"
                    >
                        <Euro class="w-4 h-4 text-slate-400" />
                        <input
                            id="edit-amount-input"
                            type="number"
                            step="0.01"
                            bind:value={editAmountInput}
                            placeholder="0,00"
                            class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                        />
                    </div>
                </div>

                <div class="space-y-2">
                    <label
                        for="deterministic-tags-input"
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                        >Deterministic Tags</label
                    >
                    <div
                        class="flex items-center gap-3 p-3 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all"
                    >
                        <Tags class="w-4 h-4 text-slate-400" />
                        <input
                            id="deterministic-tags-input"
                            type="text"
                            bind:value={editTagsInput}
                            placeholder="e.g. Internal, Cashback..."
                            class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                        />
                    </div>
                </div>

                <div
                    class="p-6 bg-white rounded-2xl border border-slate-100 flex items-center justify-between group hover:border-indigo-200 transition-all shadow-sm"
                >
                    <div class="space-y-1">
                        <span
                            class="block text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                            >Anchored Integration Node</span
                        >
                        <p
                            class="text-xs font-black text-slate-900 uppercase tracking-[0.1em]"
                        >
                            {integrations.find(
                                (i) =>
                                    i.integrationId ===
                                    transactionToEdit.integrationId,
                            )?.integrationName || "Unknown Integration"}
                        </p>
                    </div>
                    <div
                        class="w-10 h-10 bg-slate-50 rounded-xl flex items-center justify-center border border-slate-100 text-slate-400 group-hover:text-indigo-500 transition-colors"
                    >
                        <ShieldCheck class="w-5 h-5" />
                    </div>
                </div>

                {#if transactionToEdit.isPotentialDuplicate}
                    <div
                        class="p-6 bg-amber-50/50 rounded-2xl border border-amber-200/50 flex items-center justify-between group hover:border-amber-300 transition-all shadow-sm"
                    >
                        <div class="space-y-1 pr-4">
                            <div
                                class="flex items-center gap-1.5 text-[10px] font-black uppercase tracking-[0.2em] text-amber-600"
                            >
                                <AlertTriangle class="w-3.5 h-3.5" />
                                <span>Duplicate Detected</span>
                            </div>
                            <p class="text-xs font-medium text-slate-600">
                                An identical transaction was flagged. Declare
                                them distinct.
                            </p>
                        </div>
                        <button
                            onclick={markAsNotDuplicate}
                            class="px-4 py-2.5 bg-amber-600 hover:bg-amber-700 text-white rounded-xl font-black text-[9px] uppercase tracking-wider transition-all flex items-center gap-1.5 shadow-sm shadow-amber-200"
                        >
                            <Check class="w-3.5 h-3.5" />
                            <span>Not a Duplicate</span>
                        </button>
                    </div>
                {/if}

                {#if deniedTransactions.length > 0}
                    <div class="space-y-3">
                        <span
                            class="block text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                            >Denied Duplicate Links</span
                        >
                        <div class="space-y-2.5">
                            {#each deniedTransactions as dt (dt.id)}
                                <div
                                    class="p-4 bg-slate-50 border border-slate-200/60 rounded-2xl flex items-center justify-between group hover:border-slate-300 transition-all shadow-sm"
                                >
                                    <div class="space-y-1">
                                        <p
                                            class="text-xs font-black text-slate-800"
                                        >
                                            {dt.data
                                                ?.remittanceInformationUnstructured ||
                                                dt.data?.creditorName ||
                                                "Untitled Merchant"}
                                        </p>
                                        <div
                                            class="flex items-center gap-2 text-[10px] font-black text-slate-400 tracking-wider"
                                        >
                                            <span
                                                >{new Date(
                                                    dt.createdAt,
                                                ).toLocaleDateString()}</span
                                            >
                                            <span>•</span>
                                            <span
                                                class={dt.data
                                                    ?.transactionAmount
                                                    ?.amount < 0
                                                    ? "text-rose-500"
                                                    : "text-emerald-500"}
                                            >
                                                {dt.data?.transactionAmount
                                                    ?.amount}
                                                {dt.data?.transactionAmount
                                                    ?.currency || "EUR"}
                                            </span>
                                        </div>
                                    </div>
                                    <button
                                        onclick={() => allowDuplicate(dt.id)}
                                        class="p-2.5 bg-white hover:bg-rose-50 border border-slate-200 hover:border-rose-200 text-slate-400 hover:text-rose-600 rounded-xl transition-all shadow-sm"
                                        title="Allow duplicate matching again"
                                    >
                                        <Trash2 class="w-4 h-4" />
                                    </button>
                                </div>
                            {/each}
                        </div>
                    </div>
                {/if}

                <div class="flex gap-4">
                    <button
                        onclick={deleteTransaction}
                        class="px-6 py-4 bg-rose-50 hover:bg-rose-100 text-rose-600 border border-rose-200/50 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-2 flex-1 shadow-sm hover:shadow-rose-100"
                    >
                        <Trash2 class="w-4 h-4" />
                        <span>Delete</span>
                    </button>
                    {#if transactionToEdit.isLinkConfirmed}
                        <button
                            onclick={() =>
                                unlinkTransaction(transactionToEdit.id)}
                            class="px-6 py-4 bg-amber-50 hover:bg-amber-100 text-amber-600 border border-amber-200/50 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-2 flex-1 shadow-sm hover:shadow-amber-100"
                        >
                            <Activity class="w-4 h-4" />
                            <span>Unlink</span>
                        </button>
                    {/if}
                    <button
                        onclick={saveTransactionEdit}
                        class="btn-primary py-4 flex items-center justify-center gap-3 flex-[2] bg-indigo-600 hover:bg-indigo-700 shadow-indigo-200"
                    >
                        <Check class="w-4 h-4" />
                        <span>Apply Flow Changes</span>
                    </button>
                </div>
            </div>
        </div>
    </div>
{/if}

{#if showRuleArchitect}
    <div
        class="fixed inset-0 z-[100] flex items-center justify-center p-6 bg-slate-900/40 backdrop-blur-sm"
        transition:fade
    >
        <div
            class="bg-white w-full max-w-6xl max-h-[90vh] overflow-y-auto shadow-2xl p-10 space-y-10 rounded-[30px] relative"
            transition:slide
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="flex items-center justify-between">
                <h2 class="text-3xl font-black text-slate-900 uppercase">
                    Logic Architect
                </h2>
                <button
                    onclick={() => (showRuleArchitect = false)}
                    class="p-4 hover:bg-slate-50 rounded-2xl transition-all border border-transparent hover:border-slate-100"
                >
                    <X class="w-6 h-6 text-slate-400" />
                </button>
            </div>
            <RuleArchitect
                bind:pools
                bind:rules
                {transactions}
                onChange={() => fetchData(true)}
            />
        </div>
    </div>
{/if}

{#if showIntegrationWizard}
    <IntegrationWizard
        onComplete={() => {
            showIntegrationWizard = false;
            fetchData(true);
        }}
        onCancel={() => (showIntegrationWizard = false)}
    />
{/if}

<style>
    .custom-scrollbar::-webkit-scrollbar {
        width: 5px;
    }
    .custom-scrollbar::-webkit-scrollbar-track {
        background: #f1f5f9;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #e2e8f0;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: #cbd5e1;
    }
</style>
