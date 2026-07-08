<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        AssetListSchema,
        LoanListSchema,
        ModificationListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        ExpenseListSchema,
        AssetSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";
    const decode = (val: any) => {
        if (!val) return val;
        if (typeof val === "string") {
            return (globalThis as any)["JS" + "ON"].parse(val);
        }
        try {
            return structuredClone(val);
        } catch (e) {
            return JSON.parse(JSON.stringify(val));
        }
    };

    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Calendar,
        Euro,
        ArrowRight,
        Clock,
        Loader2,
        History,
        Archive,
        Undo2,
        Pencil,
        Copy,
        PieChart,
        CheckCircle2,
        AlertCircle,
        Check,
        Target,
        TrendingUp,
        LineChart,
        BarChart3,
        Activity,
        Waves,
        Layers,
        ShieldCheck,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";
    import SubAssetForm from "./assets/components/SubAssetForm.svelte";
    import EtfConfigForm from "./assets/components/EtfConfigForm.svelte";

    interface HistoryStitchingSegment {
        provider: string;
        lookupTicker: string;
        conversionTracker: string;
    }

    interface ETFTracker {
        tracker: string;
        historicalTracker: string;
        conversionTracker: string;
        historyProvider: string;
        percentage: number;
        ter: number;
        stitchingSegments?: HistoryStitchingSegment[];
    }

    interface AssetPenalty {
        name: string;
        triggerType: "WITHDRAWAL" | "INTEREST";
        percentage: number;
    }

    interface SubAsset {
        id: string;
        name: string;
        targetValue: string | number; // Handling frontend string input and backend number. We can cast it.
        amountPerMonth: number;
        isRemainderConsumer: boolean;
        remainderStartDate: string | null;
        dumpingLoanId: string | null;
        startDate: string;
        endDate: string | null;
        earliestDumpDate: string | null;
        expenseId: string | null;
        remainderPriority: number;
    }

    interface AssetVersion {
        id?: string;
        assetId?: string;
        type: "STATIC" | "ETF";
        targetValue: string;
        dumpingLoanId: string | null;
        stopModificationId: string | null;
        interestRate: number;
        interestInterval: "Monthly" | "Yearly";
        amountPerMonth: number;
        remainderStartDate: string | null;
        startDate: string;
        endDate: string | null;
        etfConfig: ETFTracker[];
        penalties: AssetPenalty[];
        subAssets: SubAsset[];
        useForPassiveIncome: boolean;
    }

    interface Asset {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        linkToScenarios?: string[];
        activeVersion?: AssetVersion;
    }

    interface Loan {
        id: string;
        name: string;
    }

    interface Expense {
        id: string;
        name: string;
    }

    let assets = $state<(Asset & { activeVersion: AssetVersion })[]>([]);
    let sortedAssets = $derived(
        [...assets].sort((a, b) => {
            const dateA = a.activeVersion?.startDate || "";
            const dateB = b.activeVersion?.startDate || "";
            if (dateA !== dateB) {
                return dateA.localeCompare(dateB);
            }
            return (a.name || "").localeCompare(b.name || "");
        })
    );
    let pools = $state<any[]>([]);
    let virtualAccounts = $state<any[]>([]);
    let loans = $state<Loan[]>([]);
    let expenses = $state<Expense[]>([]);
    let modifications = $state<any[]>([]);
    let isLoading = $state(true);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

    const poolOptions = $derived([
        { id: "", label: "None / Uncategorized" },
        ...(pools || []).map((p) => ({
            id: p.id,
            label: p.name,
        })),
    ]);

    const virtualAccountOptions = $derived([
        { id: "", label: "None / General" },
        ...(virtualAccounts || []).map((va) => ({
            id: va.id,
            label: va.name,
        })),
    ]);

    const virtualAccountMultiOptions = $derived(
        (virtualAccounts || []).map((va) => ({
            id: va.id,
            label: va.name,
        })),
    );

    const expenseOptions = $derived(
        (expenses || []).map((e) => ({
            id: e.id,
            label: e.name,
        })),
    );

    // Modal State
    let showAddModal = $state(false);
    let showDeleteConfirm = $state(false);
    let currentAsset = $state<Asset & { activeVersion: AssetVersion }>(createNewAsset() as any);
    let amountInput = $state("");
    let targetInput = $state("");
    let interestInput = $state("");
    let assetToDelete = $state<string | null>(null);

    function createNewAsset(): Asset & { activeVersion: AssetVersion } {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            name: "",
            poolId: null,
            accountIds: [],
            activeVersion: {
                type: "STATIC",
                targetValue: "0",
                dumpingLoanId: null,
                stopModificationId: null,
                interestRate: 0,
                interestInterval: "Yearly",
                amountPerMonth: 0,
                remainderStartDate: null,
                startDate: monthStr,
                endDate: null,
                etfConfig: [],
                penalties: [],
                subAssets: [],
                useForPassiveIncome: false,
            },
        } as any;
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [aR, lR, mR, pR, vaR, eR] = await Promise.all([
                wsCall("assets::list", null, null, [AssetListSchema]).one(),
                wsCall("loans::list", null, null, [LoanListSchema]).one(),
                wsCall("modifications::list", null, null, [
                    ModificationListSchema,
                ]).one(),
                wsCall("pools::list", null, null, [
                    TransactionPoolListSchema,
                ]).one(),
                wsCall("virtualaccounts::list", null, null, [
                    VirtualAccountListSchema,
                ]).one(),
                wsCall("expenses::list", null, null, [
                    ExpenseListSchema,
                ]).one(),
            ]);

            if (aR[1]) throw aR[1];
            if (lR[1]) throw lR[1];
            if (mR[1]) throw mR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];
            if (eR[1]) throw eR[1];

            assets = (aR[0]?.assets ?? []) as any;
            loans = lR[0]?.loans ?? [];
            modifications = mR[0]?.modifications ?? [];
            pools = pR[0]?.pools ?? [];
            virtualAccounts = vaR[0]?.virtualAccounts ?? [];
            expenses = eR[0]?.expenses ?? [];
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    function parseNumeric(val: string | number, locale: "DE" | "US"): number {
        if (typeof val === "number") return val;
        if (typeof val === "number") return val; if (!val) return 0;
        if (locale === "DE") return parseGermanAmount(val);
        let clean = val.toString().trim().replace(/,/g, "");
        return parseFloat(clean) || 0;
    }

    function formatGermanNumeric(val: number | string): string {
        const num = typeof val === "string" ? parseFloat(val) : val;
        if (isNaN(num)) return val.toString();
        return num.toLocaleString("de-DE", { useGrouping: false });
    }

    function calculateRequiredRate(
        targetVal: string,
        start: string,
        end: string | null,
        interestRate: number,
    ): number {
        const target = parseGermanAmount(targetVal);
        if (isNaN(target) || target <= 0 || !end || !start) return 0;

        const startDate = new Date(start);
        const endDate = new Date(end);

        const runtime =
            (endDate.getFullYear() - startDate.getFullYear()) * 12 +
            (endDate.getMonth() - startDate.getMonth());
        if (runtime <= 0) return target;

        const r = interestRate / 100.0 / 12.0;
        if (r > 0) {
            return (target * r) / (Math.pow(1 + r, runtime) - 1);
        }
        return target / runtime;
    }

    async function saveAsset() {
        if (!currentAsset.name) return;

        if (currentAsset.activeVersion.penalties) {
            currentAsset.activeVersion.penalties =
                currentAsset.activeVersion.penalties.map((p) => ({
                    ...p,
                    percentage: parseFloat(p.percentage as any) || 0,
                }));
        } else {
            currentAsset.activeVersion.penalties = [];
        }

        if (currentAsset.activeVersion.type === "STATIC") {
            currentAsset.activeVersion.interestRate = parseNumeric(
                interestInput,
                "DE",
            );
        } else if (currentAsset.activeVersion.type === "ETF") {
            currentAsset.activeVersion.interestRate = 0;
        }

        if (
            currentAsset.activeVersion.subAssets &&
            currentAsset.activeVersion.subAssets.length > 0
        ) {
            currentAsset.activeVersion.amountPerMonth = 0;
            currentAsset.activeVersion.targetValue = "0";
            currentAsset.activeVersion.dumpingLoanId = null;
            currentAsset.activeVersion.subAssets =
                currentAsset.activeVersion.subAssets.map((sa) => ({
                    ...sa,
                    amountPerMonth: parseFloat(sa.amountPerMonth as any) || 0,
                    targetValue: (
                        parseNumeric(sa.targetValue as string, "DE") || 0
                    ).toString(),
                    dumpingLoanId:
                        sa.dumpingLoanId && sa.dumpingLoanId.trim() !== ""
                            ? sa.dumpingLoanId
                            : null,
                    expenseId:
                        sa.expenseId && sa.expenseId.trim() !== ""
                            ? sa.expenseId
                            : null,
                }));
        } else {
            currentAsset.activeVersion.amountPerMonth = parseNumeric(
                amountInput,
                "DE",
            );
            const numTarget = parseNumeric(targetInput, "DE");
            currentAsset.activeVersion.targetValue = numTarget.toString();
            currentAsset.activeVersion.subAssets = [];
        }

        isSaving = true;
        try {
            const [, err] = await wsCall(
                "assets::save",
                AssetSchema,
                {
                    id: currentAsset.id || "",
                    name: currentAsset.name,
                    poolId: currentAsset.poolId || "",
                    accountIds: currentAsset.accountIds,
                    linkToScenarios: currentAsset.linkToScenarios,
                    activeVersion: {
                        id: currentAsset.activeVersion.id || "",
                        assetId: currentAsset.activeVersion.assetId || "",
                        type: currentAsset.activeVersion.type || "STOCKS",
                        targetValue:
                            parseFloat(
                                currentAsset.activeVersion.targetValue,
                            ) || 0,
                        dumpingLoanId:
                            currentAsset.activeVersion.dumpingLoanId || "",
                        stopModificationId:
                            currentAsset.activeVersion.stopModificationId || "",
                        interestRate:
                            parseFloat(
                                currentAsset.activeVersion.interestRate as any,
                            ) || 0,
                        interestInterval:
                            currentAsset.activeVersion.interestInterval ||
                            "YEARLY",
                        amountPerMonth:
                            parseFloat(
                                currentAsset.activeVersion.amountPerMonth as any,
                            ) || 0,
                        remainderStartDate:
                            currentAsset.activeVersion.remainderStartDate || "",
                        startDate: currentAsset.activeVersion.startDate || "",
                        endDate: currentAsset.activeVersion.endDate || "",
                        useForPassiveIncome: !!currentAsset.activeVersion.useForPassiveIncome,
                        etfConfig: (
                            currentAsset.activeVersion.etfConfig || []
                        ).map((t: any) => ({
                            tracker: t.tracker || "",
                            historicalTracker: t.historicalTracker || "",
                            conversionTracker: t.conversionTracker || "",
                            historyProvider: t.historyProvider || "",
                            percentage: parseFloat(t.percentage) || 0,
                            ter: parseFloat(t.ter) || 0,
                            stitchingSegments: (t.stitchingSegments || []).map((seg: any) => ({
                                provider: seg.provider || "",
                                lookupTicker: seg.lookupTicker || "",
                                conversionTracker: seg.conversionTracker || "",
                            })),
                        })),
                        penalties: (
                            currentAsset.activeVersion.penalties || []
                        ).map((p: any) => ({
                            name: p.name || "",
                            triggerType: p.triggerType || "",
                            percentage: parseFloat(p.percentage) || 0,
                        })),
                        subAssets: (
                            currentAsset.activeVersion.subAssets || []
                        ).map((s: any) => ({
                            id: s.id || "",
                            name: s.name || "",
                            targetValue: parseNumeric(s.targetValue, "DE") || 0,
                            amountPerMonth: parseNumeric(s.amountPerMonth, "DE") || 0,
                            isRemainderConsumer: !!s.isRemainderConsumer,
                            remainderStartDate: s.remainderStartDate || "",
                            dumpingLoanId: s.dumpingLoanId || "",
                            startDate: s.startDate || "",
                            endDate: s.endDate || "",
                            earliestDumpDate: s.earliestDumpDate || "",
                            expenseId: s.expenseId || "",
                            remainderPriority: Number(s.remainderPriority) || 0,
                        })),
                    },
                },
                [AssetSchema],
            ).one();
            if (err) throw err;

            await fetchData();
            showAddModal = false;
        } catch (err: any) {
            alert(err.message);
        } finally {
            isSaving = false;
        }
    }

    async function deleteAsset(mode?: string) {
        try {
            const [, err] = await wsCall(
                "assets::delete",
                GenericIDSchema,
                { id: assetToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            assetToDelete = null;
        } catch (err: any) {
            alert(err.message);
        }
    }

    function confirmDelete(mode?: string) {
        deleteAsset(mode);
    }

    async function duplicateAsset(asset: any) {
        if (!confirm(`Are you sure you want to duplicate "${asset.name}"?`)) {
            return;
        }

        try {
            const copiedAsset = decode(asset);

            // Generate new IDs for the asset
            copiedAsset.id = crypto.randomUUID();
            copiedAsset.name = copiedAsset.name + " (Copy)";

            // Update sub-asset IDs to be unique
            if (copiedAsset.activeVersion && copiedAsset.activeVersion.subAssets) {
                copiedAsset.activeVersion.subAssets = copiedAsset.activeVersion.subAssets.map((sa: any) => ({
                    ...sa,
                    id: crypto.randomUUID()
                }));
            }

            const av = copiedAsset.activeVersion || {};
            const [, err] = await wsCall(
                "assets::save",
                AssetSchema,
                {
                    id: copiedAsset.id,
                    name: copiedAsset.name,
                    poolId: copiedAsset.poolId || "",
                    accountIds: copiedAsset.accountIds || [],
                    linkToScenarios: copiedAsset.linkToScenarios || false,
                    activeVersion: {
                        id: "", // Let backend generate new version ID
                        assetId: copiedAsset.id,
                        type: av.type || "STOCKS",
                        targetValue: parseFloat(av.targetValue) || 0,
                        dumpingLoanId: av.dumpingLoanId || "",
                        stopModificationId: av.stopModificationId || "",
                        interestRate: parseFloat(av.interestRate) || 0,
                        interestInterval: av.interestInterval || "YEARLY",
                        amountPerMonth: parseFloat(av.amountPerMonth) || 0,
                        remainderStartDate: av.remainderStartDate || "",
                        startDate: av.startDate || "",
                        endDate: av.endDate || "",
                        useForPassiveIncome: !!av.useForPassiveIncome,
                        etfConfig: (av.etfConfig || []).map((t: any) => ({
                            tracker: t.tracker || "",
                            historicalTracker: t.historicalTracker || "",
                            conversionTracker: t.conversionTracker || "",
                            historyProvider: t.historyProvider || "",
                            percentage: parseFloat(t.percentage) || 0,
                            ter: parseFloat(t.ter) || 0,
                            stitchingSegments: (t.stitchingSegments || []).map((seg: any) => ({
                                provider: seg.provider || "",
                                lookupTicker: seg.lookupTicker || "",
                                conversionTracker: seg.conversionTracker || "",
                            })),
                        })),
                        penalties: (av.penalties || []).map((p: any) => ({
                            name: p.name || "",
                            triggerType: p.triggerType || "",
                            percentage: parseFloat(p.percentage) || 0,
                        })),
                        subAssets: (av.subAssets || []).map((s: any) => ({
                            id: s.id || "",
                            name: s.name || "",
                            targetValue: Number(s.targetValue) || 0,
                            amountPerMonth: Number(s.amountPerMonth) || 0,
                            isRemainderConsumer: !!s.isRemainderConsumer,
                            remainderStartDate: s.remainderStartDate || "",
                            dumpingLoanId: s.dumpingLoanId || "",
                            startDate: s.startDate || "",
                            endDate: s.endDate || "",
                            earliestDumpDate: s.earliestDumpDate || "",
                            expenseId: s.expenseId || "",
                            remainderPriority: Number(s.remainderPriority) || 0,
                        })),
                    },
                },
                [AssetSchema]
            ).one();

            if (err) throw err;

            await fetchData();
        } catch (err: any) {
            alert("Failed to duplicate asset: " + err.message);
        }
    }

    function editAsset(asset: Asset) {
        currentAsset = decode(asset);
        if (!currentAsset.accountIds) {
            currentAsset.accountIds = [];
        }
        if (!currentAsset.activeVersion) {
            currentAsset.activeVersion = {
                type: "STATIC",
                targetValue: "0",
                dumpingLoanId: null,
                stopModificationId: null,
                interestRate: 0,
                interestInterval: "Yearly",
                amountPerMonth: 0,
                remainderStartDate: null,
                startDate: new Date().toISOString(),
                endDate: null,
                etfConfig: [],
                penalties: [],
                subAssets: [],
                useForPassiveIncome: false,
            };
        }
        currentAsset.activeVersion.useForPassiveIncome = !!currentAsset.activeVersion.useForPassiveIncome;
        currentAsset.activeVersion.penalties =
            currentAsset.activeVersion.penalties || [];
        currentAsset.activeVersion.subAssets = (
            currentAsset.activeVersion.subAssets || []
        ).map((sa: any) => ({
            ...sa,
            targetValue: formatGermanNumeric(sa.targetValue),
            dumpingLoanId: sa.dumpingLoanId && sa.dumpingLoanId !== "" ? sa.dumpingLoanId : null,
            remainderStartDate: sa.remainderStartDate && sa.remainderStartDate !== "" ? sa.remainderStartDate : null,
            endDate: sa.endDate && sa.endDate !== "" ? sa.endDate : null,
            earliestDumpDate: sa.earliestDumpDate && sa.earliestDumpDate !== "" ? sa.earliestDumpDate : null,
            expenseId: sa.expenseId && sa.expenseId !== "" ? sa.expenseId : null,
        }));
        amountInput = formatGermanNumeric(
            currentAsset.activeVersion.amountPerMonth,
        );
        interestInput = formatGermanNumeric(
            currentAsset.activeVersion.interestRate,
        );
        targetInput = formatGermanNumeric(
            currentAsset.activeVersion.targetValue,
        );
        showAddModal = true;
    }

    function toInputMonth(isoStr: string | null): string {
        if (!isoStr) return "";
        return isoStr.substring(0, 7); // "YYYY-MM"
    }

    function fromInputMonth(val: string): string {
        if (!val) return "";
        return val + "-01T00:00:00Z";
    }

    function handleRecalculate() {
        const rate = calculateRequiredRate(
            targetInput,
            currentAsset.activeVersion.startDate,
            currentAsset.activeVersion.endDate,
            parseNumeric(interestInput, "DE"),
        );
        amountInput = formatGermanAmount(rate);
    }

    function formatDate(dateStr: string | null) {
        if (!dateStr) return "Ongoing";
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            year: "numeric",
            month: "2-digit",
        });
    }

    onMount(() => {
        fetchData();
    });
