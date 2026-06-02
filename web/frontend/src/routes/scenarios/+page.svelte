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
        ChevronUp,
        ChevronDown,
        Waves,
        Activity,
        TrendingUp,
        Clock,
        Loader2,
        Zap,
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
    let showConfig = $state(false);
    let showScopeModal = $state(false);

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
                runProjection(activeScenario!);
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

    async function runProjection(scenario: Scenario) {
        if (streamCancel) streamCancel();
        isProjecting = true;
        projectionResult = { months: [], simulated_yields: {} };
        selectedMonthIndex = null;

        const callResult = wsCall(
            "scenarios::projection",
            ScenarioSchema,
            scenario,
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
                let batchedMonths: any[] = [];
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
                            batchedMonths.push(message);

                            // Batch update every 24 months to keep UI alive but reduce re-renders
                            if (batchedMonths.length >= 24) {
                                projectionResult.months = [
                                    ...projectionResult.months,
                                    ...batchedMonths,
                                ];
                                batchedMonths = [];
                            }
                        } else if (typeName === "api.YieldMap") {
                            console.log("Received Simulated Yields:", message);
                            projectionResult.simulated_yields = { ...((message as any).yields || {}) };
                        } else if (typeName === "api.PerformanceMetrics") {
                            projectionResult.metrics = message;
                        }
                    }
                }

                // Final flush
                if (batchedMonths.length > 0 && !isCancelled) {
                    projectionResult.months = [
                        ...projectionResult.months,
                        ...batchedMonths,
                    ];
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
                    etfParams: Object.fromEntries(
                        Object.entries(activeScenario.etfParams || {}).map(([k, v]) => [
                            k,
                            {
                                simulations: Number(v.simulations),
                                simYears: Number(v.simYears),
                                simPercent: Number(v.simPercent),
                                lookbackYears: Number(v.lookbackYears),
                            }
                        ])
                    ),
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
                runProjection(saved);
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
                    etfParams: Object.fromEntries(
                        Object.entries(s.etfParams || {}).map(([k, v]) => [
                            k,
                            {
                                simulations: Number(v.simulations),
                                simYears: Number(v.simYears),
                                simPercent: Number(v.simPercent),
                                lookbackYears: Number(v.lookbackYears),
                            }
                        ])
                    ),
                },
                [ScenarioSchema],
            ).one();
            if (err) throw err;

            if (forked) {
                scenarios.push(forked);
                activeScenario = forked;
                runProjection(forked);
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

    function moveInRemainderOrder(index: number, direction: "up" | "down") {
        if (!activeScenario) return;
        const newOrder = [...activeScenario.remainderOrder];
        const targetIndex = direction === "up" ? index - 1 : index + 1;
        if (targetIndex < 0 || targetIndex >= newOrder.length) return;

        const [removed] = newOrder.splice(index, 1);
        newOrder.splice(targetIndex, 0, removed);
        activeScenario.remainderOrder = newOrder;
    }

    function toggleInRemainderOrder(id: string) {
        if (!activeScenario) return;
        if (activeScenario.remainderOrder.includes(id)) {
            activeScenario.remainderOrder = activeScenario.remainderOrder.filter(
                (item) => item !== id,
            );
        } else {
            activeScenario.remainderOrder = [
                ...activeScenario.remainderOrder,
                id,
            ];
        }
    }

    onMount(() => {
        fetchData();
        fetchEntities();
    });
</script>

<svelte:head>
    <title>Scenarios &amp; Playbooks — BudgetScript</title>
</svelte:head>

<div class="space-y-12">
    <!-- Header -->
    <header
        class="flex flex-col md:flex-row md:items-end justify-between gap-6"
    >
        <div class="space-y-2">
            <h1 class="text-5xl font-black tracking-tight text-slate-900">
                Scenario <span class="gradient-text">Architect</span>.
            </h1>
            <p class="text-slate-500 font-medium text-lg">
                Deterministic & probabilistic projection hub.
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
                    etfParams: {},
                };
            }}
            class="btn-primary flex items-center gap-2"
        >
            <Plus class="w-4 h-4" />
            New Simulation
        </button>
    </header>

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
                                    runProjection(s);
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
                            <div class="space-y-1 flex-1">
                                <input
                                    bind:value={activeScenario.name}
                                    class="text-2xl font-black text-slate-900 bg-transparent border-none focus:ring-0 p-0 w-full"
                                />
                                {#if projectionResult.months.length > 0}
                                    {@const lastMonth = projectionResult.months[projectionResult.months.length - 1]}
                                    <div class="flex items-center gap-4 animate-fade-in">
                                        <div class="flex items-center gap-1.5">
                                            <TrendingUp class="w-3 h-3 text-indigo-500" />
                                            <span class="text-[10px] font-black uppercase tracking-wider text-slate-400">Final Worth:</span>
                                            <span class="text-[11px] font-black text-indigo-600">{formatCurrency(lastMonth.assetWorth)} €</span>
                                        </div>
                                        <div class="flex items-center gap-1.5">
                                            <Euro class="w-3 h-3 text-emerald-500" />
                                            <span class="text-[10px] font-black uppercase tracking-wider text-slate-400">Avg. Remainder:</span>
                                            <span class="text-[11px] font-black text-emerald-600">
                                                {formatCurrency(projectionResult.months.reduce((acc: number, m: any) => acc + m.remainder, 0) / projectionResult.months.length)} €
                                            </span>
                                        </div>
                                        {#if projectionResult.simulated_yields && Object.keys(projectionResult.simulated_yields).length > 0}
                                            {@const yields = Object.entries(projectionResult.simulated_yields).filter(([k, v]) => !k.includes('_') && typeof v === 'number').map(([k, v]) => v) as number[]}
                                            {#if yields.length > 0}
                                                <div class="flex items-center gap-1.5">
                                                    <TrendingUp class="w-3 h-3 text-emerald-600" />
                                                    <span class="text-[10px] font-black uppercase tracking-wider text-slate-400">Sim. Yield:</span>
                                                    <span class="text-[11px] font-black text-emerald-600">
                                                        {(yields.reduce((a, b) => a + b, 0) / yields.length).toFixed(2)}%
                                                    </span>
                                                </div>
                                            {/if}
                                        {/if}
                                        {#if projectionResult.metrics}
                                            <div class="flex items-center gap-1.5">
                                                <Zap class="w-3 h-3 text-amber-500" />
                                                <span class="text-[10px] font-black uppercase tracking-wider text-slate-400">Engine:</span>
                                                <span class="text-[11px] font-black text-amber-600">{projectionResult.metrics.totalDurationMs}ms</span>
                                            </div>
                                        {/if}
                                    </div>
                                {/if}
                            </div>
                             <div class="flex gap-2 self-start">
                                 <button
                                     onclick={() => showScopeModal = true}
                                     class="px-5 py-3 border border-slate-200 text-slate-700 bg-white hover:bg-slate-50 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all cursor-pointer flex items-center gap-2"
                                 >
                                     Logic Scope
                                 </button>
                                 <button
                                     onclick={saveScenario}
                                     disabled={isSaving}
                                     class="btn-primary"
                                 >
                                     {isSaving ? "Saving..." : "Commit Model"}
                                 </button>
                             </div>
                        </div>

                        <!-- Scenario Configuration Controls -->
                        <div class="bg-slate-50 border border-slate-100 rounded-3xl p-6 space-y-6">
                            <div class="flex items-center justify-between cursor-pointer" onclick={() => showConfig = !showConfig}>
                                <div class="flex items-center gap-2.5">
                                    <div class="p-2 bg-indigo-600/10 text-indigo-600 rounded-xl">
                                        <Settings2 class="w-4 h-4" />
                                    </div>
                                    <div>
                                        <h4 class="text-xs font-black uppercase tracking-[0.2em] text-slate-900">
                                            Scenario & Simulation Configuration
                                        </h4>
                                        <p class="text-[10px] text-slate-400 font-semibold uppercase">
                                            Configure projection, Monte Carlo horizon, and engine settings
                                        </p>
                                    </div>
                                </div>
                                <button class="text-xs font-black uppercase tracking-wider text-indigo-600 hover:text-indigo-700 cursor-pointer">
                                    {showConfig ? 'Hide Settings' : 'Configure Model'}
                                </button>
                            </div>

                            {#if showConfig}
                                <div class="grid grid-cols-1 md:grid-cols-3 gap-6 pt-4 border-t border-slate-200/50 animate-fade-in" transition:slide>
                                    <!-- Row 1: Basics -->
                                    <div class="space-y-2 md:col-span-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Description</label>
                                        <input
                                            type="text"
                                            bind:value={activeScenario.description}
                                            placeholder="Enter scenario details..."
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Start Date</label>
                                        <input
                                            type="date"
                                            value={activeScenario.startDate ? activeScenario.startDate.substring(0, 10) : ''}
                                            onchange={(e) => {
                                                const val = e.currentTarget.value;
                                                if (val) activeScenario.startDate = val + 'T00:00:00Z';
                                            }}
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>

                                    <!-- Row 2: Projection & SWR -->
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Projection Period (Months)</label>
                                        <input
                                            type="number"
                                            bind:value={activeScenario.projectionMonths}
                                            min="1"
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Month Start Day</label>
                                        <input
                                            type="number"
                                            bind:value={activeScenario.monthStartDay}
                                            min="1"
                                            max="28"
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Passive Income SWR %</label>
                                        <input
                                            type="number"
                                            step="0.1"
                                            bind:value={activeScenario.passiveIncomePercentage}
                                            min="0"
                                            max="100"
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>

                                    <!-- Row 3: Monte Carlo Core Parameters -->
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Monte Carlo Simulations</label>
                                        <input
                                            type="number"
                                            bind:value={activeScenario.simulations}
                                            min="10"
                                            max="100000"
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Simulation Horizon (Years)</label>
                                        <input
                                            type="number"
                                            bind:value={activeScenario.simYears}
                                            min="1"
                                            max="50"
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Confidence Percentile (%)</label>
                                        <input
                                            type="number"
                                            bind:value={activeScenario.simPercent}
                                            min="1"
                                            max="99"
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>

                                    <!-- Row 4: Historical & Engine -->
                                    <div class="space-y-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Lookback Horizon (Years)</label>
                                        <input
                                            type="number"
                                            bind:value={activeScenario.lookbackYears}
                                            min="0"
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                        />
                                    </div>
                                    <div class="space-y-2 md:col-span-2">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Monte Carlo Engine Core</label>
                                        <select
                                            bind:value={activeScenario.mcImplementation}
                                            class="block w-full px-4 py-2.5 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs appearance-none cursor-pointer"
                                        >
                                            <option value="STANDARD">Standard Engine (Scalar Single-Threaded)</option>
                                            <option value="PARALLEL">Parallel Engine (Multi-Threaded Work Pool)</option>
                                            <option value="SIMD">SIMD Vectorized Engine (Hardware Accelerated)</option>
                                        </select>
                                    </div>

                                    <!-- Per-ETF Parameter Overrides Section -->
                                    {#if allAssets.filter(a => a.activeVersion?.type === 'ETF').length > 0}
                                        {@const etfAssets = allAssets.filter(a => a.activeVersion?.type === 'ETF')}
                                        <div class="md:col-span-3 space-y-4 pt-6 border-t border-slate-200/50">
                                            <h5 class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">
                                                Per-ETF Asset Parameter Overrides
                                            </h5>
                                            <div class="space-y-4">
                                                {#each etfAssets as asset}
                                                    {@const hasOverride = activeScenario.etfParams && activeScenario.etfParams[asset.id] !== undefined}
                                                    <div class="bg-white border rounded-2xl p-5 transition-all duration-300 space-y-4
                                                        {hasOverride ? 'border-indigo-100 shadow-sm bg-indigo-50/5' : 'border-slate-100'}"
                                                    >
                                                        <div class="flex items-center justify-between">
                                                            <label class="flex items-center gap-3 font-bold text-slate-800 text-xs cursor-pointer select-none">
                                                                <input
                                                                    type="checkbox"
                                                                    checked={hasOverride}
                                                                    onchange={() => {
                                                                        if (!activeScenario.etfParams) {
                                                                            activeScenario.etfParams = {};
                                                                        }
                                                                        if (hasOverride) {
                                                                            const updated = { ...activeScenario.etfParams };
                                                                            delete updated[asset.id];
                                                                            activeScenario.etfParams = updated;
                                                                        } else {
                                                                            activeScenario.etfParams = {
                                                                                ...activeScenario.etfParams,
                                                                                [asset.id]: {
                                                                                    simulations: activeScenario.simulations || 50000,
                                                                                    simYears: activeScenario.simYears || 10,
                                                                                    simPercent: activeScenario.simPercent || 50,
                                                                                    lookbackYears: activeScenario.lookbackYears || 0
                                                                                }
                                                                            };
                                                                        }
                                                                    }}
                                                                    class="rounded text-indigo-600 focus:ring-indigo-500/20 w-4 h-4 cursor-pointer"
                                                                />
                                                                Customize settings for {asset.name}
                                                            </label>

                                                            <span class="px-2.5 py-1 rounded-full text-[9px] font-black uppercase tracking-wider
                                                                {hasOverride
                                                                ? 'bg-indigo-50 text-indigo-700 border border-indigo-100'
                                                                : 'bg-slate-50 text-slate-500 border border-slate-200/60'}"
                                                            >
                                                                {hasOverride ? 'Override Active' : 'Using Defaults'}
                                                            </span>
                                                        </div>

                                                        {#if hasOverride && activeScenario.etfParams[asset.id]}
                                                            <div class="grid grid-cols-2 md:grid-cols-4 gap-4 pt-3 border-t border-slate-100 animate-fade-in">
                                                                <div class="space-y-1.5">
                                                                    <label class="text-[8px] font-black uppercase tracking-wider text-slate-400">Simulations</label>
                                                                    <input
                                                                        type="number"
                                                                        bind:value={activeScenario.etfParams[asset.id].simulations}
                                                                        min="10"
                                                                        max="100000"
                                                                        class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                                                    />
                                                                </div>
                                                                <div class="space-y-1.5">
                                                                    <label class="text-[8px] font-black uppercase tracking-wider text-slate-400">Time Horizon (Years)</label>
                                                                    <input
                                                                        type="number"
                                                                        bind:value={activeScenario.etfParams[asset.id].simYears}
                                                                        min="1"
                                                                        max="50"
                                                                        class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                                                    />
                                                                </div>
                                                                <div class="space-y-1.5">
                                                                    <label class="text-[8px] font-black uppercase tracking-wider text-slate-400">Percentile Pick (%)</label>
                                                                    <input
                                                                        type="number"
                                                                        bind:value={activeScenario.etfParams[asset.id].simPercent}
                                                                        min="1"
                                                                        max="99"
                                                                        class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                                                    />
                                                                </div>
                                                                <div class="space-y-1.5">
                                                                    <label class="text-[8px] font-black uppercase tracking-wider text-slate-400">Hist. Lookback (Years)</label>
                                                                    <input
                                                                        type="number"
                                                                        bind:value={activeScenario.etfParams[asset.id].lookbackYears}
                                                                        min="0"
                                                                        class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold text-xs"
                                                                    />
                                                                </div>
                                                            </div>
                                                        {/if}
                                                    </div>
                                                {/each}
                                            </div>
                                        </div>
                                    {/if}
                                </div>
                            {/if}
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

                        <!-- Probabilistic Monte Carlo Results -->
                        {#if projectionResult.simulated_yields && Object.keys(projectionResult.simulated_yields).length > 0}
                            {@const etfAssets = allAssets.filter(a => a.activeVersion?.type === 'ETF')}
                            {#if etfAssets.length > 0}
                                <div class="space-y-4 pt-6 border-t border-slate-100">
                                    <div class="flex items-center gap-2">
                                        <TrendingUp class="w-4 h-4 text-emerald-600" />
                                        <span class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-600">
                                            Monte Carlo Simulation Results
                                        </span>
                                    </div>
                                    <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                                        {#each etfAssets as asset}
                                            {@const assetYield = projectionResult.simulated_yields[asset.id]}
                                            <div class="bg-white border border-slate-100 rounded-3xl p-6 shadow-sm space-y-6 hover:border-indigo-200/50 hover:shadow-md transition-all duration-300 relative overflow-hidden group">
                                                <div class="absolute top-0 left-0 w-full h-1 bg-gradient-to-r from-indigo-500 to-emerald-500"></div>
                                                
                                                <div class="flex items-start justify-between">
                                                    <div>
                                                        <h4 class="font-black text-slate-900 text-base">{asset.name}</h4>
                                                        <p class="text-[10px] text-slate-400 font-bold uppercase tracking-wider mt-0.5">ETF Portfolio Overall</p>
                                                    </div>
                                                    {#if assetYield !== undefined}
                                                        <div class="text-right">
                                                            <span class="text-xs text-slate-400 font-bold uppercase tracking-wider block">Simulated Yield</span>
                                                            <span class="text-lg font-black text-emerald-600">{assetYield.toFixed(2)}%</span>
                                                        </div>
                                                    {/if}
                                                </div>

                                                {#if asset.activeVersion?.etfConfig?.length > 0}
                                                    <div class="border-t border-slate-100 pt-4 space-y-3">
                                                        <h5 class="text-[10px] font-black uppercase tracking-wider text-slate-400">Tracker Breakdowns</h5>
                                                        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                                                            {#each asset.activeVersion.etfConfig as tracker}
                                                                {@const trackerYield = projectionResult.simulated_yields[`${asset.id}_${tracker.tracker}`]}
                                                                <div class="bg-slate-50 border border-slate-100 rounded-2xl p-4 space-y-2">
                                                                    <div class="flex items-start justify-between gap-2">
                                                                        <span class="font-extrabold text-slate-900 text-xs truncate" title={tracker.tracker}>
                                                                            {tracker.tracker || "Unnamed Tracker"}
                                                                        </span>
                                                                        <span class="text-[9px] font-black px-1.5 py-0.5 bg-indigo-50 text-indigo-700 rounded-md border border-indigo-100">
                                                                            {(tracker.percentage * 100).toFixed(0)}%
                                                                        </span>
                                                                    </div>
                                                                    <div class="flex items-baseline justify-between text-[10px] text-slate-500">
                                                                        <span>TER: {tracker.ter || 0}%</span>
                                                                        {#if trackerYield !== undefined}
                                                                            <span class="font-black text-emerald-600 text-xs">Yield: {trackerYield.toFixed(2)}%</span>
                                                                        {/if}
                                                                    </div>
                                                                </div>
                                                            {/each}
                                                        </div>
                                                    </div>
                                                {/if}
                                            </div>
                                        {/each}
                                    </div>
                                </div>
                            {/if}
                        {/if}
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
            class="bg-white dark-budget-modal rounded-[32px] shadow-2xl border border-slate-100 max-w-6xl w-full max-h-[90vh] flex flex-col relative overflow-hidden"
            transition:slide={{ duration: 200 }}
        >
            <!-- Modal Header -->
            <div class="px-8 py-5 bg-slate-50 dark-budget-modal-header border-b border-slate-100 flex items-center justify-between">
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
            <div class="flex-1 overflow-y-auto p-8 space-y-6 bg-slate-50/30 dark-budget-modal-content">
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

<!-- Logic Configuration Modal Overlay -->
{#if showScopeModal && activeScenario}
    <div
        class="fixed inset-0 bg-slate-900/60 backdrop-blur-md z-50 flex items-center justify-center p-4 md:p-8"
        transition:fade={{ duration: 150 }}
    >
        <!-- Modal Container -->
        <div
            class="bg-white dark-budget-modal rounded-[32px] shadow-2xl border border-slate-100 max-w-5xl w-full max-h-[85vh] p-10 relative overflow-hidden flex flex-col"
            transition:slide={{ duration: 200 }}
        >
            <!-- Close Button -->
            <button
                onclick={() => showScopeModal = false}
                class="absolute top-6 right-6 p-2 rounded-xl hover:bg-slate-100 text-slate-400 hover:text-slate-600 transition-all cursor-pointer"
            >
                <X class="w-5 h-5" />
            </button>

            <!-- Modal Header -->
            <div class="space-y-1 mb-8">
                <h3 class="text-2xl font-black text-slate-900 leading-none">
                    Logic Configuration
                </h3>
                <p class="text-xs text-slate-400 font-bold uppercase tracking-wider">
                    Fine-tune the deterministic scope.
                </p>
            </div>

            <!-- Modal Content - Scrollable Columns -->
            <div class="flex-1 overflow-y-auto pr-2 grid grid-cols-1 md:grid-cols-2 gap-8 min-h-0">
                <!-- Left Column: Incomes & Bills -->
                <div class="space-y-8">
                    <!-- INCOMES -->
                    <div class="space-y-4">
                        <div class="flex items-center gap-2 border-b border-slate-100 pb-2">
                            <span class="text-xs font-black uppercase tracking-[0.2em] text-slate-900">Included Incomes</span>
                        </div>
                        <div class="flex flex-wrap gap-2.5">
                            {#each allIncomes as inc}
                                {@const isIncluded = activeScenario.entities.some(e => e.entityId === inc.id && e.entityType === 'INCOME')}
                                <button
                                    onclick={() => {
                                        if (isIncluded) {
                                            activeScenario.entities = activeScenario.entities.filter(e => !(e.entityId === inc.id && e.entityType === 'INCOME'));
                                        } else {
                                            activeScenario.entities = [
                                                ...activeScenario.entities,
                                                { entityId: inc.id, entityType: 'INCOME', versionId: inc.activeVersion?.id || "" }
                                            ];
                                        }
                                    }}
                                    class="px-4 py-2 rounded-full border text-xs font-bold transition-all cursor-pointer
                                        {isIncluded
                                        ? 'bg-indigo-600 border-indigo-600 text-white font-extrabold shadow-sm'
                                        : 'bg-white border-slate-200 text-slate-600 hover:border-slate-300'}"
                                >
                                    {inc.name}
                                </button>
                            {/each}
                            {#if allIncomes.length === 0}
                                <span class="text-xs text-slate-400 font-semibold italic">No incomes found</span>
                            {/if}
                        </div>
                    </div>

                    <!-- BILLS -->
                    <div class="space-y-4">
                        <div class="flex items-center gap-2 border-b border-slate-100 pb-2">
                            <span class="text-xs font-black uppercase tracking-[0.2em] text-slate-900">Included Bills</span>
                        </div>
                        <div class="flex flex-wrap gap-2.5">
                            {#each allBills as bill}
                                {@const isIncluded = activeScenario.entities.some(e => e.entityId === bill.id && e.entityType === 'BILL')}
                                <button
                                    onclick={() => {
                                        if (isIncluded) {
                                            activeScenario.entities = activeScenario.entities.filter(e => !(e.entityId === bill.id && e.entityType === 'BILL'));
                                        } else {
                                            activeScenario.entities = [
                                                ...activeScenario.entities,
                                                { entityId: bill.id, entityType: 'BILL', versionId: bill.activeVersion?.id || "" }
                                            ];
                                        }
                                    }}
                                    class="px-4 py-2 rounded-full border text-xs font-bold transition-all cursor-pointer
                                        {isIncluded
                                        ? 'bg-indigo-600 border-indigo-600 text-white font-extrabold shadow-sm'
                                        : 'bg-white border-slate-200 text-slate-600 hover:border-slate-300'}"
                                >
                                    {bill.name}
                                </button>
                            {/each}
                            {#if allBills.length === 0}
                                <span class="text-xs text-slate-400 font-semibold italic">No bills found</span>
                            {/if}
                        </div>
                    </div>
                </div>

                <!-- Right Column: Assets & Loans -->
                <div class="space-y-8">
                    <!-- ASSETS -->
                    <div class="space-y-4">
                        <div class="flex items-center gap-2 border-b border-slate-100 pb-2">
                            <span class="text-xs font-black uppercase tracking-[0.2em] text-slate-900">Included Assets</span>
                        </div>
                        <div class="flex flex-wrap gap-2.5">
                            {#each allAssets as asset}
                                {@const isIncluded = activeScenario.entities.some(e => e.entityId === asset.id && e.entityType === 'ASSET')}
                                <button
                                    onclick={() => {
                                        if (isIncluded) {
                                            activeScenario.entities = activeScenario.entities.filter(e => !(e.entityId === asset.id && e.entityType === 'ASSET'));
                                        } else {
                                            activeScenario.entities = [
                                                ...activeScenario.entities,
                                                { entityId: asset.id, entityType: 'ASSET', versionId: asset.activeVersion?.id || "" }
                                            ];
                                        }
                                    }}
                                    class="px-4 py-2 rounded-full border text-xs font-bold transition-all cursor-pointer
                                        {isIncluded
                                        ? 'bg-indigo-600 border-indigo-600 text-white font-extrabold shadow-sm'
                                        : 'bg-white border-slate-200 text-slate-600 hover:border-slate-300'}"
                                >
                                    {asset.name}
                                </button>
                            {/each}
                            {#if allAssets.length === 0}
                                <span class="text-xs text-slate-400 font-semibold italic">No assets found</span>
                            {/if}
                        </div>
                    </div>

                    <!-- LOANS -->
                    <div class="space-y-4">
                        <div class="flex items-center gap-2 border-b border-slate-100 pb-2">
                            <span class="text-xs font-black uppercase tracking-[0.2em] text-slate-900">Included Loans</span>
                        </div>
                        <div class="flex flex-wrap gap-2.5">
                            {#each allLoans as loan}
                                {@const isIncluded = activeScenario.entities.some(e => e.entityId === loan.id && e.entityType === 'LOAN')}
                                <button
                                    onclick={() => {
                                        if (isIncluded) {
                                            activeScenario.entities = activeScenario.entities.filter(e => !(e.entityId === loan.id && e.entityType === 'LOAN'));
                                        } else {
                                            activeScenario.entities = [
                                                ...activeScenario.entities,
                                                { entityId: loan.id, entityType: 'LOAN', versionId: loan.activeVersion?.id || "" }
                                            ];
                                        }
                                    }}
                                    class="px-4 py-2 rounded-full border text-xs font-bold transition-all cursor-pointer
                                        {isIncluded
                                        ? 'bg-indigo-600 border-indigo-600 text-white font-extrabold shadow-sm'
                                        : 'bg-white border-slate-200 text-slate-600 hover:border-slate-300'}"
                                >
                                    {loan.name}
                                </button>
                            {/each}
                            {#if allLoans.length === 0}
                                <span class="text-xs text-slate-400 font-semibold italic">No loans found</span>
                            {/if}
                        </div>
                    </div>
                </div>

                <!-- Full Width: Remainder Waterfall -->
                <div class="md:col-span-2 space-y-6 pt-8 border-t border-slate-100">
                    <div class="space-y-1">
                        <span class="text-xs font-black uppercase tracking-[0.2em] text-slate-900">Remainder Waterfall Priority</span>
                        <p class="text-[10px] text-slate-400 font-bold uppercase tracking-wider">
                            Define the priority order for surplus funds distribution. Surpluses will flow into these entities sequentially until targets are met.
                        </p>
                    </div>

                    <div class="grid grid-cols-1 md:grid-cols-2 gap-8">
                        <!-- Available for Waterfall -->
                        <div class="space-y-4">
                            <span class="text-[10px] font-black uppercase tracking-[0.15em] text-slate-400">Available Entities</span>
                            <div class="flex flex-wrap gap-2.5">
                                {#each [...allAssets, ...allLoans].filter(entity => !activeScenario.remainderOrder.includes(entity.id)) as entity}
                                    <button
                                        onclick={() => toggleInRemainderOrder(entity.id)}
                                        class="px-4 py-2 rounded-xl border border-slate-200 bg-white text-slate-600 text-xs font-bold hover:border-indigo-200 hover:bg-indigo-50/30 transition-all cursor-pointer flex items-center gap-2"
                                    >
                                        <Plus class="w-3 h-3 text-slate-400" />
                                        {entity.name}
                                    </button>
                                {/each}
                            </div>
                        </div>

                        <!-- Active Waterfall Order -->
                        <div class="space-y-4">
                            <span class="text-[10px] font-black uppercase tracking-[0.15em] text-slate-400">Active Priority Order</span>
                            <div class="space-y-2">
                                {#each activeScenario.remainderOrder as entityId, i}
                                    {@const entity = [...allAssets, ...allLoans].find(e => e.id === entityId)}
                                    {#if entity}
                                        <div class="flex items-center gap-3 p-3 bg-slate-50 border border-slate-200 rounded-2xl group hover:border-indigo-200 transition-all">
                                            <div class="flex flex-col gap-1">
                                                <button
                                                    onclick={() => moveInRemainderOrder(i, 'up')}
                                                    disabled={i === 0}
                                                    class="p-0.5 hover:text-indigo-600 disabled:opacity-30 transition-colors"
                                                >
                                                    <ChevronUp class="w-3.5 h-3.5" />
                                                </button>
                                                <button
                                                    onclick={() => moveInRemainderOrder(i, 'down')}
                                                    disabled={i === activeScenario.remainderOrder.length - 1}
                                                    class="p-0.5 hover:text-indigo-600 disabled:opacity-30 transition-colors"
                                                >
                                                    <ChevronDown class="w-3.5 h-3.5" />
                                                </button>
                                            </div>
                                            
                                            <div class="w-6 h-6 rounded-lg bg-indigo-600 text-white flex items-center justify-center text-[10px] font-black">
                                                {i + 1}
                                            </div>

                                            <span class="flex-1 text-xs font-black text-slate-900 uppercase truncate">
                                                {entity.name}
                                            </span>

                                            <button
                                                onclick={() => toggleInRemainderOrder(entityId)}
                                                class="p-2 text-slate-400 hover:text-rose-600 transition-colors"
                                            >
                                                <X class="w-4 h-4" />
                                            </button>
                                        </div>
                                    {/if}
                                {/each}
                                {#if activeScenario.remainderOrder.length === 0}
                                    <div class="p-8 border-2 border-dashed border-slate-100 rounded-[32px] flex flex-col items-center justify-center space-y-2 opacity-50">
                                        <Activity class="w-6 h-6 text-slate-300" />
                                        <p class="text-[9px] font-black uppercase tracking-widest text-slate-400">Waterfall empty</p>
                                    </div>
                                {/if}
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            <!-- Footer Save/Close Actions -->
            <div class="mt-8 pt-4 border-t border-slate-100 flex justify-end">
                <button
                    onclick={() => showScopeModal = false}
                    class="btn-primary"
                >
                    Apply Scope Configuration
                </button>
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

    :global(.dark) .dark-budget-modal {
        background-color: #090d16 !important;
        border-color: rgba(51, 65, 85, 0.4) !important;
    }

    :global(.dark) .dark-budget-modal-header {
        background-color: #0f172a !important;
        border-color: rgba(51, 65, 85, 0.4) !important;
    }

    :global(.dark) .dark-budget-modal-content {
        background-color: #090d16 !important;
    }
</style>
