<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        AssetListSchema,
        LoanListSchema,
        ModificationListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
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

    let assets = $state<(Asset & { activeVersion: AssetVersion })[]>([]);
    let pools = $state<any[]>([]);
    let virtualAccounts = $state<any[]>([]);
    let loans = $state<Loan[]>([]);
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
            },
        } as any;
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [aR, lR, mR, pR, vaR] = await Promise.all([
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
            ]);

            if (aR[1]) throw aR[1];
            if (lR[1]) throw lR[1];
            if (mR[1]) throw mR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];

            assets = (aR[0]?.assets ?? []) as any;
            loans = lR[0]?.loans ?? [];
            modifications = mR[0]?.modifications ?? [];
            pools = pR[0]?.pools ?? [];
            virtualAccounts = vaR[0]?.virtualAccounts ?? [];
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
            };
        }
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
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {#each assets as asset (asset.id)}
                <div
                    transition:fade
                    class="glass-card p-8 group hover:border-emerald-200/50 transition-all duration-300 relative overflow-hidden"
                >
                    <div
                        class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-emerald-500/0 via-emerald-500/20 to-emerald-500/0 opacity-0 group-hover:opacity-100 transition-opacity"
                    ></div>

                    <div class="flex justify-between items-start mb-6">
                        <div class="space-y-1">
                            <h3
                                class="text-xl font-black tracking-tight text-slate-900"
                            >
                                {asset.name}
                            </h3>
                            <div class="flex items-center gap-2">
                                <span
                                    class="px-2 py-0.5 bg-emerald-50 text-emerald-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]"
                                >
                                    {asset.activeVersion.type === "ETF"
                                        ? "ETF Portfolio"
                                        : asset.activeVersion.interestInterval}
                                </span>
                                <span
                                    class="px-2 py-0.5 bg-slate-100 text-slate-400 rounded-md text-[9px] font-black uppercase tracking-[0.2em] flex items-center gap-1"
                                >
                                    <History class="w-2.5 h-2.5" /> Latest
                                </span>
                                {#if asset.activeVersion.stopModificationId}
                                    {@const mod = modifications.find(
                                        (m) =>
                                            m.id ===
                                            asset.activeVersion
                                                .stopModificationId,
                                    )}
                                    <span
                                        class="px-2 py-0.5 bg-amber-50 text-amber-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em] flex items-center gap-1"
                                        title="Closes when {mod?.description ||
                                            'Modification'} triggers"
                                    >
                                        <Layers class="w-2.5 h-2.5" /> Auto-Stop
                                    </span>
                                {/if}
                            </div>
                        </div>
                        <div class="flex gap-2">
                            <button
                                onclick={() => editAsset(asset)}
                                class="p-2.5 text-slate-400 hover:text-emerald-600 hover:bg-emerald-50 rounded-xl transition-all border border-transparent hover:border-emerald-100"
                                title="Refine (New Version)"
                            >
                                <Pencil class="w-4 h-4" />
                            </button>
                            <button
                                onclick={() => {
                                    assetToDelete = asset.id!;
                                    showDeleteConfirm = true;
                                }}
                                class="p-2.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-xl transition-all border border-transparent hover:border-red-100"
                                title="Lifecycle Action"
                            >
                                <Trash2 class="w-4 h-4" />
                            </button>
                        </div>
                    </div>

                    <div class="space-y-6">
                        <div class="grid grid-cols-2 gap-4">
                            <div>
                                <p
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 mb-1"
                                >
                                    Monthly Rate
                                </p>
                                <div class="flex items-center gap-2">
                                    {#if asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                                        <div>
                                            <p
                                                class="text-2xl font-black text-slate-900"
                                            >
                                                {formatGermanAmount(
                                                    asset.activeVersion.subAssets.reduce(
                                                        (sum, sa) =>
                                                            sum +
                                                            sa.amountPerMonth,
                                                        0,
                                                    ),
                                                )} €
                                            </p>
                                            <p
                                                class="text-[8px] font-black text-indigo-500 uppercase tracking-[0.2em] mt-0.5"
                                            >
                                                Target Sum
                                            </p>
                                        </div>
                                    {:else if asset.activeVersion.amountPerMonth > 0}
                                        <p
                                            class="text-2xl font-black text-slate-900"
                                        >
                                            {formatGermanAmount(
                                                asset.activeVersion
                                                    .amountPerMonth,
                                            )} €
                                        </p>
                                    {:else}
                                        <div
                                            class="flex items-center gap-1.5 text-indigo-600 bg-indigo-50 px-2 py-1 rounded-lg border border-indigo-100 shadow-sm"
                                        >
                                            <Waves class="w-3.5 h-3.5" />
                                            <span
                                                class="text-[10px] font-black uppercase tracking-[0.2em]"
                                                >Remainder</span
                                            >
                                        </div>
                                    {/if}
                                </div>
                            </div>
                            <div>
                                <p
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 mb-1"
                                >
                                    Interest
                                </p>
                                <p class="text-2xl font-black text-slate-900">
                                    {asset.activeVersion.type === "ETF"
                                        ? "STOCHASTIC"
                                        : formatGermanAmount(
                                              asset.activeVersion.interestRate,
                                          ) + "%"}
                                </p>
                                {#if asset.activeVersion.penalties && asset.activeVersion.penalties.length > 0}
                                    {#each asset.activeVersion.penalties as penalty}
                                        <p
                                            class="text-[9px] font-black text-rose-500 uppercase tracking-tighter mt-1 flex items-center gap-1"
                                        >
                                            <AlertCircle class="w-2.5 h-2.5" />
                                            {penalty.name}: {formatGermanAmount(
                                                penalty.percentage,
                                            )}% ({penalty.triggerType ===
                                            "WITHDRAWAL"
                                                ? "W"
                                                : "I"})
                                        </p>
                                    {/each}
                                {/if}
                            </div>
                        </div>

                        <!-- ETF Tracker Detail Overlay (If ETF) -->
                        {#if asset.activeVersion.type === "ETF" && asset.activeVersion.etfConfig && asset.activeVersion.etfConfig.length > 0}
                            <div
                                class="p-4 bg-slate-50/50 border border-slate-100 rounded-2xl space-y-3"
                            >
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400"
                                >
                                    Portfolio Nodes
                                </p>
                                <div class="space-y-2">
                                    {#each asset.activeVersion.etfConfig as tracker}
                                        <div
                                            class="flex justify-between items-center text-[10px]"
                                        >
                                            <span
                                                class="font-bold text-slate-600"
                                                >{tracker.tracker}
                                                {#if tracker.historicalTracker}
                                                    <span
                                                        class="text-slate-400 font-medium ml-1"
                                                        >({tracker.historicalTracker}{tracker.conversionTracker
                                                            ? ` / ${tracker.conversionTracker}`
                                                            : ""})</span
                                                    >
                                                {/if}</span
                                            >
                                            <div
                                                class="flex items-center gap-3"
                                            >
                                                <span
                                                    class="font-black text-slate-900"
                                                    >{(
                                                        tracker.percentage * 100
                                                    ).toFixed(0)}%</span
                                                >
                                                <span
                                                    class="px-1.5 py-0.5 bg-white border border-slate-200 rounded text-[8px] font-black text-slate-400"
                                                    >TER {tracker.ter}%</span
                                                >
                                            </div>
                                        </div>
                                    {/each}
                                </div>
                            </div>
                        {/if}

                        <!-- Sub-Assets Targets Detail Overlay -->
                        {#if asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                            <div
                                class="p-4 bg-slate-50/50 border border-slate-100 rounded-2xl space-y-3"
                            >
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 flex items-center gap-1"
                                >
                                    <Target
                                        class="w-3.5 h-3.5 text-indigo-500"
                                    /> Logical Targets ({asset.activeVersion
                                        .subAssets.length})
                                </p>
                                <div
                                    class="space-y-2 max-h-[160px] overflow-y-auto pr-1"
                                >
                                    {#each asset.activeVersion.subAssets as target}
                                        <div
                                            class="flex flex-col p-2.5 bg-white border border-slate-100 rounded-xl space-y-1 shadow-sm"
                                        >
                                            <div
                                                class="flex justify-between items-center text-[11px]"
                                            >
                                                <span
                                                    class="font-black text-slate-700"
                                                    >{target.name}</span
                                                >
                                                <span
                                                    class="font-black text-indigo-600 bg-indigo-50 px-1.5 py-0.5 rounded text-[9px] tracking-tight"
                                                >
                                                    Goal: {formatGermanAmount(
                                                        parseFloat(
                                                            String(target.targetValue),
                                                        ),
                                                    )} €
                                                </span>
                                            </div>
                                            <div
                                                class="flex justify-between items-center text-[9px] text-slate-400 font-bold"
                                            >
                                                <span
                                                    >Contrib: {formatGermanAmount(
                                                        target.amountPerMonth,
                                                    )} €/m</span
                                                >
                                                {#if target.dumpingLoanId}
                                                    <span
                                                        class="text-emerald-600 bg-emerald-50 px-1.5 py-0.5 rounded tracking-tighter flex items-center gap-0.5"
                                                    >
                                                        <Activity
                                                            class="w-2.5 h-2.5"
                                                        /> Dumps: {loans.find(
                                                            (l) =>
                                                                l.id ===
                                                                target.dumpingLoanId,
                                                        )?.name || "Loan"}
                                                    </span>
                                                {/if}
                                            </div>
                                            <div
                                                class="flex items-center gap-1 text-[9px] text-slate-400 font-medium"
                                            >
                                                <Calendar
                                                    class="w-3 h-3 text-slate-350"
                                                />
                                                <span
                                                    >Active: {formatDate(
                                                        target.startDate,
                                                    )} - {target.endDate
                                                        ? formatDate(
                                                              target.endDate,
                                                          )
                                                        : "Ongoing"}</span
                                                >
                                            </div>
                                        </div>
                                    {/each}
                                </div>
                            </div>
                        {/if}

                        <div
                            class="flex items-center gap-6 pt-6 border-t border-slate-100"
                        >
                            <div class="space-y-1 flex-1">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 flex items-center gap-1"
                                >
                                    <Calendar class="w-3 h-3" /> Started
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {formatDate(asset.activeVersion.startDate)}
                                </p>
                            </div>
                            <div class="space-y-1 flex-1 text-right">
                                <p
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 mb-0.5 flex items-center justify-end gap-1"
                                >
                                    <Target class="w-3 h-3" /> Target
                                </p>
                                <p
                                    class="text-sm font-black text-indigo-600 truncate"
                                >
                                    {#if asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                                        {formatGermanAmount(
                                            asset.activeVersion.subAssets.reduce(
                                                (sum, sa) =>
                                                    sum +
                                                    (parseFloat(
                                                        String(sa.targetValue),
                                                    ) || 0),
                                                0,
                                            ),
                                        )} €
                                    {:else if asset.activeVersion.dumpingLoanId}
                                        Dumps: {loans.find(
                                            (l) =>
                                                l.id ===
                                                asset.activeVersion
                                                    .dumpingLoanId,
                                        )?.name || "Loading..."}
                                    {:else}
                                        {formatGermanAmount(
                                            parseFloat(
                                                String(asset.activeVersion.targetValue),
                                            ),
                                        )} €
                                    {/if}
                                </p>
                                {#if asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                                    <p
                                        class="text-[8px] font-black text-indigo-500 uppercase tracking-[0.2em] mt-0.5"
                                    >
                                        Multiple Targets
                                    </p>
                                {/if}
                            </div>
                        </div>
                    </div>
                </div>
            {/each}
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
                    <div
                        class="space-y-4 p-6 bg-white rounded-2xl border border-slate-100 shadow-sm"
                    >
                        <div class="flex items-center justify-between">
                            <div class="space-y-0.5">
                                <label class="text-sm font-black text-slate-900"
                                    >Logical Sub-Assets / Targets</label
                                >
                                <p
                                    class="text-[10px] font-medium text-slate-500"
                                >
                                    Define multiple logical target sub-assets
                                    sharing this same account.
                                </p>
                            </div>
                            <button
                                type="button"
                                onclick={() => {
                                    if (!currentAsset.activeVersion.subAssets) {
                                        currentAsset.activeVersion.subAssets =
                                            [];
                                    }
                                    currentAsset.activeVersion.subAssets.push({
                                        id: crypto.randomUUID(),
                                        name: "",
                                        targetValue: "0",
                                        amountPerMonth: 0,
                                        isRemainderConsumer: false,
                                        remainderStartDate: null,
                                        dumpingLoanId: null,
                                        startDate:
                                            currentAsset.activeVersion
                                                .startDate ||
                                            new Date().toISOString(),
                                        endDate: null,
                                        earliestDumpDate: null,
                                    });
                                }}
                                class="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-[10px] font-black uppercase tracking-wider transition-colors shadow-sm flex items-center gap-1"
                            >
                                <Plus class="w-3 h-3" /> Add Target
                            </button>
                        </div>

                        {#if currentAsset.activeVersion.subAssets && currentAsset.activeVersion.subAssets.length > 0}
                            <div class="space-y-4 pt-2">
                                {#each currentAsset.activeVersion.subAssets as target, i}
                                    <div
                                        class="p-4 bg-white border border-slate-100 rounded-xl space-y-3 relative shadow-sm"
                                        transition:slide
                                    >
                                        <div
                                            class="flex justify-between items-center"
                                        >
                                            <span
                                                class="px-2 py-0.5 bg-indigo-50 text-indigo-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]"
                                            >
                                                Target #{i + 1}
                                            </span>
                                            <button
                                                type="button"
                                                onclick={() =>
                                                    currentAsset.activeVersion.subAssets.splice(
                                                        i,
                                                        1,
                                                    )}
                                                class="text-rose-400 hover:text-rose-600 transition-colors"
                                                title="Remove Target"
                                            >
                                                <Trash2 class="w-4 h-4" />
                                            </button>
                                        </div>

                                        <div
                                            class="grid grid-cols-1 md:grid-cols-2 gap-4"
                                        >
                                            <div class="space-y-1">
                                                <label
                                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                    >Target Name</label
                                                >
                                                <input
                                                    type="text"
                                                    bind:value={target.name}
                                                    placeholder="e.g. Umzug München"
                                                    class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                    required
                                                />
                                            </div>
                                            <div class="grid grid-cols-2 gap-2">
                                                <div class="space-y-1">
                                                    <div
                                                        class="flex items-center justify-between"
                                                    >
                                                        <label
                                                            class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                            >Monthly savings (€)</label
                                                        >
                                                        <button
                                                            type="button"
                                                            onclick={() => {
                                                                const endDate =
                                                                    target.endDate ||
                                                                    currentAsset
                                                                        .activeVersion
                                                                        .endDate;
                                                                const rate =
                                                                    calculateRequiredRate(
                                                                        String(target.targetValue),
                                                                        target.startDate,
                                                                        endDate,
                                                                        currentAsset
                                                                            .activeVersion
                                                                            .type ===
                                                                            "STATIC"
                                                                            ? parseNumeric(
                                                                                  interestInput,
                                                                                  "DE",
                                                                              )
                                                                            : 0,
                                                                    );
                                                                target.amountPerMonth =
                                                                    Math.round(
                                                                        rate *
                                                                            100,
                                                                    ) / 100;
                                                            }}
                                                            class="text-[8px] font-black text-emerald-600 hover:underline uppercase flex items-center gap-0.5"
                                                        >
                                                            Recalc
                                                        </button>
                                                    </div>
                                                    <input
                                                        type="number"
                                                        bind:value={
                                                            target.amountPerMonth
                                                        }
                                                        step="0.01"
                                                        placeholder="150"
                                                        class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                        required
                                                    />
                                                </div>
                                                <div class="space-y-1">
                                                    <label
                                                        class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                        >Goal target (€)</label
                                                    >
                                                    <input
                                                        type="text"
                                                        bind:value={
                                                            target.targetValue
                                                        }
                                                        placeholder="5000"
                                                        class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                        required
                                                    />
                                                    <span class="text-[9px] text-slate-400">
                                                        Goal: {formatGermanAmount(
                                                            parseFloat(
                                                                String(target.targetValue),
                                                            ),
                                                        )} €
                                                    </span>
                                                </div>
                                            </div>
                                        </div>

                                        <div
                                            class="grid grid-cols-1 md:grid-cols-3 gap-4 pt-1"
                                        >
                                            <div class="space-y-1">
                                                <label
                                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                    >Start Month</label
                                                >
                                                <input
                                                    type="month"
                                                    value={toInputMonth(
                                                        target.startDate,
                                                    )}
                                                    oninput={(e: any) =>
                                                        (target.startDate =
                                                            fromInputMonth(
                                                                e.target.value,
                                                            ))}
                                                    class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                    required
                                                />
                                            </div>
                                            <div class="space-y-1">
                                                <label
                                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                    >Remainder Start</label
                                                >
                                                <input
                                                    type="month"
                                                    value={toInputMonth(
                                                        target.remainderStartDate,
                                                    )}
                                                    oninput={(e: any) =>
                                                        (target.remainderStartDate =
                                                            e.target.value
                                                                ? fromInputMonth(
                                                                      e.target
                                                                          .value,
                                                                  )
                                                                : null)}
                                                    class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                />
                                            </div>
                                            <div class="space-y-1">
                                                <label
                                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                    >Payout Month (Optional)</label
                                                >
                                                <input
                                                    type="month"
                                                    value={toInputMonth(
                                                        target.endDate,
                                                    )}
                                                    oninput={(e: any) =>
                                                        (target.endDate = e
                                                            .target.value
                                                            ? fromInputMonth(
                                                                  e.target
                                                                      .value,
                                                              )
                                                            : null)}
                                                    class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                />
                                            </div>
                                        </div>

                                        <div class="flex justify-end pt-1">
                                            <button
                                                type="button"
                                                onclick={() => {
                                                    const endDate =
                                                        target.endDate ||
                                                        currentAsset
                                                            .activeVersion
                                                            .endDate;
                                                    const rate =
                                                        calculateRequiredRate(
                                                            String(target.targetValue),
                                                            target.startDate,
                                                            endDate,
                                                            currentAsset
                                                                .activeVersion
                                                                .type ===
                                                                "STATIC"
                                                                ? parseNumeric(
                                                                      interestInput,
                                                                      "DE",
                                                                  )
                                                                : 0,
                                                        );
                                                    target.amountPerMonth =
                                                        Math.round(rate * 100) /
                                                        100;
                                                }}
                                                class="px-2.5 py-1 bg-indigo-50 hover:bg-indigo-100 text-indigo-600 rounded-lg text-[9px] font-black uppercase tracking-wider transition-colors shadow-sm flex items-center gap-1"
                                            >
                                                Recalculate Rate
                                            </button>
                                        </div>

                                        <div
                                            class="grid grid-cols-1 md:grid-cols-2 gap-4 pt-1 items-center font-bold"
                                        >
                                            <div class="space-y-1">
                                                <div
                                                    class="flex items-center gap-2"
                                                >
                                                    <input
                                                        type="checkbox"
                                                        id="dump_target_{target.id}"
                                                        checked={target.dumpingLoanId !== null && target.dumpingLoanId !== ""}
                                                        onchange={(e: any) => {
                                                            target.dumpingLoanId =
                                                                e.target.checked
                                                                    ? loans[0]
                                                                          ?.id ||
                                                                      ""
                                                                    : null;
                                                            if (
                                                                !e.target
                                                                    .checked
                                                            ) {
                                                                target.earliestDumpDate =
                                                                    null;
                                                            }
                                                        }}
                                                        class="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                                                    />
                                                    <label
                                                        for="dump_target_{target.id}"
                                                        class="text-[10px] font-bold text-slate-600 cursor-pointer"
                                                    >
                                                        Enable Target Loan
                                                        Dumping
                                                    </label>
                                                </div>
                                                <div
                                                    class="flex items-center gap-2"
                                                >
                                                    <input
                                                        type="checkbox"
                                                        id="remainder_target_{target.id}"
                                                        bind:checked={
                                                            target.isRemainderConsumer
                                                        }
                                                        class="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                                                    />
                                                    <label
                                                        for="remainder_target_{target.id}"
                                                        class="text-[10px] font-bold text-slate-600 cursor-pointer"
                                                    >
                                                        Enable Remainder
                                                        Consumption
                                                    </label>
                                                </div>
                                            </div>
                                            {#if target.dumpingLoanId !== null && target.dumpingLoanId !== ""}
                                                <div
                                                    class="grid grid-cols-1 md:grid-cols-2 gap-3"
                                                    transition:slide
                                                >
                                                    <div class="space-y-1">
                                                        <label
                                                            class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                            >Target Loan</label
                                                        >
                                                        <select
                                                            bind:value={
                                                                target.dumpingLoanId
                                                            }
                                                            class="block w-full px-3 py-1.5 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold appearance-none cursor-pointer focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                        >
                                                            <option
                                                                value=""
                                                                disabled
                                                                >Select Loan...</option
                                                            >
                                                            {#each loans as loan}
                                                                <option
                                                                    value={loan.id}
                                                                    >{loan.name}</option
                                                                >
                                                            {/each}
                                                        </select>
                                                    </div>
                                                    <div class="space-y-1">
                                                        <label
                                                            class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                                                            >Earliest Dump Month
                                                            (Optional)</label
                                                        >
                                                        <input
                                                            type="month"
                                                            value={toInputMonth(
                                                                target.earliestDumpDate,
                                                            )}
                                                            oninput={(e: any) =>
                                                                (target.earliestDumpDate =
                                                                    e.target
                                                                        .value
                                                                        ? fromInputMonth(
                                                                              e
                                                                                  .target
                                                                                  .value,
                                                                          )
                                                                        : null)}
                                                            class="block w-full px-3 py-1.5 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                                        />
                                                    </div>
                                                </div>
                                            {/if}
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        {/if}
                    </div>

                    {#if currentAsset.activeVersion.type === "ETF"}
                        <div class="space-y-4">
                            <div class="flex items-center justify-between">
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                    >ETF Tracker Nodes</label
                                >
                                <button
                                    type="button"
                                    onclick={() =>
                                        currentAsset.activeVersion.etfConfig.push(
                                            {
                                                tracker: "",
                                                historicalTracker: "",
                                                conversionTracker: "",
                                                historyProvider: "",
                                                percentage: 0.7,
                                                ter: 0.2,
                                                stitchingSegments: [],
                                            },
                                        )}
                                    class="text-[9px] font-black text-emerald-600 hover:underline uppercase"
                                    >+ Add Tracker</button
                                >
                            </div>
                            {#each currentAsset.activeVersion.etfConfig as etf, i}
                                <div class="p-4 bg-slate-50 border border-slate-200 rounded-xl space-y-3">
                                    <div
                                        class="grid grid-cols-12 gap-2 items-center"
                                    >
                                        <input
                                            bind:value={etf.tracker}
                                            placeholder="Ticker"
                                            class="col-span-2 px-3 py-2 bg-white border border-slate-200 rounded-lg text-[10px] font-bold"
                                        />
                                        <input
                                            bind:value={etf.historicalTracker}
                                            placeholder="Index"
                                            class="col-span-2 px-3 py-2 bg-white border border-slate-200 rounded-lg text-[10px] font-bold"
                                        />
                                        <input
                                            bind:value={etf.conversionTracker}
                                            placeholder="Conv"
                                            class="col-span-2 px-3 py-2 bg-white border border-slate-200 rounded-lg text-[10px] font-bold"
                                        />
                                        <select
                                            bind:value={etf.historyProvider}
                                            class="col-span-2 px-3 py-2 bg-white border border-slate-200 rounded-lg text-[10px] font-bold"
                                        >
                                            <option value="">Yahoo</option>
                                            <option value="solactive">Solactive</option>
                                            <option value="msci">MSCI</option>
                                            <option value="justetf">justETF</option>
                                        </select>
                                        <input
                                            type="number"
                                            bind:value={etf.percentage}
                                            step="0.001"
                                            placeholder="0.7"
                                            class="col-span-2 px-3 py-2 bg-white border border-slate-200 rounded-lg text-[10px] font-bold"
                                        />
                                        <input
                                            type="number"
                                            bind:value={etf.ter}
                                            step="0.001"
                                            placeholder="0.2"
                                            class="col-span-1 px-3 py-2 bg-white border border-slate-200 rounded-lg text-[10px] font-bold"
                                        />
                                        <button
                                            type="button"
                                            onclick={() =>
                                                currentAsset.activeVersion.etfConfig.splice(
                                                    i,
                                                    1,
                                                )}
                                            class="col-span-1 text-rose-400 hover:text-rose-600 transition-colors"
                                            ><Trash2 class="w-4 h-4 mx-auto" /></button
                                        >
                                    </div>

                                    <!-- Stitching segments section -->
                                    <div class="pl-4 border-l-2 border-slate-200 space-y-2">
                                        <div class="flex justify-between items-center">
                                            <span class="text-[9px] font-black uppercase text-slate-400">History Stitching (Optional backfill)</span>
                                            <button
                                                type="button"
                                                onclick={() => {
                                                    if (!etf.stitchingSegments) {
                                                        etf.stitchingSegments = [];
                                                    }
                                                    etf.stitchingSegments.push({ provider: "", lookupTicker: "", conversionTracker: "" });
                                                }}
                                                class="text-[8px] font-black text-emerald-600 hover:underline uppercase"
                                                >+ Add Stitching Segment</button
                                            >
                                        </div>
                                        {#if etf.stitchingSegments && etf.stitchingSegments.length > 0}
                                            {#each etf.stitchingSegments as seg, segIdx}
                                                <div class="grid grid-cols-12 gap-2 items-center">
                                                    <select
                                                        bind:value={seg.provider}
                                                        class="col-span-3 px-2 py-1 bg-white border border-slate-200 rounded-lg text-[9px] font-bold"
                                                    >
                                                        <option value="">Yahoo</option>
                                                        <option value="solactive">Solactive</option>
                                                        <option value="msci">MSCI</option>
                                                        <option value="justetf">justETF</option>
                                                    </select>
                                                    <input
                                                        bind:value={seg.lookupTicker}
                                                        placeholder="Lookup Ticker (e.g. ^GSPC, ISIN)"
                                                        class="col-span-4 px-2 py-1 bg-white border border-slate-200 rounded-lg text-[9px] font-bold"
                                                    />
                                                    <input
                                                        bind:value={seg.conversionTracker}
                                                        placeholder="Conv (e.g. USDEUR=X)"
                                                        class="col-span-4 px-2 py-1 bg-white border border-slate-200 rounded-lg text-[9px] font-bold"
                                                    />
                                                    <button
                                                        type="button"
                                                        onclick={() => etf.stitchingSegments?.splice(segIdx, 1)}
                                                        class="col-span-1 text-rose-400 hover:text-rose-600 transition-colors text-[9px] font-bold"
                                                        >✕</button
                                                    >
                                                </div>
                                            {/each}
                                            <p class="text-[8px] text-slate-400 italic">Segments are stitched chronologically. Primary (top) runs first, and older/missing history is filled by subsequent backfill segments scaled at overlap dates.</p>
                                        {/if}
                                    </div>
                                </div>
                            {/each}
                        </div>
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