</script>

<svelte:window onkeydown={(e) => {
    if (e.key === 'Escape') {
        showAddModal = false;
        showDeleteConfirm = false;
    }
}} />

<div class="space-y-8">
    <!-- Header -->
    <div
        class="flex flex-col md:flex-row md:items-center justify-between gap-6"
    >
        <div>
            <h2
                class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500"
            >
                ETF & Assets
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Deterministic growth models, compound interest, and investment
                portfolios.
            </p>
        </div>
        <div class="flex gap-4">
            <button
                onclick={() => {
                    currentAsset = createNewAsset();
                    amountInput = "";
                    targetInput = "";
                    interestInput = "";
                    showAddModal = true;
                }}
                class="btn-primary bg-emerald-600 hover:bg-emerald-700 shadow-emerald-200"
            >
                <Plus class="w-5 h-5" />
                Add Asset
            </button>
        </div>
    </div>

    {#if error}
        <div
            transition:fade
            class="glass-card p-6 border-rose-200 bg-rose-50/50 flex items-center gap-4 text-rose-600"
        >
            <AlertCircle class="w-6 h-6 flex-shrink-0" />
            <div class="flex-1">
                <p class="text-xs font-black uppercase tracking-widest">
                    Node Engine Error
                </p>
                <p class="text-sm font-bold">{error}</p>
            </div>
            <button
                onclick={fetchData}
                class="px-4 py-2 bg-rose-600 text-white rounded-xl text-[10px] font-black uppercase tracking-widest hover:bg-rose-700 transition-colors shadow-lg shadow-rose-200"
            >
                Retry
            </button>
        </div>
    {/if}

    {#if isLoading}
        <div class="flex flex-col items-center justify-center py-20 space-y-4">
            <Loader2 class="w-10 h-10 text-emerald-600 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]"
            >
                Projecting Asset Lifecycle...
            </p>
        </div>
    {:else if assets.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div
                class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner"
            >
                <PieChart class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Assets Initialized
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Add static interest accounts or ETF portfolios to start
                    deterministic growth simulation.
                </p>
            </div>
            <button
                onclick={() => (showAddModal = true)}
                class="btn-secondary mx-auto"
            >
                Create First Asset
            </button>
        </div>
    {:else}
        <div class="glass-card overflow-hidden">
            <div class="overflow-x-auto">
                <table class="w-full border-collapse text-left">
                    <thead>
                        <tr class="border-b border-slate-100 bg-slate-50/50">
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Name</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Type</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Interest</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Monthly Rate</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Started</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Target</th>
                            <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
                        </tr>
                    </thead>
                    <tbody>
                        {#each sortedAssets as asset (asset.id)}
                            <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                                <td class="px-6 py-4">
                                    <div class="font-bold text-slate-800">{asset.name}</div>
                                    {#if asset.activeVersion.stopModificationId}
                                        <div class="text-[9px] font-black text-amber-600 uppercase tracking-wider mt-0.5">Auto-Stop</div>
                                    {/if}
                                </td>
                                <td class="px-6 py-4 text-xs font-bold text-slate-700">
                                    <span class="px-2 py-0.5 bg-emerald-50 text-emerald-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]">
                                        {asset.activeVersion.type === "ETF"
                                            ? "ETF Portfolio"
                                            : asset.activeVersion.interestInterval}
                                    </span>
                                </td>
                                <td class="px-6 py-4 text-xs font-bold text-slate-700">
                                    {asset.activeVersion.type === "ETF"
                                        ? "STOCHASTIC"
                                        : formatGermanAmount(asset.activeVersion.interestRate) + "%"}
                                </td>
                                <td class="px-6 py-4">
                                    <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900">
                                        <span>€</span>
                                        <span>
                                            {#if asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                                                {formatGermanAmount(
                                                    asset.activeVersion.subAssets.reduce((sum, sa) => sum + sa.amountPerMonth, 0)
                                                )}
                                            {:else if asset.activeVersion.amountPerMonth > 0}
                                                {formatGermanAmount(asset.activeVersion.amountPerMonth)}
                                            {:else}
                                                Remainder
                                            {/if}
                                        </span>
                                    </div>
                                </td>
                                <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatDate(asset.activeVersion.startDate)}</td>
                                <td class="px-6 py-4">
                                    <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-indigo-600">
                                        <span>€</span>
                                        <span>
                                            {#if asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                                                {formatGermanAmount(
                                                    asset.activeVersion.subAssets.reduce(
                                                        (sum, sa) => sum + (parseFloat(String(sa.targetValue)) || 0),
                                                        0
                                                    )
                                                )}
                                            {:else if asset.activeVersion.dumpingLoanId}
                                                Dumps Loan
                                            {:else}
                                                {formatGermanAmount(parseFloat(String(asset.activeVersion.targetValue)) || 0)}
                                            {/if}
                                        </span>
                                    </div>
                                </td>
                                <td class="px-6 py-4 text-right">
                                    <div class="inline-flex gap-2">
                                        <button
                                            onclick={() => duplicateAsset(asset)}
                                            class="p-2 text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 rounded-xl transition-all border border-transparent hover:border-indigo-100"
                                            title="Duplicate Asset"
                                        >
                                            <Copy class="w-4 h-4" />
                                        </button>
                                        <button
                                            onclick={() => editAsset(asset)}
                                            class="p-2 text-slate-400 hover:text-emerald-600 hover:bg-emerald-50 rounded-xl transition-all border border-transparent hover:border-emerald-100"
                                            title="Refine (New Version)"
                                        >
                                            <Pencil class="w-4 h-4" />
                                        </button>
                                        <button
                                            onclick={() => {
                                                assetToDelete = asset.id!;
                                                showDeleteConfirm = true;
                                            }}
                                            class="p-2 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-xl transition-all border border-transparent hover:border-red-100"
                                            title="Lifecycle Action"
                                        >
                                            <Trash2 class="w-4 h-4" />
                                        </button>
                                    </div>
                                </td>
                            </tr>
                        {/each}
                    </tbody>
                </table>
            </div>
        </div>
    {/if}
</div>

<!-- Add/Edit Modal -->
{#if showAddModal}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-2xl bg-white rounded-[30px] shadow-2xl relative max-h-[90vh] overflow-y-auto"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <button
                onclick={() => (showAddModal = false)}
                class="absolute top-6 right-6 text-slate-400 hover:text-slate-900 transition-colors"
                ><Plus class="w-6 h-6 rotate-45" /></button
            >

            <div class="p-10 space-y-10">
                <div>
                    <h3
                        class="text-2xl font-black text-slate-900 tracking-tight"
                    >
                        {currentAsset.id ? "Refine" : "New"} Wealth Node
                    </h3>
                    <p class="text-slate-500 font-medium text-sm">
                        Define deterministic growth parameters for this asset.
                    </p>
                </div>

                <form
                    onsubmit={(e) => {
                        e.preventDefault();
                        saveAsset();
                    }}
                    class="space-y-8"
                >
                    <div class="space-y-6">
                        <div class="grid grid-cols-2 gap-6">
                            <div class="space-y-2">
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                    >Asset Identity</label
                                >
                                <input
                                    bind:value={currentAsset.name}
                                    placeholder="e.g. Main Savings"
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold"
                                    required
                                />
                            </div>
                            <SearchableMultiSelect
                                label="Planned Account Link"
                                options={virtualAccountMultiOptions}
                                bind:values={currentAsset.accountIds}
                                placeholder="Select accounts..."
                            />
                        </div>
                        <div class="grid grid-cols-2 gap-6">
                            <SearchableDropdown
                                label="Realtime Pool Link"
                                options={poolOptions}
                                bind:value={currentAsset.poolId}
                                placeholder="None / Uncategorized"
                            />
                        </div>
                    </div>

                    <div class="grid grid-cols-1 md:grid-cols-2 gap-8">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Engine Type</label
                            >
                            <select
                                bind:value={currentAsset.activeVersion.type}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                            >
                                <option value="STATIC">Static Interest</option>
                                <option value="ETF"
                                    >ETF Portfolio (Monte Carlo)</option
                                >
                            </select>
                        </div>

                        {#if currentAsset.activeVersion.type === "ETF"}
                            <div class="p-4 bg-slate-50 dark:bg-slate-800/50 rounded-2xl flex items-center justify-between gap-4 border border-slate-100 dark:border-slate-800 self-end">
                                <div class="space-y-0.5">
                                    <span class="text-xs font-black text-slate-700 dark:text-slate-200 block">Use for Passive Income</span>
                                    <span class="text-[10px] text-slate-400 leading-normal block font-medium">
                                        Includes this asset's balance in the passive income milestone calculation.
                                    </span>
                                </div>
                                <input
                                    type="checkbox"
                                    bind:checked={currentAsset.activeVersion.useForPassiveIncome}
                                    class="w-5 h-5 accent-emerald-600 rounded-lg cursor-pointer"
                                />
                            </div>
                        {/if}
                    </div>

                    <!-- Core Asset Inputs -->
                    {#if currentAsset.activeVersion.type === "STATIC" || !currentAsset.activeVersion.subAssets || currentAsset.activeVersion.subAssets.length === 0}
                        <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
                            {#if !currentAsset.activeVersion.subAssets || currentAsset.activeVersion.subAssets.length === 0}
                                <div
                                    class="space-y-2 {currentAsset.activeVersion
                                        .type !== 'STATIC'
                                        ? 'md:col-span-3'
                                        : ''}"
                                >
                                    <div
                                        class="flex items-center justify-between"
                                    >
                                        <label
                                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                            >Monthly Rate (€)</label
                                        >
                                        <button
                                            type="button"
                                            onclick={handleRecalculate}
                                            class="text-[9px] font-black text-emerald-600 hover:underline uppercase flex items-center gap-1"
                                        >
                                            <Activity class="w-3 h-3" /> Recalculate
                                        </button>
                                    </div>
                                    <input
                                        type="text"
                                        bind:value={amountInput}
                                        placeholder="328,00"
                                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold"
                                    />
                                </div>
                            {/if}
                            {#if currentAsset.activeVersion.type === "STATIC" || currentAsset.activeVersion.type === "ETF"}
                                <div
                                    class="space-y-2 {currentAsset.activeVersion
                                        .subAssets?.length > 0
                                        ? 'md:col-span-2'
                                        : ''}"
                                >
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >{currentAsset.activeVersion.type === "ETF" ? 'Payout' : 'Interest'} %</label
                                    >
                                    <input
                                        type="text"
                                        bind:value={interestInput}
                                        placeholder="2,50"
                                        disabled={currentAsset.activeVersion.type === "ETF"}
                                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold disabled:opacity-50"
                                    />
                                </div>
                                <div class="space-y-2">
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >{currentAsset.activeVersion.type === "ETF" ? 'Mode' : 'Interval'}</label
                                    >
                                    <select
                                        bind:value={
                                            currentAsset.activeVersion
                                                .interestInterval
                                        }
                                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                                    >
                                        {#if currentAsset.activeVersion.type === "ETF"}
                                            <option value="Yearly">Accumulating</option>
                                            <option value="Monthly">Distributing (Monthly Payout)</option>
                                        {:else}
                                            <option value="Monthly">Monthly</option>
                                            <option value="Yearly">Yearly</option>
                                        {/if}
                                    </select>
                                </div>
                            {/if}
                        </div>
                    {/if}

                    {#if !currentAsset.activeVersion.subAssets || currentAsset.activeVersion.subAssets.length === 0}
                        <div class="space-y-2" transition:slide>
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Target Value (€)</label
                            >
                            <input
                                bind:value={targetInput}
                                placeholder="301000"
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold"
                            />
                        </div>

                        <!-- Termination Trigger Section -->
                        <div
                            class="space-y-4 p-6 bg-white rounded-2xl border border-slate-100 shadow-sm"
                            transition:slide
                        >
                            <div class="space-y-2">
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                    >Termination Trigger</label
                                >
                                <select
                                    bind:value={
                                        currentAsset.activeVersion
                                            .stopModificationId
                                    }
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                                >
                                    <option value={null}>None (Ongoing)</option>
                                    {#each modifications.filter((m) => m.activeVersion.withdrawalPercentage > 0) as m}
                                        <option value={m.id}
                                            >{m.description} ({m.activeVersion
                                                .withdrawalPercentage}% Dynamic)</option
                                        >
                                    {/each}
                                </select>
                                <p
                                    class="text-[9px] font-medium text-slate-500 ml-1"
                                >
                                    Automatically close this asset and stop
                                    contributions once the selected modification
                                    triggers.
                                </p>
                            </div>
                        </div>

                        <!-- Loan Dumping Section -->
                        <div
                            class="space-y-4 p-6 bg-white rounded-2xl border border-slate-100 shadow-sm"
                            transition:slide
                        >
                            <div class="flex items-center justify-between">
                                <div class="space-y-0.5">
                                    <label
                                        class="text-sm font-black text-slate-900"
                                        >Enable Loan Dumping</label
                                    >
                                    <p
                                        class="text-[10px] font-medium text-slate-500"
                                    >
                                        Automatically payoff a loan when this
                                        asset has enough funds.
                                    </p>
                                </div>
                                <button
                                    type="button"
                                    onclick={() =>
                                        (currentAsset.activeVersion.dumpingLoanId =
                                            currentAsset.activeVersion
                                                .dumpingLoanId
                                                ? null
                                                : loans[0]?.id || null)}
                                    class="w-12 h-6 rounded-full transition-all relative {currentAsset
                                        .activeVersion.dumpingLoanId
                                        ? 'bg-emerald-500'
                                        : 'bg-slate-200'}"
                                >
                                    <div
                                        class="absolute top-1 left-1 w-4 h-4 bg-white rounded-full transition-all {currentAsset
                                            .activeVersion.dumpingLoanId
                                            ? 'translate-x-6'
                                            : ''}"
                                    ></div>
                                </button>
                            </div>

                            {#if currentAsset.activeVersion.dumpingLoanId}
                                <div class="space-y-2" transition:slide>
                                    <label
                                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                        >Target Loan</label
                                    >
                                    <select
                                        bind:value={
                                            currentAsset.activeVersion
                                                .dumpingLoanId
                                        }
                                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                                    >
                                        {#each loans as loan}
                                            <option value={loan.id}
                                                >{loan.name}</option
                                            >
                                        {/each}
                                    </select>
                                </div>
                            {/if}
                        </div>
                    {/if}

                    <!-- Logical Sub-Assets / Targets Configurator -->
                    <SubAssetForm
                        bind:subAssets={currentAsset.activeVersion.subAssets}
                        loans={loans}
                        expenses={expenses}
                        interestInput={interestInput}
                        assetType={currentAsset.activeVersion.type}
                        startDate={currentAsset.activeVersion.startDate}
                        toInputMonth={toInputMonth}
                        fromInputMonth={fromInputMonth}
                        calculateRequiredRate={calculateRequiredRate}
                        parseNumeric={parseNumeric}
                    />

                    {#if currentAsset.activeVersion.type === "ETF"}
                        <EtfConfigForm bind:etfConfig={currentAsset.activeVersion.etfConfig} />
                    {/if}

                    <!-- Custom Named Penalties Section -->
                    <div
                        class="space-y-4 p-6 bg-white rounded-2xl border border-slate-100 shadow-sm"
                    >
                        <div class="flex items-center justify-between">
                            <div class="space-y-0.5">
                                <label class="text-sm font-black text-slate-900"
                                    >Custom Named Penalties</label
                                >
                                <p
                                    class="text-[10px] font-medium text-slate-500"
                                >
                                    Apply fees or taxes automatically on
                                    withdrawal or interest generation events.
                                </p>
                            </div>
                            <button
                                type="button"
                                onclick={() => {
                                    if (!currentAsset.activeVersion.penalties) {
                                        currentAsset.activeVersion.penalties =
                                            [];
                                    }
                                    currentAsset.activeVersion.penalties.push({
                                        name: "",
                                        triggerType: "WITHDRAWAL",
                                        percentage: 0,
                                    });
                                }}
                                class="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-[10px] font-black uppercase tracking-wider transition-colors shadow-sm flex items-center gap-1"
                            >
                                <Plus class="w-3 h-3" /> Add Penalty
                            </button>
                        </div>

                        {#if currentAsset.activeVersion.penalties && currentAsset.activeVersion.penalties.length > 0}
                            <div class="space-y-3 pt-2">
                                {#each currentAsset.activeVersion.penalties as penalty, i}
                                    <div
                                        class="grid grid-cols-12 gap-3 items-center"
                                        transition:slide
                                    >
                                        <div class="col-span-5">
                                            <input
                                                type="text"
                                                bind:value={penalty.name}
                                                placeholder="e.g. Capital Gains Tax"
                                                class="block w-full px-3 py-2 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all text-xs font-bold"
                                                required
                                            />
                                        </div>
                                        <div class="col-span-4">
                                            <select
                                                bind:value={penalty.triggerType}
                                                class="block w-full px-3 py-2 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all text-xs font-bold appearance-none cursor-pointer"
                                            >
                                                <option value="WITHDRAWAL"
                                                    >On Withdrawal</option
                                                >
                                                <option value="INTEREST"
                                                    >On Interest</option
                                                >
                                            </select>
                                        </div>
                                        <div class="col-span-2 relative">
                                            <input
                                                type="number"
                                                bind:value={penalty.percentage}
                                                step="0.001"
                                                placeholder="25.0"
                                                class="block w-full pl-3 pr-6 py-2 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all text-xs font-bold"
                                                required
                                            />
                                            <span
                                                class="absolute right-3 top-2 text-xs text-slate-400 font-bold"
                                                >%</span
                                            >
                                        </div>
                                        <div
                                            class="col-span-1 flex justify-center"
                                        >
                                            <button
                                                type="button"
                                                onclick={() =>
                                                    currentAsset.activeVersion.penalties.splice(
                                                        i,
                                                        1,
                                                    )}
                                                class="text-rose-400 hover:text-rose-600 transition-colors"
                                            >
                                                <Trash2 class="w-4 h-4" />
                                            </button>
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        {/if}
                    </div>

                    <div class="grid grid-cols-3 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Start Month</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentAsset.activeVersion.startDate,
                                )}
                                oninput={(e: any) =>
                                    (currentAsset.activeVersion.startDate =
                                        fromInputMonth(e.target.value))}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold"
                                required
                            />
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Remainder Start</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentAsset.activeVersion
                                        .remainderStartDate,
                                )}
                                oninput={(e: any) =>
                                    (currentAsset.activeVersion.remainderStartDate =
                                        e.target.value
                                            ? fromInputMonth(e.target.value)
                                            : null)}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold"
                            />
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Payout Month (Optional)</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentAsset.activeVersion.endDate,
                                )}
                                oninput={(e: any) =>
                                    (currentAsset.activeVersion.endDate = e
                                        .target.value
                                        ? fromInputMonth(e.target.value)
                                        : null)}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold"
                            />
                        </div>
                    </div>

                    <div class="pt-6">
                        <button
                            disabled={isSaving}
                            class="btn-primary w-full py-4 text-lg shadow-2xl shadow-emerald-100 bg-emerald-600 hover:bg-emerald-700"
                        >
                            {#if isSaving}
                                <Loader2 class="w-6 h-6 animate-spin" />
                                <span>Processing Model...</span>
                            {:else}
                                <span>Commit Wealth Version</span>
                            {/if}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    </div>
{/if}

<!-- Deletion Confirmation Modal -->
{#if showDeleteConfirm}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-md bg-white rounded-[30px] shadow-2xl space-y-8 p-10 relative overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="text-center space-y-2">
                <h3 class="text-2xl font-black text-slate-900 tracking-tight">
                    Wealth Node Lifecycle
                </h3>
                <p class="text-slate-500 font-medium text-sm">
                    Action deterministic removal or refinement.
                </p>
            </div>

            <div class="grid grid-cols-1 gap-4">
                <button
                    onclick={() => confirmDelete("revert")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-emerald-50 hover:border-emerald-100 hover:bg-emerald-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-emerald-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Undo2 class="w-6 h-6 text-emerald-600" />
                    </div>
                    <div>
                        <p class="font-black text-slate-900 leading-tight">
                            Revert Version
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Delete only the latest growth snapshot.
                        </p>
                    </div>
                </button>
                <button
                    onclick={() => confirmDelete("full")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-rose-50 hover:border-rose-100 hover:bg-rose-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-rose-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Archive class="w-6 h-6 text-rose-600" />
                    </div>
                    <div>
                        <p class="font-black text-slate-900 leading-tight">
                            Node Archive
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Hide this asset and its complete history.
                        </p>
                    </div>
                </button>
            </div>
            <button
                onclick={() => {
                    showDeleteConfirm = false;
                    assetToDelete = null;
                }}
                class="w-full py-3 text-slate-400 font-black uppercase tracking-[0.2em] text-[10px] hover:text-slate-900 transition-colors"
                >Cancel Action</button
            >
        </div>
    </div>
{/if}