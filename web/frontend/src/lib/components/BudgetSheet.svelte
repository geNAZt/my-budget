<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import { AssetListSchema, LoanListSchema, AssetSchema, LoanSchema } from "$lib/gen/api_pb.js";

    import { Euro } from "@lucide/svelte";
    import { onMount } from "svelte";

    interface Breakdown {
        incomes: Entry[];
        bills: Entry[];
        expenses: Entry[];
        assets: Entry[];
        loans: Entry[];
    }

    interface VirtualAccountBalance {
        account_id?: string;
        name: string;
        color?: string;
        starting_balance?: number;
        inflow?: number;
        outflow?: number;
        balance: number;
        asset_worth?: number;
        loan_debt?: number;
        id?: string;
        allocatedBills?: number;
        allocatedExpenses?: number;
        assetWorth?: number;
        loanDebt?: number;
        outstandingInflow?: number;
        outstandingOutflow?: number;
    }

    let {
        date,
        periodStart,
        periodEnd,
        breakdown,
        totalIncome = 0,
        totalBills = 0,
        totalExpenses = 0,
        totalAssets = 0,
        totalLoans = 0,
        remainder = 0,
        virtualAccounts = [],
    } = $props<{
        date: string;
        periodStart?: string;
        periodEnd?: string;
        breakdown: Breakdown;
        totalIncome?: number;
        totalBills?: number;
        totalExpenses?: number;
        totalAssets?: number;
        totalLoans?: number;
        remainder?: number;
        virtualAccounts?: any[];
    }>();

    function getAccountColor(id: string, index: number): string {
        const COLORS = [
            "#10b981", // Emerald
            "#6366f1", // Indigo
            "#f59e0b", // Amber
            "#ec4899", // Pink
            "#3b82f6", // Blue
            "#8b5cf6", // Purple
            "#ef4444", // Red
            "#14b8a6", // Teal
        ];
        if (!id) return COLORS[index % COLORS.length];
        let hash = 0;
        for (let i = 0; i < id.length; i++) {
            hash = id.charCodeAt(i) + ((hash << 5) - hash);
        }
        return COLORS[Math.abs(hash) % COLORS.length];
    }

    const normalizedVirtualAccounts = $derived.by(() => {
        // Find all active virtual account IDs (excluding "unassigned")
        const activeVAIDs = new Set<string>();
        for (const va of virtualAccounts || []) {
            const vaID = va.id || va.account_id || va.accountId || "";
            if (vaID && vaID !== "unassigned") {
                activeVAIDs.add(vaID);
            }
        }

        // Helper to check assignment factor
        const getAssignmentFactor = (accountIds: string[] | undefined, vaID: string) => {
            if (vaID === "unassigned") {
                if (!accountIds || accountIds.length === 0) {
                    return 1.0;
                }
                const hasAnyActive = accountIds.some(id => activeVAIDs.has(id));
                return hasAnyActive ? 0.0 : 1.0;
            }

            if (!accountIds || accountIds.length === 0) {
                return 0.0;
            }
            let hasVA = false;
            let validCount = 0;
            for (const id of accountIds) {
                if (activeVAIDs.has(id)) {
                    validCount++;
                    if (id === vaID) {
                        hasVA = true;
                    }
                }
            }
            if (hasVA && validCount > 0) {
                return 1.0 / validCount;
            }
            return 0.0;
        };

        return (virtualAccounts || []).map((va: any, index: number) => {
            const id = va.id || va.account_id || va.accountId || "";
            
            // Calculate outstanding in and outflows
            let outstandingInflow = 0;
            let outstandingOutflow = 0;

            const isOutstanding = (entry: any) => {
                return entry.realtimeBalance === undefined || entry.realtimeBalance === null || entry.realtimeBalance === 0;
            };

            const mappedEntities: { name: string; type: string; amount: number; outstanding: boolean }[] = [];

            for (const entry of breakdown.incomes || []) {
                const factor = getAssignmentFactor(entry.accountIds, id);
                if (factor > 0) {
                    if (isOutstanding(entry)) {
                        outstandingInflow += entry.amount * factor;
                    }
                    mappedEntities.push({
                        name: entry.name,
                        type: "Income",
                        amount: entry.amount * factor,
                        outstanding: isOutstanding(entry),
                    });
                }
            }

            for (const entry of breakdown.bills || []) {
                const factor = getAssignmentFactor(entry.accountIds, id);
                if (factor > 0) {
                    if (isOutstanding(entry)) {
                        outstandingOutflow += entry.amount * factor;
                    }
                    mappedEntities.push({
                        name: entry.name,
                        type: "Bill",
                        amount: entry.amount * factor,
                        outstanding: isOutstanding(entry),
                    });
                }
            }

            for (const entry of breakdown.expenses || []) {
                const factor = getAssignmentFactor(entry.accountIds, id);
                if (factor > 0) {
                    if (isOutstanding(entry)) {
                        outstandingOutflow += entry.amount * factor;
                    }
                    mappedEntities.push({
                        name: entry.name,
                        type: "Event",
                        amount: entry.amount * factor,
                        outstanding: isOutstanding(entry),
                    });
                }
            }

            for (const entry of breakdown.assets || []) {
                const factor = getAssignmentFactor(entry.accountIds, id);
                if (factor > 0) {
                    if (isOutstanding(entry)) {
                        if (entry.amount >= 0) {
                            outstandingOutflow += entry.amount * factor;
                        } else {
                            outstandingInflow += Math.abs(entry.amount) * factor;
                        }
                    }
                    mappedEntities.push({
                        name: entry.name || entry.entityName || "Asset",
                        type: "Asset",
                        amount: entry.amount * factor,
                        outstanding: isOutstanding(entry),
                    });
                }
            }

            for (const entry of breakdown.loans || []) {
                const factor = getAssignmentFactor(entry.accountIds, id);
                if (factor > 0) {
                    if (isOutstanding(entry)) {
                        outstandingOutflow += entry.amount * factor;
                    }
                    mappedEntities.push({
                        name: entry.name,
                        type: "Loan",
                        amount: entry.amount * factor,
                        outstanding: isOutstanding(entry),
                    });
                }
            }

            return {
                id,
                name: va.name || "",
                color: va.color || getAccountColor(id, index),
                balance: va.balance !== undefined ? va.balance : 0,
                inflow: va.inflow !== undefined ? va.inflow : (va.allocatedBills !== undefined ? va.allocatedBills : 0),
                outflow: va.outflow !== undefined ? va.outflow : (va.allocatedExpenses !== undefined ? va.allocatedExpenses : 0),
                asset_worth: va.asset_worth !== undefined ? va.asset_worth : (va.assetWorth !== undefined ? va.assetWorth : 0),
                loan_debt: va.loan_debt !== undefined ? va.loan_debt : (va.loanDebt !== undefined ? va.loanDebt : 0),
                outstandingInflow,
                outstandingOutflow,
                mappedEntities,
            };
        });
    });

    function formatCurrency(val: number) {
        return val.toLocaleString("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        });
    }

    function formatDate(dateStr: string) {
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            month: "long",
            year: "numeric",
        });
    }

    function formatDayMonth(dateStr: string) {
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            day: "2-digit",
            month: "2-digit",
        });
    }

    interface Entry {
        name: string;
        entityName?: string;
        amount: number;
        realtimeBalance?: number;
        penalty?: number;
        balance?: number;
        realSplit?: Record<string, number>;
        trackerFlows?: Record<string, number>;
        subAssetFlows?: Record<string, number>;
        accountIds?: string[];
    }

    const groupedAssets = $derived.by(() => {
        const groups: Record<
            string,
            {
                name: string;
                amount: number;
                penalty: number;
                balance: number;
                realSplit?: Record<string, number>;
                trackerFlows: Record<string, number>;
                subAssetFlows: Record<string, number>;
                realtimeBalance?: number;
            }
        > = {};

        for (const entry of breakdown.assets) {
            const key = entry.entityName || entry.name;
            if (!groups[key]) {
                groups[key] = {
                    name: key,
                    amount: 0,
                    penalty: 0,
                    balance: 0,
                    trackerFlows: {},
                    subAssetFlows: {},
                };
            }
            groups[key].amount += entry.amount;
            groups[key].penalty += entry.penalty || 0;
            groups[key].balance = entry.balance || groups[key].balance;
            if (
                entry.realtimeBalance !== undefined &&
                entry.realtimeBalance !== null
            ) {
                groups[key].realtimeBalance =
                    (groups[key].realtimeBalance || 0) +
                    entry.realtimeBalance;
            }
            if (entry.realSplit) {
                groups[key].realSplit = entry.realSplit;
            }
            if (entry.trackerFlows) {
                for (const [tracker, amount] of Object.entries(
                    entry.trackerFlows,
                )) {
                    groups[key].trackerFlows[tracker] =
                        (groups[key].trackerFlows[tracker] || 0) +
                        (amount as number);
                }
            }
            if (entry.subAssetFlows) {
                for (const [sa, amount] of Object.entries(
                    entry.subAssetFlows,
                )) {
                    groups[key].subAssetFlows[sa] =
                        (groups[key].subAssetFlows[sa] || 0) +
                        (amount as number);
                }
            }
        }
        return Object.values(groups);
    });
    const groupedLoans = $derived.by(() => {
        const groups: Record<
            string,
            {
                name: string;
                amount: number;
                balance: number;
                realtimeBalance?: number;
            }
        > = {};
        for (const entry of breakdown.loans) {
            const key = entry.entityName || entry.name;
            if (!groups[key]) {
                groups[key] = { name: key, amount: 0, balance: 0 };
            }
            groups[key].amount += entry.amount;
            groups[key].balance = entry.balance || groups[key].balance;
            if (
                entry.realtimeBalance !== undefined &&
                entry.realtimeBalance !== null
            ) {
                groups[key].realtimeBalance =
                    (groups[key].realtimeBalance || 0) +
                    entry.realtimeBalance;
            }
        }
        return Object.values(groups);
    });

    const _unused =
        typeof window !== "undefined"
            ? window.location.protocol === "https:"
                ? ""
                : `http://${window.location.hostname}:8080`
            : "http://localhost:8080";
    let allAssets = $state<any[]>([]);
    let allLoans = $state<any[]>([]);
    let editingGroup = $state<{
        type: "asset" | "loan";
        id: string;
        name: string;
    } | null>(null);

    onMount(async () => {
        try {
            const [assetsRes, loansRes] = await Promise.all([
                wsCall("assets::list", null, null, [AssetListSchema]).one(),
                wsCall("loans::list", null, null, [LoanListSchema]).one(),
            ]);
            const [assetsMsg, assetsErr] = assetsRes;
            const [loansMsg, loansErr] = loansRes;
            if (assetsErr) console.error(assetsErr);
            if (loansErr) console.error(loansErr);
            allAssets = assetsMsg ? assetsMsg.assets : [];
            allLoans = loansMsg ? loansMsg.loans : [];
        } catch (e) {
            console.error(e);
        }
    });

    function startEditing(entry: any, type: "asset" | "loan") {
        let entity;
        if (type === "asset") {
            entity = allAssets.find((a) => a.name === entry.name);
        } else {
            entity = allLoans.find((l) => l.name === entry.name);
        }
        if (entity) {
            editingGroup = { type, id: entity.id, name: entity.name };
        }
    }

    async function saveGroupName() {
        if (!editingGroup || !editingGroup.name.trim()) {
            editingGroup = null;
            return;
        }

        const newName = editingGroup.name.trim();
        const id = editingGroup.id;
        const type = editingGroup.type;

        try {
            if (type === "asset") {
                const asset = allAssets.find((a) => a.id === id);
                if (asset && asset.name !== newName) {
                    try {
                        await wsCall("assets::save", AssetSchema, {
                            id: asset.id,
                            name: newName,
                            poolId: asset.poolId,
                            accountIds: asset.accountIds,
                            linkToScenarios: asset.linkToScenarios,
                            activeVersion: asset.activeVersion,
                        }, [AssetSchema]).one();
                        asset.name = newName;
                        window.location.reload();
                    } catch (e) {}
                }
            } else if (type === "loan") {
                const loan = allLoans.find((l) => l.id === id);
                if (loan && loan.name !== newName) {
                    try {
                        await wsCall("loans::save", LoanSchema, {
                            id: loan.id,
                            name: newName,
                            poolId: loan.poolId,
                            accountIds: loan.accountIds,
                            linkToScenarios: loan.linkToScenarios,
                            activeVersion: loan.activeVersion,
                        }, [LoanSchema]).one();
                        loan.name = newName;
                        window.location.reload();
                    } catch (e) {}
                }
            }
        } catch (e) {
            console.error(e);
        }

        editingGroup = null;
    }

    const assetMap = $derived.by(() => {
        const map: Record<string, any> = {};
        for (const a of allAssets) {
            map[a.name] = a;
        }
        return map;
    });
