<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        ScenarioListSchema,
        ScenarioSchema,
        GenericIDSchema,
        ErrorSchema,
        AssetListSchema,
        LoanListSchema,
        IncomeListSchema,
        BillListSchema,
        ExpenseListSchema,
        ProjectionMonthSchema,
        YieldMapSchema,
        PerformanceMetricsSchema,
    } from "$lib/gen/api_pb.js";
    import { onMount } from "svelte";
    import { fade, slide } from "svelte/transition";
    import {
        Layers,
        Play,
        History,
        Plus,
        Save,
        Trash2,
        Copy,
        Settings2,
        AlertCircle,
        ChevronRight,
        ChevronLeft,
        Waves,
        Activity,
        TrendingUp,
        Clock,
        Loader2,
        CheckCircle2,
        Euro,
        ArrowRight,
        X,
        FileCode2,
        Shield,
        Boxes,
    } from "@lucide/svelte";
    import BudgetSheet from "$lib/components/BudgetSheet.svelte";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    interface Scenario {
        id?: string;
        name: string;
        description: string;
        projectionMonths: number;
        remainderOrder: string[];
        isActive: boolean;
        monthStartDay: number;
        startDate: string;
        entities: {
            entityId: string;
            entityType: string;
            versionId: string;
        }[];
        simulations: number;
        simYears: number;
        simPercent: number;
        lookbackYears: number;
        mcImplementation: string;
        passiveIncomePercentage: number;
    }

    let scenarios = $state<Scenario[]>([]);
    let activeScenario = $state<Scenario | null>(null);
    let isLoading = $state(true);
    let isSaving = $state(false);
    let projectionResult = $state<any>({ months: [], simulated_yields: {} });
    let isProjecting = $state(false);

    let selectedMonthIndex = $state<number | null>(null);

    let scenarioToDelete = $state<string | null>(null);
    let showDeleteConfirm = $state(false);

    let allAssets = $state<any[]>([]);
    let allLoans = $state<any[]>([]);
    let allIncomes = $state<any[]>([]);
    let allBills = $state<any[]>([]);
    let allExpenses = $state<any[]>([]);

    function formatCurrency(val: number) {
        if (val === undefined || val === null) return "0,00";
        return val.toLocaleString("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2,
        });
    }

    function formatDate(dateStr: string) {
        if (!dateStr) return "";
        const d = new Date(dateStr);
        const month = String(d.getMonth() + 1).padStart(2, "0");
        const year = d.getFullYear();
        return `${month} / ${year}`;
    }

    function formatDateLong(dateStr: string) {
        if (!dateStr) return "";
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            month: "long",
            year: "numeric",
        });
    }

    function handleKeyDown(event: KeyboardEvent) {
        if (selectedMonthIndex === null) return;
        if (event.key === "ArrowLeft") {
            if (selectedMonthIndex > 0) {
                selectedMonthIndex--;
            }
        } else if (event.key === "ArrowRight") {
            if (selectedMonthIndex < projectionResult.months.length - 1) {
                selectedMonthIndex++;
            }
        } else if (event.key === "Escape") {
            selectedMonthIndex = null;
        }
    }

    async function fetchData() {
        isLoading = true;
        try {
            const [resp, err] = await wsCall("scenarios::list", null, null, [
                ScenarioListSchema,
            ]).one();
            if (err) throw err;
            scenarios = resp ? resp.scenarios : [];
            if (scenarios.length > 0 && !activeScenario) {
                activeScenario = scenarios[0];
                runProjection(activeScenario.id!);
            }
        } catch (e) {
            console.error("Failed to load scenarios:", e);
        } finally {
            isLoading = false;
        }
    }

    async function fetchEntities() {
        try {
            const [assetsRes, loansRes, incomesRes, billsRes, expensesRes] =
                await Promise.all([
                    wsCall("assets::list", null, null, [AssetListSchema]).one(),
                    wsCall("loans::list", null, null, [LoanListSchema]).one(),
                    wsCall("incomes::list", null, null, [
                        IncomeListSchema,
                    ]).one(),
                    wsCall("bills::list", null, null, [BillListSchema]).one(),
                    wsCall("expenses::list", null, null, [
                        ExpenseListSchema,
                    ]).one(),
                ]);
            allAssets = assetsRes[0] ? assetsRes[0].assets : [];
            allLoans = loansRes[0] ? loansRes[0].loans : [];
            allIncomes = incomesRes[0] ? incomesRes[0].incomes : [];
            allBills = billsRes[0] ? billsRes[0].bills : [];
            allExpenses = expensesRes[0] ? expensesRes[0].expenses : [];
        } catch (e) {
            console.error("Failed to load entities:", e);
        }
    }

    let streamCancel: (() => void) | null = null;

    async function runProjection(id: string) {
        if (streamCancel) streamCancel();
        isProjecting = true;
        projectionResult = { months: [], simulated_yields: {} };
        selectedMonthIndex = null;

        const callResult = wsCall(
            "scenarios::projection",
            ScenarioSchema,
            { id },
            [
                ProjectionMonthSchema,
                YieldMapSchema,
                PerformanceMetricsSchema,
                ErrorSchema,
            ],
        );

        let isCancelled = false;
        streamCancel = () => {
            isCancelled = true;
        };

        (async () => {
            try {
                // The projection stream sends multiple message types
                for await (const [message, error] of callResult.many()) {
                    if (isCancelled) break;
                    if (error) {
                        console.error("Projection stream error:", error);
                        break;
                    }
                    if (message) {
                        const typeName = (message as any).$typeName;
                        if (typeName === "api.ProjectionMonth") {
                            projectionResult.months = [
                                ...projectionResult.months,
                                message,
                            ];
                        } else if (typeName === "api.YieldMap") {
                            projectionResult.simulated_yields =
                                (message as any).yields || {};
                        } else if (typeName === "api.PerformanceMetrics") {
                            projectionResult.metrics = message;
                        }
                    }
                }
            } finally {
                if (!isCancelled) isProjecting = false;
                streamCancel = null;
            }
        })();
    }

    async function saveScenario() {
        if (!activeScenario) return;
        try {
            isSaving = true;
            const [saved, err] = await wsCall(
                "scenarios::save",
                ScenarioSchema,
                {
                    id: activeScenario.id || "",
                    name: activeScenario.name,
                    description: activeScenario.description,
                    projectionMonths: activeScenario.projectionMonths,
                    remainderOrder: activeScenario.remainderOrder,
                    isActive: activeScenario.isActive,
                    monthStartDay: activeScenario.monthStartDay,
                    startDate: activeScenario.startDate,
                    entities: activeScenario.entities.map((e) => ({
                        entityId: e.entityId,
                        entityType: e.entityType,
                        versionId: e.versionId,
                    })),
                    simulations: activeScenario.simulations,
                    simYears: activeScenario.simYears,
                    simPercent: activeScenario.simPercent,
                    lookbackYears: activeScenario.lookbackYears,
                    mcImplementation: activeScenario.mcImplementation,
                    passiveIncomePercentage:
                        activeScenario.passiveIncomePercentage,
                },
                [ScenarioSchema],
            ).one();
            if (err) throw err;

            if (saved) {
                const index = scenarios.findIndex((s) => s.id === saved.id);
                if (index !== -1) {
                    scenarios[index] = saved;
                } else {
                    scenarios.push(saved);
                }
                activeScenario = saved;
                runProjection(saved.id!);
            }
        } catch (e) {
            console.error("Failed to save scenario:", e);
        } finally {
            isSaving = false;
        }
    }

    async function forkScenario(s: Scenario) {
        try {
            const [forked, err] = await wsCall(
                "scenarios::save",
                ScenarioSchema,
                {
                    id: crypto.randomUUID(),
                    name: `${s.name} (Fork)`,
                    description: s.description,
                    projectionMonths: s.projectionMonths,
                    remainderOrder: s.remainderOrder,
                    isActive: s.isActive,
                    monthStartDay: s.monthStartDay,
                    startDate: s.startDate,
                    entities: s.entities.map((e) => ({
                        entityId: e.entityId,
                        entityType: e.entityType,
                        versionId: e.versionId,
                    })),
                    simulations: s.simulations,
                    simYears: s.simYears,
                    simPercent: s.simPercent,
                    lookbackYears: s.lookbackYears,
                    mcImplementation: s.mcImplementation,
                    passiveIncomePercentage: s.passiveIncomePercentage,
                },
                [ScenarioSchema],
            ).one();
            if (err) throw err;

            if (forked) {
                scenarios.push(forked);
                activeScenario = forked;
                runProjection(forked.id!);
            }
        } catch (e) {
            console.error("Failed to fork scenario:", e);
        }
    }

    async function deleteScenario() {
        if (!scenarioToDelete) return;
        try {
            const [, err] = await wsCall(
                "scenarios::delete",
                GenericIDSchema,
                { id: scenarioToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            scenarios = scenarios.filter((s) => s.id !== scenarioToDelete);
            if (activeScenario?.id === scenarioToDelete) {
                activeScenario = scenarios.length > 0 ? scenarios[0] : null;
            }
            showDeleteConfirm = false;
            scenarioToDelete = null;
        } catch (e) {
            console.error("Failed to delete scenario:", e);
        }
    }

    onMount(() => {
        fetchData();
        fetchEntities();
    });
</script>

<div class="max-w-[1440px] mx-auto p-4 md:p-8 space-y-8 min-h-screen">
    <!-- Header -->
    <div
        class="flex flex-col md:flex-row md:items-center justify-between gap-6"
    >
        <div class="space-y-1">
            <div class="flex items-center gap-2">
                <div class="p-2 bg-indigo-600 rounded-xl">
                    <Layers class="w-6 h-6 text-white" />
                </div>
                <h2 class="text-3xl font-black text-slate-900 tracking-tight">
                    Scenario Architect
                </h2>
            </div>
            <p
                class="text-sm text-slate-400 font-bold uppercase tracking-widest ml-1"
            >
                Deterministic & Probabilistic Projection Hub
            </p>
        </div>

        <button
            onclick={() => {
                activeScenario = {
                    name: "New Scenario",
                    description: "A fresh projection model",
                    projectionMonths: 120,
                    remainderOrder: [],
                    isActive: false,
                    monthStartDay: 1,
                    startDate:
                        new Date().toISOString().substring(0, 7) +
                        "-01T00:00:00Z",
                    entities: [],
                    simulations: 1000,
                    simYears: 10,
                    simPercent: 50,
                    lookbackYears: 20,
                    mcImplementation: "STANDARD",
                    passiveIncomePercentage: 100,
                };
            }}
            class="btn-primary flex items-center gap-2"
        >
            <Plus class="w-4 h-4" />
            New Simulation
        </button>
    </div>

    {#if isLoading}
        <div
            class="glass-card p-20 flex flex-col items-center justify-center space-y-4"
        >
            <Loader2 class="w-10 h-10 text-indigo-600 animate-spin" />
            <p
                class="text-sm font-black text-slate-400 uppercase tracking-widest"
            >
                Hydrating Scenario Models...
            </p>
        </div>
    {:else}
        <div class="grid grid-cols-1 lg:grid-cols-12 gap-8 items-start">
            <!-- Sidebar -->
            <div class="lg:col-span-3 space-y-6">
                <div class="glass-card p-6 space-y-4">
                    <span
                        class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                        >Simulations Registry</span
                    >
                    <div class="space-y-3">
                        {#each scenarios as s}
                            <button
                                onclick={() => {
                                    activeScenario = s;
                                    runProjection(s.id!);
                                }}
                                class="w-full text-left p-4 rounded-2xl border transition-all relative overflow-hidden group
                                    {activeScenario?.id === s.id
                                    ? 'bg-indigo-600 border-indigo-600 text-white shadow-xl'
                                    : 'bg-white border-slate-100 hover:border-indigo-200'}"
                            >
                                <h4 class="font-black text-sm tracking-tight">
                                    {s.name}
                                </h4>
                                <div class="flex items-center gap-2 mt-2">
                                    <span
                                        class="text-[9px] font-black uppercase {activeScenario?.id ===
                                        s.id
                                            ? 'text-indigo-200'
                                            : 'text-slate-400'}"
                                    >
                                        {s.projectionMonths / 12} Years
                                    </span>
                                </div>
                            </button>
                        {/each}
                    </div>
                </div>
            </div>

            <!-- Main Panel -->
            <div class="lg:col-span-9 space-y-8">
                {#if activeScenario}
                    <div class="glass-card p-8 space-y-8" transition:fade>
                        <div class="flex items-center justify-between">
                            <input
                                bind:value={activeScenario.name}
                                class="text-2xl font-black text-slate-900 bg-transparent border-none focus:ring-0 p-0 w-full"
                            />
                            <div class="flex gap-2">
                                <button
                                    onclick={saveScenario}
                                    disabled={isSaving}
                                    class="btn-primary"
                                >
                                    {isSaving ? "Saving..." : "Commit Model"}
                                </button>
                            </div>
                        </div>

                        <!-- Budget Sheet Summary View -->
                        <div class="space-y-4">
                            <div class="flex items-center gap-2">
                                <Activity class="w-4 h-4 text-indigo-600" />
                                <span
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-600"
                                    >Deterministic Projection View</span
                                >
                            </div>
                            {#if isProjecting && projectionResult.months.length === 0}
                                <div
                                    class="h-[400px] flex flex-col items-center justify-center bg-slate-50 rounded-3xl border border-dashed border-slate-200"
                                >
                                    <Loader2
                                        class="w-8 h-8 text-indigo-600 animate-spin mb-4"
                                    />
                                    <p
                                        class="text-[10px] font-black text-slate-400 uppercase tracking-widest"
                                    >
                                        Running Simulation Engines...
                                    </p>
                                </div>
                            {:else}
                                <div class="overflow-hidden border border-slate-100 rounded-3xl bg-white shadow-sm">
                                    <div class="overflow-x-auto">
                                        <table class="w-full text-left border-collapse table-fixed min-w-[1100px]">
                                            <colgroup>
                                                <col class="w-[10%]" />
                                                <col class="w-[11%]" />
                                                <col class="w-[11%]" />
                                                <col class="w-[11%]" />
                                                <col class="w-[11%]" />
                                                <col class="w-[11%]" />
                                                <col class="w-[11%]" />
                                                <col class="w-[12%]" />
                                                <col class="w-[12%]" />
                                            </colgroup>
                                            <thead>
                                                <tr class="bg-slate-50 border-b border-slate-100 text-[10px] font-black uppercase tracking-[0.1em] text-slate-400">
                                                    <th class="px-6 py-4">Month/Year</th>
                                                    <th class="px-4 py-4 text-right">Incoming Money</th>
                                                    <th class="px-4 py-4 text-right">Asset Outgoing</th>
                                                    <th class="px-4 py-4 text-right">Loans Outgoing</th>
                                                    <th class="px-4 py-4 text-right">Bills Outgoing</th>
                                                    <th class="px-4 py-4 text-right">Expenses Outgoing</th>
                                                    <th class="px-4 py-4 text-right">Net Remainder</th>
                                                    <th class="px-4 py-4 text-right">Loan Balances Acc.</th>
                                                    <th class="px-6 py-4 text-right">Net Worth</th>
                                                </tr>
                                            </thead>
                                            <tbody class="divide-y divide-slate-100 text-xs font-bold text-slate-600">
                                                {#each projectionResult.months as month, index}
                                                    <tr
                                                        onclick={() => selectedMonthIndex = index}
                                                        class="hover:bg-indigo-50/30 active:bg-indigo-50/60 transition-all cursor-pointer group"
                                                    >
                                                        <td class="px-6 py-4 text-slate-950 font-black">
                                                            <span class="group-hover:text-indigo-600 transition-colors">
                                                                {formatDate(month.date)}
                                                            </span>
                                                        </td>
                                                        <td class="px-4 py-4 text-right text-emerald-600 font-extrabold">
                                                            + {formatCurrency(month.income)} €
                                                        </td>
                                                        <td class="px-4 py-4 text-right text-slate-500">
                                                            - {formatCurrency(month.assets)} €
                                                        </td>
                                                        <td class="px-4 py-4 text-right text-rose-500">
                                                            - {formatCurrency(month.loans)} €
                                                        </td>
                                                        <td class="px-4 py-4 text-right text-slate-500">
                                                            - {formatCurrency(month.bills)} €
                                                        </td>
                                                        <td class="px-4 py-4 text-right text-slate-500">
                                                            - {formatCurrency(month.expenses)} €
                                                        </td>
                                                        <td class="px-4 py-4 text-right {month.remainder >= 0 ? 'text-emerald-600' : 'text-rose-600'} font-extrabold">
                                                            {month.remainder >= 0 ? '+' : ''}{formatCurrency(month.remainder)} €
                                                        </td>
                                                        <td class="px-4 py-4 text-right text-rose-600 font-extrabold">
                                                            {formatCurrency(month.loanDebt)} €
                                                        </td>
                                                        <td class="px-6 py-4 text-right text-indigo-600 font-black">
                                                            {formatCurrency(month.assetWorth)} €
                                                        </td>
                                                    </tr>
                                                {/each}
                                            </tbody>
                                        </table>
                                    </div>
                                </div>
                            {/if}
                        </div>
                    </div>
                {:else}
                    <div
                        class="glass-card p-12 text-center flex flex-col items-center justify-center space-y-4"
                    >
                        <Boxes class="w-12 h-12 text-slate-200" />
                        <p
                            class="text-sm font-black text-slate-400 uppercase tracking-widest"
                        >
                            Select or create a scenario to begin architecture
                        </p>
                    </div>
                {/if}
            </div>
        </div>
    {/if}
</div>

<!-- Keydown Listener for arrow navigation -->
<svelte:window onkeydown={handleKeyDown} />

<!-- Ultra-Premium Detailed Budget Sheet Modal Overlay -->
{#if selectedMonthIndex !== null && projectionResult.months[selectedMonthIndex]}
    {@const month = projectionResult.months[selectedMonthIndex]}
    <div
        class="fixed inset-0 bg-slate-900/60 backdrop-blur-md z-50 flex items-center justify-center p-4 md:p-8"
        transition:fade={{ duration: 150 }}
    >
        <!-- Modal Container -->
        <div
            class="bg-white rounded-[32px] shadow-2xl border border-slate-100 max-w-6xl w-full max-h-[90vh] flex flex-col relative overflow-hidden"
            transition:slide={{ duration: 200 }}
        >
            <!-- Modal Header -->
            <div class="px-8 py-5 bg-slate-50 border-b border-slate-100 flex items-center justify-between">
                <div class="flex items-center gap-4">
                    <div class="p-2.5 bg-indigo-600/10 text-indigo-600 rounded-xl">
                        <Activity class="w-5 h-5" />
                    </div>
                    <div>
                        <h3 class="text-lg font-black text-slate-900 leading-none">
                            {formatDateLong(month.date)}
                        </h3>
                        <p class="text-[10px] text-slate-400 font-bold uppercase tracking-widest mt-1">
                            Use Left/Right arrows or keys to navigate
                        </p>
                    </div>
                </div>

                <!-- Navigation and Close Actions -->
                <div class="flex items-center gap-3">
                    <!-- Prev Button -->
                    <button
                        onclick={() => {
                            if (selectedMonthIndex! > 0) selectedMonthIndex!--;
                        }}
                        disabled={selectedMonthIndex === 0}
                        class="p-2.5 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 text-slate-600 disabled:opacity-30 disabled:hover:bg-white transition-all cursor-pointer disabled:cursor-not-allowed"
                        title="Previous Month (Left Arrow)"
                    >
                        <ChevronLeft class="w-4 h-4" />
                    </button>

                    <!-- Next Button -->
                    <button
                        onclick={() => {
                            if (selectedMonthIndex! < projectionResult.months.length - 1) selectedMonthIndex!++;
                        }}
                        disabled={selectedMonthIndex === projectionResult.months.length - 1}
                        class="p-2.5 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 text-slate-600 disabled:opacity-30 disabled:hover:bg-white transition-all cursor-pointer disabled:cursor-not-allowed"
                        title="Next Month (Right Arrow)"
                    >
                        <ChevronRight class="w-4 h-4" />
                    </button>

                    <div class="w-px h-6 bg-slate-200 mx-1"></div>

                    <!-- Close Button -->
                    <button
                        onclick={() => selectedMonthIndex = null}
                        class="p-2.5 rounded-xl border border-slate-200 bg-white hover:bg-slate-50 hover:text-rose-600 text-slate-600 transition-all cursor-pointer"
                        title="Close Modal (ESC)"
                    >
                        <X class="w-4 h-4" />
                    </button>
                </div>
            </div>

            <!-- Scrollable Modal Content -->
            <div class="flex-1 overflow-y-auto p-8 space-y-6 bg-slate-50/30">
                <BudgetSheet
                    date={month.date}
                    breakdown={month.breakdown}
                    totalIncome={month.income}
                    totalBills={month.bills}
                    totalExpenses={month.expenses}
                    totalAssets={month.assets}
                    totalLoans={month.loans}
                    remainder={month.remainder}
                    virtualAccounts={month.virtualAccounts}
                />
            </div>
        </div>
    </div>
{/if}

<style>
    @reference "../../app.css";
    .glass-card {
        @apply bg-white border border-slate-100 rounded-[32px] shadow-sm transition-all duration-500;
    }

    .btn-primary {
        @apply px-6 py-3 bg-slate-900 text-white rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] hover:bg-indigo-600 transition-all active:scale-95 disabled:opacity-50;
    }
</style>
