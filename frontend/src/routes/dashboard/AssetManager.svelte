<script lang="ts">
    import { wsCall, decode } from "$lib/utils/ws_fetch";
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
    import { toInputMonth, fromInputMonth } from "$lib/utils/date";


    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Undo2,
        Archive,
        Pencil,
        Copy,
        PieChart,
        AlertCircle,
        Activity,
        Layers,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";
    import SubAssetForm from "./assets/components/SubAssetForm.svelte";
    import EtfConfigForm from "./assets/components/EtfConfigForm.svelte";
    import Button from "$lib/components/ui/Button.svelte";
    import Input from "$lib/components/ui/Input.svelte";
    import CurrencyInput from "$lib/components/ui/CurrencyInput.svelte";
    import Badge from "$lib/components/ui/Badge.svelte";
    import Modal from "$lib/components/ui/Modal.svelte";
    import ConfirmModal from "$lib/components/ui/ConfirmModal.svelte";
    import Table from "$lib/components/ui/Table.svelte";

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
        targetValue: string | number;
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
        taxAllowance?: number;
        taxAllowanceStartDate?: string | null;
        taxAllowanceEndDate?: string | null;
        taxAllowances?: TaxAllowance[];
    }

    interface TaxAllowance {
        id?: string;
        amount: number;
        startDate?: string | null;
        endDate?: string | null;
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
    let taxAllowanceInput = $state("");

    function addTaxAllowance() {
        if (!currentAsset.activeVersion) return;
        if (!currentAsset.activeVersion.taxAllowances) {
            currentAsset.activeVersion.taxAllowances = [];
        }
        currentAsset.activeVersion.taxAllowances.push({
            id: crypto.randomUUID(),
            amount: 1000,
            startDate: null,
            endDate: null,
        });
    }

    function removeTaxAllowance(idx: number) {
        if (!currentAsset.activeVersion || !currentAsset.activeVersion.taxAllowances) return;
        currentAsset.activeVersion.taxAllowances.splice(idx, 1);
    }
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
                taxAllowance: 0,
                taxAllowanceStartDate: null,
                taxAllowanceEndDate: null,
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
        if (!val) return 0;
        if (locale === "DE") return parseGermanAmount(val);
        let clean = val.toString().trim().replace(/,/g, "");
        return parseFloat(clean) || 0;
    }

    function formatGermanNumeric(val: number | string | null | undefined): string {
        if (val === null || val === undefined) return "";
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
                            currentAsset.activeVersion.amountPerMonth || 0,
                        remainderStartDate:
                            currentAsset.activeVersion.remainderStartDate || "",
                        startDate: currentAsset.activeVersion.startDate || "",
                        endDate: currentAsset.activeVersion.endDate || "",
                        useForPassiveIncome:
                            !!currentAsset.activeVersion.useForPassiveIncome,
                        taxAllowance:
                            parseFloat(
                                currentAsset.activeVersion.taxAllowance as any,
                            ) || 0,
                        taxAllowanceStartDate:
                            currentAsset.activeVersion.taxAllowanceStartDate || "",
                        taxAllowanceEndDate:
                            currentAsset.activeVersion.taxAllowanceEndDate || "",
                        taxAllowances: (
                            currentAsset.activeVersion.taxAllowances || []
                        ).map((ta) => ({
                            id: ta.id || "",
                            amount: parseFloat(ta.amount as any) || 0,
                            startDate: ta.startDate || "",
                            endDate: ta.endDate || "",
                        })),
                        etfConfig: (
                            currentAsset.activeVersion.etfConfig || []
                        ).map((t) => ({
                            tracker: t.tracker || "",
                            historicalTracker: t.historicalTracker || "",
                            conversionTracker: t.conversionTracker || "",
                            historyProvider: t.historyProvider || "",
                            percentage: parseFloat(t.percentage as any) || 0,
                            ter: parseFloat(t.ter as any) || 0,
                            stitchingSegments: (
                                t.stitchingSegments || []
                            ).map((seg) => ({
                                provider: seg.provider || "",
                                lookupTicker: seg.lookupTicker || "",
                                conversionTracker:
                                    seg.conversionTracker || "",
                            })),
                        })),
                        penalties: (
                            currentAsset.activeVersion.penalties || []
                        ).map((p) => ({
                            name: p.name || "",
                            triggerType: p.triggerType || "WITHDRAWAL",
                            percentage: p.percentage || 0,
                        })),
                        subAssets: (
                            currentAsset.activeVersion.subAssets || []
                        ).map((sa) => ({
                            id: sa.id || "",
                            name: sa.name || "",
                            targetValue:
                                parseFloat(sa.targetValue as any) || 0,
                            amountPerMonth: sa.amountPerMonth || 0,
                            isRemainderConsumer: !!sa.isRemainderConsumer,
                            remainderStartDate: sa.remainderStartDate || "",
                            dumpingLoanId: sa.dumpingLoanId || "",
                            startDate: sa.startDate || "",
                            endDate: sa.endDate || "",
                            earliestDumpDate: sa.earliestDumpDate || "",
                            expenseId: sa.expenseId || "",
                            remainderPriority: sa.remainderPriority || 0,
                        })),
                    },
                },
                [AssetSchema, ErrorSchema],
            ).one();
            if (err) throw err;
            showAddModal = false;
            await fetchData();
        } catch (err: any) {
            error = err.message;
        } finally {
            isSaving = false;
        }
    }

    async function deleteAsset(mode?: string) {
        if (!assetToDelete) return;
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
            copiedAsset.id = crypto.randomUUID();
            copiedAsset.name = copiedAsset.name + " (Copy)";

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
                        id: "",
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
                        taxAllowance: parseFloat(av.taxAllowance as any) || 0,
                        taxAllowanceStartDate: av.taxAllowanceStartDate || "",
                        taxAllowanceEndDate: av.taxAllowanceEndDate || "",
                        taxAllowances: (av.taxAllowances || []).map((ta: any) => ({
                            id: ta.id || "",
                            amount: parseFloat(ta.amount) || 0,
                            startDate: ta.startDate || "",
                            endDate: ta.endDate || "",
                        })),
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
                            triggerType: p.triggerType || "WITHDRAWAL",
                            percentage: p.percentage || 0,
                        })),
                        subAssets: (av.subAssets || []).map((sa: any) => ({
                            id: sa.id || "",
                            name: sa.name || "",
                            targetValue: parseFloat(sa.targetValue) || 0,
                            amountPerMonth: sa.amountPerMonth || 0,
                            isRemainderConsumer: !!sa.isRemainderConsumer,
                            remainderStartDate: sa.remainderStartDate || "",
                            dumpingLoanId: sa.dumpingLoanId || "",
                            startDate: sa.startDate || "",
                            endDate: sa.endDate || "",
                            earliestDumpDate: sa.earliestDumpDate || "",
                            expenseId: sa.expenseId || "",
                            remainderPriority: sa.remainderPriority || 0,
                        })),
                    },
                },
                [AssetSchema, ErrorSchema],
            ).one();

            if (err) throw err;
            await fetchData();
        } catch (err: any) {
            alert("Duplication failed: " + err.message);
        }
    }

    function editAsset(asset: Asset) {
        currentAsset = decode(asset);
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
        taxAllowanceInput = formatGermanNumeric(
            currentAsset.activeVersion.taxAllowance || 0,
        );
        showAddModal = true;
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
    <div class="flex flex-col md:flex-row md:items-center justify-between gap-6">
        <div>
            <h2 class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500">
                ETF & Assets
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Deterministic growth models, compound interest, and investment portfolios.
            </p>
        </div>
        <div>
            <Button
                onclick={() => {
                    currentAsset = createNewAsset();
                    amountInput = "";
                    targetInput = "";
                    interestInput = "";
                    taxAllowanceInput = "";
                    showAddModal = true;
                }}
                class="bg-emerald-600 hover:bg-emerald-700 shadow-emerald-100"
            >
                <Plus class="w-5 h-5" />
                Add Asset
            </Button>
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
                    Connection Error
                </p>
                <p class="text-sm font-bold">{error}</p>
            </div>
            <Button
                onclick={fetchData}
                class="bg-rose-600 text-white hover:bg-rose-700 shadow-rose-200"
            >
                Retry
            </Button>
        </div>
    {/if}

    {#if isLoading}
        <div class="flex flex-col items-center justify-center py-20 space-y-4">
            <div class="w-10 h-10 border-4 border-t-emerald-600 border-emerald-100 rounded-full animate-spin"></div>
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]">
                Loading Assets...
            </p>
        </div>
    {:else if assets.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner">
                <PieChart class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Assets Configured
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Configure long-term savings accounts, mutual funds, or stock portfolios.
                </p>
            </div>
            <Button
                variant="secondary"
                onclick={() => (showAddModal = true)}
                class="mx-auto"
            >
                Initialize First Entry
            </Button>
        </div>
    {:else}
        <Table>
            {#snippet header()}
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Name</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Type</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Rate/Month</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Interest</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Target Value</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
            {/snippet}
            {#snippet body()}
                {#each sortedAssets as asset (asset.id)}
                    <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                        <td class="px-6 py-4 font-bold text-slate-800">
                            {asset.name}
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            <Badge variant={asset.activeVersion?.type === 'ETF' ? 'success' : 'slate'}>
                                {asset.activeVersion?.type === 'ETF' ? 'ETF Monte Carlo' : 'Static Interest'}
                            </Badge>
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700 tabular-nums">
                            {#if asset.activeVersion && asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                                <span class="text-slate-400 italic">Logical Sub-Assets</span>
                            {:else}
                                € {formatGermanAmount(asset.activeVersion?.amountPerMonth || 0)}
                            {/if}
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            {#if asset.activeVersion?.type === "ETF"}
                                <span class="text-slate-400 italic">Market-Driven</span>
                            {:else}
                                {formatGermanAmount(asset.activeVersion?.interestRate || 0)}% ({asset.activeVersion?.interestInterval})
                            {/if}
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="flex items-center justify-between w-32 ml-auto tabular-nums font-black text-slate-900">
                                <span>€</span>
                                <span>
                                    {#if asset.activeVersion && asset.activeVersion.subAssets && asset.activeVersion.subAssets.length > 0}
                                        {formatGermanAmount(asset.activeVersion.subAssets.reduce((sum, sa) => sum + (parseFloat(String(sa.targetValue)) || 0), 0))}
                                    {:else}
                                        {formatGermanAmount(parseFloat(String(asset.activeVersion?.targetValue)) || 0)}
                                    {/if}
                                </span>
                            </div>
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="inline-flex gap-2">
                                <Button
                                    variant="ghost"
                                    onclick={() => duplicateAsset(asset)}
                                    title="Duplicate Asset"
                                    class="hover:text-indigo-600 hover:bg-indigo-50 hover:border-indigo-100"
                                >
                                    <Copy class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => editAsset(asset)}
                                    title="Refine (New Version)"
                                    class="hover:text-emerald-600 hover:bg-emerald-50 hover:border-emerald-100"
                                >
                                    <Pencil class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => {
                                        assetToDelete = asset.id!;
                                        showDeleteConfirm = true;
                                    }}
                                    title="Lifecycle Action"
                                    class="hover:text-red-600 hover:bg-red-50 hover:border-red-100"
                                >
                                    <Trash2 class="w-4 h-4" />
                                </Button>
                            </div>
                        </td>
                    </tr>
                {/each}
            {/snippet}
        </Table>
    {/if}
</div>

<!-- Add/Edit Modal -->
<Modal
    bind:open={showAddModal}
    title="{currentAsset.id ? 'Edit' : 'New'} Asset"
    subtitle="Define the settings and expected growth for this asset."
    maxWidth="max-w-2xl"
>
    <form
        onsubmit={(e) => {
            e.preventDefault();
            saveAsset();
        }}
        class="space-y-8"
    >
        <div class="space-y-6">
            <div class="grid grid-cols-2 gap-6">
                <Input
                    label="Asset Identity"
                    bind:value={currentAsset.name}
                    placeholder="e.g. Main Savings"
                    required
                />
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

        {#if currentAsset.activeVersion}
            <div class="grid grid-cols-1 md:grid-cols-2 gap-8">
                <div class="space-y-2">
                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                        Growth Model
                    </label>
                    <select
                        bind:value={currentAsset.activeVersion.type}
                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                    >
                        <option value="STATIC">Fixed Interest (e.g. Savings)</option>
                        <option value="ETF">Investment Portfolio (e.g. ETF / Stock Market)</option>
                    </select>
                </div>

                {#if currentAsset.activeVersion.type === "ETF"}
                    <div class="p-4 bg-slate-50 dark:bg-slate-800/40 rounded-2xl flex items-center justify-between gap-4 border border-slate-100 dark:border-slate-800 self-end">
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
                        <div class="space-y-2 {currentAsset.activeVersion.type !== 'STATIC' ? 'md:col-span-3' : ''}">
                            <div class="flex items-center justify-between">
                                <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                                    Monthly Rate (€)
                                </label>
                                {#if !currentAsset.activeVersion.remainderStartDate}
                                    <button
                                        type="button"
                                        onclick={handleRecalculate}
                                        class="text-[9px] font-black text-emerald-600 hover:underline uppercase flex items-center gap-1"
                                    >
                                        <Activity class="w-3 h-3" /> Recalculate
                                    </button>
                                {/if}
                            </div>
                            <input
                                type="text"
                                bind:value={amountInput}
                                placeholder={currentAsset.activeVersion.remainderStartDate ? "Remainder Consumer" : "328,00"}
                                disabled={!!currentAsset.activeVersion.remainderStartDate}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold disabled:opacity-50 dark:bg-slate-800 dark:border-slate-700"
                            />
                        </div>
                    {/if}
                    {#if currentAsset.activeVersion.type === "STATIC" || currentAsset.activeVersion.type === "ETF"}
                        <div class="space-y-2 {currentAsset.activeVersion.subAssets?.length > 0 ? 'md:col-span-2' : ''}">
                            <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                                {currentAsset.activeVersion.type === "ETF" ? 'Payout' : 'Interest'} %
                            </label>
                            <input
                                type="text"
                                bind:value={interestInput}
                                placeholder="2,50"
                                disabled={currentAsset.activeVersion.type === "ETF"}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold disabled:opacity-50 dark:bg-slate-800 dark:border-slate-700"
                            />
                        </div>
                        <div class="space-y-2">
                            <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                                {currentAsset.activeVersion.type === "ETF" ? 'Mode' : 'Interval'}
                            </label>
                            {#if currentAsset.activeVersion.type === "ETF"}
                                <select
                                    bind:value={currentAsset.activeVersion.interestInterval}
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                                >
                                    <option value="Yearly">Accumulating</option>
                                    <option value="Monthly">Distributing (Monthly Payout)</option>
                                </select>
                            {:else}
                                <select
                                    bind:value={currentAsset.activeVersion.interestInterval}
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                                >
                                    <option value="Monthly">Monthly</option>
                                    <option value="Yearly">Yearly</option>
                                </select>
                            {/if}
                        </div>
                    {/if}
                </div>
            {/if}

            {#if !currentAsset.activeVersion.subAssets || currentAsset.activeVersion.subAssets.length === 0}
                <div class="space-y-2" transition:slide>
                    <Input
                        label="Target Value (€)"
                        bind:value={targetInput}
                        placeholder="301000"
                    />
                </div>

                <!-- Termination Trigger Section -->
                <div class="space-y-4 p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm animate-fade-in" transition:slide>
                    <div class="space-y-2">
                        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                            End Saving Trigger
                        </label>
                        <select
                            bind:value={currentAsset.activeVersion.stopModificationId}
                            class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                        >
                            <option value={null}>None (Ongoing)</option>
                            {#each modifications.filter((m) => m.activeVersion.withdrawalPercentage > 0) as m}
                                <option value={m.id}>
                                    {m.description} ({m.activeVersion.withdrawalPercentage}% Dynamic)
                                </option>
                            {/each}
                        </select>
                        <p class="text-[9px] font-medium text-slate-500 dark:text-slate-400 ml-1">
                            Automatically stop saving and close this asset when the selected lifestyle adjustment starts.
                        </p>
                    </div>
                </div>

                <!-- Loan Dumping Section -->
                <div class="space-y-4 p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm" transition:slide>
                    <div class="flex items-center justify-between">
                        <div class="space-y-0.5">
                            <label class="text-sm font-black text-slate-900 dark:text-slate-100">
                                Enable Loan Dumping
                            </label>
                            <p class="text-[10px] font-medium text-slate-500 dark:text-slate-400">
                                Automatically payoff a loan when this asset has enough funds.
                            </p>
                        </div>
                        <button
                            type="button"
                            onclick={() => (currentAsset.activeVersion.dumpingLoanId = currentAsset.activeVersion.dumpingLoanId ? null : loans[0]?.id || null)}
                            class="w-12 h-6 rounded-full transition-all relative {currentAsset.activeVersion.dumpingLoanId ? 'bg-emerald-500 shadow-lg shadow-emerald-100 dark:shadow-none' : 'bg-slate-200 dark:bg-slate-700'}"
                        >
                            <div class="absolute top-1 left-1 w-4 h-4 bg-white rounded-full transition-all {currentAsset.activeVersion.dumpingLoanId ? 'translate-x-6' : ''}"></div>
                        </button>
                    </div>

                    {#if currentAsset.activeVersion.dumpingLoanId}
                        <div class="space-y-2" transition:slide>
                            <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                                Target Loan
                            </label>
                            <select
                                bind:value={currentAsset.activeVersion.dumpingLoanId}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                            >
                                {#each loans as loan}
                                    <option value={loan.id}>{loan.name}</option>
                                {/each}
                            </select>
                        </div>
                    {/if}
                </div>
            {/if}

            <!-- Tax Allowance Section -->
            <div class="space-y-4 p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm animate-fade-in" transition:slide>
                <div class="flex items-center justify-between">
                    <div class="space-y-0.5">
                        <label class="text-sm font-black text-slate-900 dark:text-slate-100">
                            Tax Allowance (Sparer-Pauschbetrag)
                        </label>
                        <p class="text-[10px] font-medium text-slate-500 dark:text-slate-400">
                            Configure annual tax-free capital gain limits and validity periods for this asset.
                        </p>
                    </div>

                    <button
                        type="button"
                        onclick={addTaxAllowance}
                        class="px-4 py-2 text-xs font-black uppercase tracking-wider text-emerald-600 bg-emerald-50 rounded-xl hover:bg-emerald-100 transition-colors border border-emerald-200 flex items-center gap-1.5"
                    >
                        <Plus class="w-3.5 h-3.5" />
                        Add Allowance
                    </button>
                </div>

                {#if !currentAsset.activeVersion.taxAllowances || currentAsset.activeVersion.taxAllowances.length === 0}
                    <div class="p-6 border border-dashed border-slate-200 rounded-2xl text-center space-y-2">
                        <p class="text-xs font-bold text-slate-400">No tax allowances configured for this asset.</p>
                    </div>
                {:else}
                    <div class="space-y-3">
                        {#each currentAsset.activeVersion.taxAllowances as ta, idx}
                            <div class="grid grid-cols-1 md:grid-cols-12 gap-4 p-4 bg-slate-50/50 rounded-2xl border border-slate-100 items-end">
                                <div class="md:col-span-4 space-y-1">
                                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 block">
                                        Amount (€/year)
                                    </label>
                                    <input
                                        type="number"
                                        step="0.01"
                                        bind:value={ta.amount}
                                        placeholder="1000.00"
                                        class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all font-bold text-xs"
                                    />
                                </div>
                                <div class="md:col-span-3 space-y-1">
                                    <Input
                                        type="month"
                                        label="Start Period"
                                        value={toInputMonth(ta.startDate)}
                                        oninput={(e: any) => ta.startDate = fromInputMonth(e.target.value)}
                                    />
                                </div>
                                <div class="md:col-span-4 space-y-1">
                                    <Input
                                        type="month"
                                        label="End Period"
                                        value={toInputMonth(ta.endDate)}
                                        oninput={(e: any) => ta.endDate = fromInputMonth(e.target.value)}
                                    />
                                </div>
                                <div class="md:col-span-1 flex justify-end pb-1">
                                    <button
                                        type="button"
                                        onclick={() => removeTaxAllowance(idx)}
                                        class="p-2 text-rose-500 hover:bg-rose-50 rounded-xl transition-colors"
                                        title="Delete Allowance"
                                    >
                                        <Trash2 class="w-4 h-4" />
                                    </button>
                                </div>
                            </div>
                        {/each}
                    </div>
                {/if}
            </div>

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

            <!-- Custom Fees & Taxes Section -->
            <div class="space-y-4 p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm">
                <div class="flex items-center justify-between">
                    <div class="space-y-0.5">
                        <label class="text-sm font-black text-slate-900 dark:text-slate-100">
                            Custom Fees & Taxes
                        </label>
                        <p class="text-[10px] font-medium text-slate-500 dark:text-slate-400">
                            Apply fees or taxes automatically on withdrawal or interest generation events.
                        </p>
                    </div>
                    <Button
                        onclick={() => {
                            if (!currentAsset.activeVersion.penalties) {
                                currentAsset.activeVersion.penalties = [];
                            }
                            currentAsset.activeVersion.penalties.push({
                                name: "",
                                triggerType: "WITHDRAWAL",
                                percentage: 0,
                            });
                        }}
                        class="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-[10px] font-black uppercase tracking-wider transition-colors shadow-sm flex items-center gap-1"
                    >
                        <Plus class="w-3 h-3" /> Add Fee/Tax
                    </Button>
                </div>

                {#if currentAsset.activeVersion.penalties && currentAsset.activeVersion.penalties.length > 0}
                    <div class="space-y-3 pt-2">
                        {#each currentAsset.activeVersion.penalties as penalty, i}
                            <div class="grid grid-cols-12 gap-3 items-center" transition:slide>
                                <div class="col-span-5">
                                    <Input
                                        bind:value={penalty.name}
                                        placeholder="e.g. Capital Gains Tax"
                                        required
                                        class="px-3 py-2 text-xs"
                                    />
                                </div>
                                <div class="col-span-4">
                                    <select
                                        bind:value={penalty.triggerType}
                                        class="block w-full px-3 py-2 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all text-xs font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                                    >
                                        <option value="WITHDRAWAL">On Withdrawal</option>
                                        <option value="INTEREST">On Interest</option>
                                    </select>
                                </div>
                                <div class="col-span-2 relative">
                                    <input
                                        type="number"
                                        bind:value={penalty.percentage}
                                        step="0.001"
                                        placeholder="25.0"
                                        class="block w-full pl-3 pr-6 py-2 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all text-xs font-bold dark:bg-slate-800 dark:border-slate-700"
                                        required
                                    />
                                    <span class="absolute right-3 top-2 text-xs text-slate-400 font-bold">%</span>
                                </div>
                                <div class="col-span-1 flex justify-center">
                                    <button
                                        type="button"
                                        onclick={() => currentAsset.activeVersion.penalties.splice(i, 1)}
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
                <Input
                    type="month"
                    label="Start Month"
                    value={toInputMonth(currentAsset.activeVersion.startDate)}
                    oninput={(e: any) => (currentAsset.activeVersion.startDate = fromInputMonth(e.target.value))}
                    required
                />
                <Input
                    type="month"
                    label="Remainder Start"
                    value={toInputMonth(currentAsset.activeVersion.remainderStartDate)}
                    oninput={(e: any) => {
                        currentAsset.activeVersion.remainderStartDate = e.target.value ? fromInputMonth(e.target.value) : null;
                        if (currentAsset.activeVersion.remainderStartDate) {
                            amountInput = "";
                        }
                    }}
                />
                <Input
                    type="month"
                    label="Payout Month (Optional)"
                    value={toInputMonth(currentAsset.activeVersion.endDate)}
                    oninput={(e: any) => (currentAsset.activeVersion.endDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                />
            </div>
        {/if}

        <div class="pt-6">
            <Button
                type="submit"
                variant="primary"
                loading={isSaving}
                loadingLabel="Processing Model..."
                class="w-full py-4 text-lg bg-emerald-600 hover:bg-emerald-700 shadow-emerald-100"
            >
                Commit Wealth Version
            </Button>
        </div>
    </form>
</Modal>

<!-- Deletion Confirmation Modal -->
<ConfirmModal
    bind:open={showDeleteConfirm}
    title="Asset Lifecycle Options"
    description="How would you like to handle this delete action?"
>
    <div class="grid grid-cols-1 gap-4">
        <button
            onclick={() => confirmDelete("revert")}
            class="flex items-center gap-4 p-5 rounded-2xl border-2 border-emerald-50 hover:border-emerald-100 hover:bg-emerald-50 dark:border-slate-800 dark:hover:bg-slate-800/50 transition-all text-left group"
        >
            <div class="p-3 bg-emerald-100 dark:bg-emerald-500/20 rounded-xl group-hover:scale-110 transition-transform">
                <Undo2 class="w-6 h-6 text-emerald-600 dark:text-emerald-400" />
            </div>
            <div>
                <p class="font-black text-slate-900 dark:text-slate-100 leading-tight">
                    Revert Version
                </p>
                <p class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Delete only the latest growth snapshot.
                </p>
            </div>
        </button>
        <button
            onclick={() => confirmDelete("full")}
            class="flex items-center gap-4 p-5 rounded-2xl border-2 border-rose-50 hover:border-rose-100 hover:bg-rose-50 dark:border-slate-800 dark:hover:bg-slate-800/50 transition-all text-left group"
        >
            <div class="p-3 bg-rose-100 dark:bg-rose-500/20 rounded-xl group-hover:scale-110 transition-transform">
                <Archive class="w-6 h-6 text-rose-600 dark:text-rose-400" />
            </div>
            <div>
                <p class="font-black text-slate-900 dark:text-slate-100 leading-tight">
                    Archive Asset
                </p>
                <p class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Hide this asset and its complete history.
                </p>
            </div>
        </button>
    </div>
    <Button
        variant="secondary"
        onclick={() => {
            showDeleteConfirm = false;
            assetToDelete = null;
        }}
        class="mt-8 w-full"
    >
        Cancel Action
    </Button>
</ConfirmModal>