</script>

<div class="space-y-5 animate-in fade-in duration-500">
    <!-- Summary Header -->
    <div class="mb-4 space-y-2.5">
        <div>
            <h3
                class="text-2xl font-black text-slate-900 tracking-tight mb-1"
            >
                {formatDate(date)}
            </h3>
            {#if periodStart && periodEnd}
                <p
                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 mb-1"
                >
                    {formatDayMonth(periodStart)} - {formatDayMonth(periodEnd)}
                </p>
            {/if}
        </div>
        <div
            class="flex flex-wrap items-center gap-2.5 text-[10px] font-medium text-slate-500"
        >
            <span class="flex items-center gap-1.5"
                ><span class="text-emerald-600 font-black">+</span> Incomes
                <span class="font-black text-slate-900"
                    >€ {formatCurrency(totalIncome)}</span
                ></span
            >
            <span class="w-1 h-1 rounded-full bg-slate-300"></span>
            <span class="flex items-center gap-1.5"
                ><span class="text-rose-500 font-black">-</span> Bills
                <span class="font-black text-slate-900"
                    >€ {formatCurrency(totalBills)}</span
                ></span
            >
            <span class="w-1 h-1 rounded-full bg-slate-300"></span>
            <span class="flex items-center gap-1.5"
                ><span class="text-rose-500 font-black">-</span> Events
                <span class="font-black text-slate-900"
                    >€ {formatCurrency(totalExpenses)}</span
                ></span
            >
            <span class="w-1 h-1 rounded-full bg-slate-300"></span>
            <span class="flex items-center gap-1.5"
                ><span class="text-rose-500 font-black">-</span> Loans
                <span class="font-black text-slate-900"
                    >€ {formatCurrency(totalLoans)}</span
                ></span
            >
            <span class="w-1 h-1 rounded-full bg-slate-300"></span>
            <span class="flex items-center gap-1.5">
                <span
                    class="{totalAssets < 0
                        ? 'text-emerald-600'
                        : 'text-rose-500'} font-black"
                    >{totalAssets < 0 ? "+" : "-"}</span
                >
                Assets
                <span class="font-black text-slate-900"
                    >€ {formatCurrency(Math.abs(totalAssets))}</span
                >
            </span>
            <span class="w-1 h-1 rounded-full bg-slate-300"></span>
            <span class="flex items-center gap-1.5"
                ><span
                    class="font-bold uppercase tracking-[0.2em] text-[9px] text-slate-400"
                    >Net Remainder</span
                >
                <span
                    class="font-black {remainder >= 0
                        ? 'text-emerald-600'
                        : 'text-rose-600'} text-xs"
                    >€ {formatCurrency(remainder)}</span
                ></span
            >
        </div>

        <!-- Additionally see the virtual account balances of the month -->
        {#if normalizedVirtualAccounts && normalizedVirtualAccounts.length > 0}
            <div
                class="flex flex-wrap items-center gap-2 pt-2 border-t border-slate-100/80 dark:border-slate-800/40"
            >
                <span
                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 dark:text-slate-500 mr-2"
                    >Virtual Accounts:</span
                >
                {#each normalizedVirtualAccounts as va}
                    <div
                        class="relative group flex items-center gap-2 bg-slate-50 dark:bg-slate-900 border border-slate-100 dark:border-slate-800 pl-2.5 pr-3 py-1 rounded-full text-[10px] font-bold text-slate-700 dark:text-slate-300 shadow-sm cursor-help hover:bg-slate-100 dark:hover:bg-slate-800 transition-all duration-150"
                        style="border-left: 3px solid {va.color}"
                    >
                        <span
                            class="truncate max-w-[120px] font-black text-slate-900 dark:text-slate-100"
                            >{va.name}</span
                        >
                        <span class="tabular-nums font-black text-slate-900 dark:text-slate-100"
                            >€ {formatCurrency(va.balance)}</span
                        >

                        <!-- Premium Tooltip Popover -->
                        <div
                            class="invisible group-hover:visible opacity-0 group-hover:opacity-100 transition-all duration-200 absolute left-0 top-full mt-2 w-80 bg-white/95 dark:bg-slate-950/95 backdrop-blur-md border border-slate-100 dark:border-slate-800/60 rounded-2xl shadow-xl z-[150] p-4 text-left pointer-events-none"
                        >
                            <!-- Top accent color bar -->
                            <div
                                class="absolute top-0 left-0 right-0 h-1.5 rounded-t-2xl"
                                style="background-color: {va.color}"
                            ></div>
                            
                            <div class="space-y-3.5 pt-1.5">
                                <div class="flex items-center justify-between">
                                    <span
                                        class="text-xs font-black tracking-tight text-slate-900 dark:text-slate-100 break-all"
                                        >{va.name}</span
                                    >
                                    <span
                                        class="text-[8px] font-black uppercase px-2 py-0.5 rounded-full text-slate-500 dark:text-slate-400 bg-slate-100 dark:bg-slate-800"
                                        style="border-left: 3px solid {va.color}"
                                    >
                                        Planned
                                    </span>
                                </div>
                                <div class="flex justify-between items-baseline">
                                    <span
                                        class="text-[9px] font-black uppercase tracking-wider text-slate-400 dark:text-slate-500"
                                        >Balance:</span
                                    >
                                    <span
                                        class="text-base font-black text-slate-900 dark:text-slate-100 tabular-nums"
                                        >€ {formatCurrency(va.balance)}</span
                                    >
                                </div>
                                <div
                                    class="grid grid-cols-2 gap-2 text-[9px] font-bold text-slate-500 dark:text-slate-400 border-t border-slate-100 dark:border-slate-800/80 pt-2.5"
                                >
                                    <div class="flex flex-col">
                                        <span
                                            class="text-slate-400 dark:text-slate-500 font-black uppercase text-[8px] tracking-wider"
                                            >Inflow</span
                                        >
                                        <span
                                            class="text-emerald-600 dark:text-emerald-400 font-black tabular-nums"
                                        >
                                            +€ {formatCurrency(va.inflow)}
                                            {#if va.outstandingInflow > 0}
                                                <span class="text-[9px] font-bold text-slate-400 dark:text-slate-500">
                                                    (+€ {formatCurrency(va.outstandingInflow)})
                                                </span>
                                            {/if}
                                        </span>
                                    </div>
                                    <div class="flex flex-col text-right">
                                        <span
                                            class="text-slate-400 dark:text-slate-500 font-black uppercase text-[8px] tracking-wider"
                                            >Outflow</span
                                        >
                                        <span
                                            class="text-rose-500 dark:text-rose-400 font-black tabular-nums"
                                        >
                                            -€ {formatCurrency(va.outflow)}
                                            {#if va.outstandingOutflow > 0}
                                                <span class="text-[9px] font-bold text-slate-400 dark:text-slate-500">
                                                    (-€ {formatCurrency(va.outstandingOutflow)})
                                                </span>
                                            {/if}
                                        </span>
                                    </div>
                                </div>
                                {#if va.asset_worth > 0 || va.loan_debt > 0}
                                    <div
                                        class="grid grid-cols-2 gap-2 text-[9px] font-bold text-slate-500 dark:text-slate-400 border-t border-slate-100 dark:border-slate-800/50 pt-2"
                                    >
                                        {#if va.asset_worth > 0}
                                            <div class="flex flex-col">
                                                <span
                                                    class="text-slate-400 dark:text-slate-500 font-black uppercase text-[8px] tracking-wider"
                                                    >Assets</span
                                                >
                                                <span
                                                    class="text-indigo-600 dark:text-indigo-400 font-black tabular-nums"
                                                    >€ {formatCurrency(
                                                        va.asset_worth,
                                                    )}</span
                                                >
                                            </div>
                                        {/if}
                                        {#if va.loan_debt > 0}
                                            <div class="flex flex-col text-right">
                                                <span
                                                    class="text-slate-400 dark:text-slate-500 font-black uppercase text-[8px] tracking-wider"
                                                    >Debt</span
                                                >
                                                <span
                                                    class="text-rose-600 dark:text-rose-400 font-black tabular-nums"
                                                    >€ {formatCurrency(
                                                        va.loan_debt,
                                                    )}</span
                                                >
                                            </div>
                                        {/if}
                                    </div>
                                {/if}

                                <!-- Booked / Mapped Entities List -->
                                {#if va.mappedEntities && va.mappedEntities.length > 0}
                                    <div class="border-t border-slate-100 dark:border-slate-800/80 pt-2.5 space-y-2">
                                        <span class="text-[8px] font-black uppercase tracking-[0.15em] text-slate-400 dark:text-slate-500 block">
                                            Mapped Items ({va.mappedEntities.length})
                                        </span>
                                        <div class="max-h-36 overflow-y-auto space-y-1.5 pr-1 scrollbar-thin scrollbar-thumb-slate-200 dark:scrollbar-thumb-slate-800 scrollbar-track-transparent">
                                            {#each va.mappedEntities as entity}
                                                <div class="flex items-center justify-between gap-2 text-[10px] py-0.5">
                                                    <div class="flex flex-col min-w-0">
                                                        <span class="font-bold text-slate-700 dark:text-slate-200 truncate max-w-[160px]" title={entity.name}>
                                                            {entity.name}
                                                        </span>
                                                        <span class="text-[8px] font-black uppercase text-slate-400 dark:text-slate-500 tracking-wider">
                                                            {entity.type} {#if entity.outstanding}• <span class="text-slate-400/80 dark:text-slate-500/80 font-bold lowercase">outstanding</span>{/if}
                                                        </span>
                                                    </div>
                                                    <span class="font-extrabold tabular-nums whitespace-nowrap {entity.type === 'Income' ? 'text-emerald-600 dark:text-emerald-400' : (entity.type === 'Bill' || entity.type === 'Event' || entity.type === 'Loan' ? 'text-rose-600 dark:text-rose-400' : 'text-slate-700 dark:text-slate-350')}">
                                                        {entity.type === 'Income' ? '+' : (entity.type === 'Bill' || entity.type === 'Event' || entity.type === 'Loan' ? '-' : '')}€ {formatCurrency(Math.abs(entity.amount))}
                                                    </span>
                                                </div>
                                            {/each}
                                        </div>
                                    </div>
                                {/if}
                            </div>
                        </div>
                    </div>
                {/each}
            </div>
        {/if}
    </div>


    <!-- Main Multi-Column Sheet -->
    <div class="grid grid-cols-1 lg:grid-cols-12 gap-4 items-start">
        <!-- Left Side: Incomes & Bills -->
        <div class="lg:col-span-4 space-y-4">
            <!-- Incomes -->
            <section class="glass-card overflow-hidden">
                <div
                    class="px-4 py-2 bg-slate-50 dark:bg-slate-900/40 border-b border-slate-100 dark:border-slate-800/40 flex justify-between items-center"
                >
                    <h4
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-900"
                    >
                        Income Sources
                    </h4>
                    <span class="text-[10px] font-black text-emerald-600"
                        >€ {formatCurrency(totalIncome)}</span
                    >
                </div>
                <div class="p-0">
                    <table class="w-full text-left border-collapse table-fixed">
                        <colgroup>
                            <col class="w-auto" />
                            <col class="w-36" />
                        </colgroup>
                        <tbody class="divide-y divide-slate-50">
                            {#each breakdown.incomes as entry}
                                <tr
                                    class="hover:bg-slate-50/50 transition-colors"
                                >
                                    <td
                                        class="px-4 py-1.5 text-xs font-bold text-slate-700 dark:text-slate-300 align-top break-words pr-2"
                                        >{entry.name}</td
                                    >
                                    <td
                                        class="px-4 py-1.5 text-xs align-top whitespace-nowrap"
                                    >
                                        <div
                                            class="flex items-start justify-between gap-2 font-black text-slate-900"
                                        >
                                            <span class="text-slate-400">€</span
                                            >
                                            <span
                                                >{formatCurrency(
                                                    entry.amount,
                                                )}</span
                                            >
                                        </div>
                                        {#if entry.realtimeBalance !== undefined && entry.realtimeBalance !== null}
                                            <div
                                                class="text-[10px] text-slate-500 mt-1 flex items-center justify-between font-semibold border-t border-slate-100/50 pt-1"
                                            >
                                                <span>Real:</span>
                                                <span class="text-slate-700"
                                                    >€ {formatCurrency(
                                                        entry.realtimeBalance,
                                                    )}</span
                                                >
                                            </div>
                                        {/if}
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </section>

            <!-- Bills -->
            <section class="glass-card overflow-hidden">
                <div
                    class="px-4 py-2 bg-slate-50 dark:bg-slate-900/40 border-b border-slate-100 dark:border-slate-800/40 flex justify-between items-center"
                >
                    <h4
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-900"
                    >
                        Liability: Bills
                    </h4>
                    <span class="text-[10px] font-black text-rose-600"
                        >€ {formatCurrency(totalBills)}</span
                    >
                </div>
                <div class="p-0 max-h-[400px] overflow-y-auto">
                    <table class="w-full text-left border-collapse table-fixed">
                        <colgroup>
                            <col class="w-auto" />
                            <col class="w-36" />
                        </colgroup>
                        <tbody class="divide-y divide-slate-50">
                            {#each breakdown.bills as entry}
                                <tr
                                    class="hover:bg-slate-50/50 transition-colors"
                                >
                                    <td
                                        class="px-4 py-1.5 text-xs font-bold text-slate-700 dark:text-slate-300 align-top break-words pr-2"
                                        >{entry.name}</td
                                    >
                                    <td
                                        class="px-4 py-1.5 text-xs align-top whitespace-nowrap"
                                    >
                                        <div
                                            class="flex items-start justify-between gap-2 font-black text-slate-900"
                                        >
                                            <span class="text-slate-400">€</span
                                            >
                                            <span
                                                >{formatCurrency(
                                                    entry.amount,
                                                )}</span
                                            >
                                        </div>
                                        {#if entry.realtimeBalance !== undefined && entry.realtimeBalance !== null}
                                            <div
                                                class="text-[10px] text-slate-500 mt-1 flex items-center justify-between font-semibold border-t border-slate-100/50 pt-1"
                                            >
                                                <span>Real:</span>
                                                <span class="text-slate-700"
                                                    >€ {formatCurrency(
                                                        entry.realtimeBalance,
                                                    )}</span
                                                >
                                            </div>
                                        {/if}
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </section>
        </div>

        <!-- Middle: Expenses & Assets -->
        <div class="lg:col-span-4 space-y-4">
            <!-- Expenses -->
            <section class="glass-card overflow-hidden">
                <div
                    class="px-4 py-2 bg-slate-50 dark:bg-slate-900/40 border-b border-slate-100 dark:border-slate-800/40 flex justify-between items-center"
                >
                    <h4
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-900"
                    >
                        One-Time Events
                    </h4>
                    <span class="text-[10px] font-black text-rose-600"
                        >€ {formatCurrency(totalExpenses)}</span
                    >
                </div>
                <div class="p-0">
                    <table class="w-full text-left border-collapse table-fixed">
                        <colgroup>
                            <col class="w-auto" />
                            <col class="w-36" />
                        </colgroup>
                        <tbody class="divide-y divide-slate-50">
                            {#each breakdown.expenses as entry}
                                <tr
                                    class="hover:bg-slate-50/50 transition-colors"
                                >
                                    <td
                                        class="px-4 py-1.5 text-xs font-bold text-slate-700 dark:text-slate-300 align-top break-words pr-2"
                                        >{entry.name}</td
                                    >
                                    <td
                                        class="px-4 py-1.5 text-xs align-top whitespace-nowrap"
                                    >
                                        <div
                                            class="flex items-start justify-between gap-2 font-black text-slate-900"
                                        >
                                            <span class="text-slate-400">€</span
                                            >
                                            <span
                                                >{formatCurrency(
                                                    entry.amount,
                                                )}</span
                                            >
                                        </div>
                                        {#if entry.realtimeBalance !== undefined && entry.realtimeBalance !== null}
                                            <div
                                                class="text-[10px] text-slate-500 mt-1 flex items-center justify-between font-semibold border-t border-slate-100/50 pt-1"
                                            >
                                                <span>Real:</span>
                                                <span class="text-slate-700"
                                                    >€ {formatCurrency(
                                                        entry.realtimeBalance,
                                                    )}</span
                                                >
                                            </div>
                                        {/if}
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </section>

            <!-- Assets -->
            <section class="glass-card overflow-hidden border-emerald-100/50">
                <div
                    class="px-4 py-2 bg-emerald-50/30 dark:bg-emerald-950/20 border-b border-emerald-100/50 dark:border-emerald-900/30 flex justify-between items-center"
                >
                    <h4
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-emerald-900 dark:text-emerald-300"
                    >
                        Wealth Nodes
                    </h4>
                    <span class="text-[10px] font-black text-emerald-700 dark:text-emerald-400"
                        >Inflow: € {formatCurrency(totalAssets)}</span
                    >
                </div>
                <div class="p-0">
                    <table class="w-full text-left border-collapse table-fixed">
                        <colgroup>
                            <col class="w-auto" />
                            <col class="w-36" />
                        </colgroup>
                        <tbody class="divide-y divide-slate-50">
                            {#each groupedAssets as entry}
                                <tr
                                    class="hover:bg-emerald-50/20 transition-colors"
                                >
                                    <td
                                        class="px-4 py-1.5 text-xs font-bold text-slate-700 dark:text-slate-300 align-top break-words pr-2"
                                    >
                                        {#if editingGroup && editingGroup.type === "asset" && editingGroup.id === allAssets.find((a) => a.name === entry.name)?.id}
                                            <input
                                                type="text"
                                                bind:value={editingGroup.name}
                                                class="px-2 py-1 bg-white border border-emerald-500 focus:ring-2 focus:ring-emerald-500/20 rounded-md text-xs font-bold text-slate-800 outline-none w-full shadow-sm mb-2"
                                                onkeydown={(e) => {
                                                    if (e.key === "Enter")
                                                        saveGroupName();
                                                    if (e.key === "Escape")
                                                        editingGroup = null;
                                                }}
                                                onblur={saveGroupName}
                                                autofocus
                                            />
                                        {:else}
                                            <div
                                                class="flex items-center gap-2 group/title cursor-pointer mb-2"
                                                onclick={() =>
                                                    startEditing(
                                                        entry,
                                                        "asset",
                                                    )}
                                            >
                                                <span
                                                    class="hover:text-emerald-600 transition-colors"
                                                    >{entry.name}</span
                                                >
                                                <span
                                                    class="opacity-0 group-hover/title:opacity-100 text-slate-400 hover:text-emerald-600 transition-all"
                                                >
                                                    <svg
                                                        class="w-3.5 h-3.5 inline-block align-text-top"
                                                        fill="none"
                                                        viewBox="0 0 24 24"
                                                        stroke="currentColor"
                                                        stroke-width="2"
                                                    >
                                                        <path
                                                            stroke-linecap="round"
                                                            stroke-linejoin="round"
                                                            d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"
                                                        />
                                                    </svg>
                                                </span>
                                            </div>
                                        {/if}
                                          {#if entry.subAssetFlows && Object.keys(entry.subAssetFlows).length > 0}
                                            <div class="mt-2 space-y-1">
                                                <p
                                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 border-b border-slate-100 pb-0.5 mb-1"
                                                >
                                                    Target Inflows
                                                </p>
                                                {#each Object.entries(entry.subAssetFlows) as [sa, amt]}
                                                    <div
                                                        class="flex items-start text-[10px] gap-3"
                                                    >
                                                        <span
                                                            class="font-bold text-slate-500 flex-1"
                                                            >{sa}</span
                                                        >
                                                        <div
                                                            class="flex justify-between w-24 font-black text-indigo-600"
                                                        >
                                                            <span
                                                                class="text-slate-400 font-bold"
                                                                >+ €</span
                                                            >
                                                            <span
                                                                >{formatCurrency(
                                                                    amt,
                                                                )}</span
                                                            >
                                                        </div>
                                                    </div>
                                                {/each}
                                            </div>
                                        {/if}

                                        {#if assetMap[entry.name]?.activeVersion?.type === "ETF" && assetMap[entry.name]?.activeVersion?.etfConfig?.length > 0}
                                            <span
                                                class="text-slate-400 font-medium text-[10px] mt-0.5 block leading-normal"
                                            >
                                                {#if entry.realSplit && Object.keys(entry.realSplit).length > 0}
                                                    {Object.entries(
                                                        entry.realSplit,
                                                    )
                                                        .map(
                                                            ([
                                                                tracker,
                                                                fraction,
                                                            ]) => {
                                                                const flow =
                                                                    entry
                                                                        .trackerFlows?.[
                                                                        tracker
                                                                    ];
                                                                const flowText =
                                                                    flow !==
                                                                        undefined &&
                                                                    flow !== 0
                                                                        ? ` (${flow > 0 ? "+" : ""}€ ${formatCurrency(flow)})`
                                                                        : "";
                                                                return `${tracker}: ${(fraction * 100).toFixed(0)}%${flowText}`;
                                                            },
                                                        )
                                                        .join(", ")}
                                                {:else}
                                                    {assetMap[
                                                        entry.name
                                                    ].activeVersion.etfConfig
                                                        .map(
                                                            (t: any) =>
                                                                `${t.tracker}: ${(t.percentage * 100).toFixed(0)}%`,
                                                        )
                                                        .join(", ")}
                                                {/if}
                                            </span>
                                        {/if}
                                    </td>
                                    <td
                                        class="px-4 py-1.5 align-top whitespace-nowrap"
                                    >
                                        <div
                                            class="flex items-start justify-between gap-2 text-xs font-black text-slate-900"
                                        >
                                            <span class="text-slate-400">€</span>
                                            <span
                                                >{formatCurrency(
                                                    entry.amount,
                                                )}</span
                                            >
                                        </div>
                                        {#if entry.realtimeBalance !== undefined && entry.realtimeBalance !== null}
                                            <div
                                                class="text-[10px] text-slate-500 mt-1 flex items-center justify-between font-semibold border-t border-slate-100/50 pt-1"
                                            >
                                                <span>Real:</span>
                                                <span class="text-slate-700"
                                                    >€ {formatCurrency(
                                                        entry.realtimeBalance,
                                                    )}</span
                                                >
                                            </div>
                                        {/if}
                                        {#if entry.penalty > 0}
                                            <div
                                                class="flex items-start justify-between gap-2 text-[10px] font-bold text-rose-500 mt-0.5 leading-tight"
                                            >
                                                <span
                                                    class="text-slate-400 opacity-0"
                                                    >€</span
                                                >
                                                <span
                                                    >-{formatCurrency(
                                                        entry.penalty,
                                                    )}€</span
                                                >
                                            </div>
                                        {/if}
                                        <p
                                            class="text-[9px] font-bold text-emerald-600 mt-1 text-right"
                                        >
                                            Node: € {formatCurrency(
                                                entry.balance || 0,
                                            )}
                                        </p>
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </section>
        </div>

        <!-- Right: Loans -->
        <div class="lg:col-span-4">
            <section class="glass-card overflow-hidden border-rose-100/50">
                <div
                    class="px-4 py-2 bg-rose-50/30 dark:bg-rose-950/20 border-b border-rose-100/50 dark:border-rose-900/30 flex justify-between items-center"
                >
                    <h4
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-rose-900 dark:text-rose-300"
                    >
                        Loan Amortization
                    </h4>
                    <span class="text-[10px] font-black text-rose-700 dark:text-rose-400"
                        >€ {formatCurrency(totalLoans)}</span
                    >
                </div>
                <div class="p-0">
                    <table class="w-full text-left border-collapse table-fixed">
                        <colgroup>
                            <col class="w-auto" />
                            <col class="w-36" />
                        </colgroup>
                        <tbody class="divide-y divide-slate-50">
                            {#each groupedLoans as entry}
                                <tr
                                    class="hover:bg-rose-50/20 transition-colors"
                                >
                                    <td
                                        class="px-4 py-1.5 text-xs font-bold text-slate-700 dark:text-slate-300 align-top break-words pr-2"
                                    >
                                        {#if editingGroup && editingGroup.type === "loan" && editingGroup.id === allLoans.find((l) => l.name === entry.name)?.id}
                                            <input
                                                type="text"
                                                bind:value={editingGroup.name}
                                                class="px-2 py-1 bg-white border border-rose-500 focus:ring-2 focus:ring-rose-500/20 rounded-md text-xs font-bold text-slate-800 outline-none w-full shadow-sm"
                                                onkeydown={(e) => {
                                                    if (e.key === "Enter")
                                                        saveGroupName();
                                                    if (e.key === "Escape")
                                                        editingGroup = null;
                                                }}
                                                onblur={saveGroupName}
                                                autofocus
                                            />
                                        {:else}
                                            <div
                                                class="flex items-center gap-2 group/title cursor-pointer"
                                                onclick={() =>
                                                    startEditing(entry, "loan")}
                                            >
                                                <span
                                                    class="hover:text-rose-600 transition-colors"
                                                    >{entry.name}</span
                                                >
                                                <span
                                                    class="opacity-0 group-hover/title:opacity-100 text-slate-400 hover:text-rose-600 transition-all"
                                                >
                                                    <svg
                                                        class="w-3.5 h-3.5 inline-block align-text-top"
                                                        fill="none"
                                                        viewBox="0 0 24 24"
                                                        stroke="currentColor"
                                                        stroke-width="2"
                                                    >
                                                        <path
                                                            stroke-linecap="round"
                                                            stroke-linejoin="round"
                                                            d="M15.232 5.232l3.536 3.536m-2.036-5.036a2.5 2.5 0 113.536 3.536L6.5 21.036H3v-3.572L16.732 3.732z"
                                                        />
                                                    </svg>
                                                </span>
                                            </div>
                                        {/if}
                                    </td>
                                    <td
                                        class="px-4 py-1.5 align-top whitespace-nowrap"
                                    >
                                        <div
                                            class="flex items-start justify-between gap-2 text-xs font-black text-slate-900"
                                        >
                                            <span class="text-slate-400">€</span
                                            >
                                            <span
                                                >{formatCurrency(
                                                    entry.amount,
                                                )}</span
                                            >
                                        </div>
                                        {#if entry.realtimeBalance !== undefined && entry.realtimeBalance !== null}
                                            <div
                                                class="text-[10px] text-slate-500 mt-1 flex items-center justify-between font-semibold border-t border-slate-100/50 pt-1"
                                            >
                                                <span>Real:</span>
                                                <span class="text-slate-700"
                                                    >€ {formatCurrency(
                                                        entry.realtimeBalance,
                                                    )}</span
                                                >
                                            </div>
                                        {/if}
                                        <p
                                            class="text-[9px] font-bold text-rose-600 mt-1 text-right"
                                        >
                                            Principal: € {formatCurrency(
                                                entry.balance || 0,
                                            )}
                                        </p>
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </section>
        </div>
    </div>
</div>
