<script lang="ts">
    import { onMount } from "svelte";
    import { auth } from "$lib/stores/auth.svelte";

    import {
        LayoutDashboard,
        Activity,
        ArrowRight,
        Loader2,
        RefreshCw,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import BudgetSheet from "$lib/components/BudgetSheet.svelte";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import { wsCall } from "$lib/utils/ws_fetch";
    import { resolveActiveScenario, savePreferredScenario } from "$lib/utils/scenario";
    import {
        ScenarioListSchema,
        AuthSuccessResponseSchema,
        ScenarioSchema,
        ProjectionMonthSchema,
        YieldMapSchema,
        PerformanceMetricsSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    let scenarios = $state<any[]>([]);
    let selectedScenarioId = $state<string | null>(null);
    let isLoading = $state(true);

    let currentMonthSheet = $state<any>(null);
    let isProjecting = $state(false);

    async function loadCurrentMonthBudgetSheet(scenarioId: string) {
        isProjecting = true;
        currentMonthSheet = null;

        try {
            const scenario = scenarios.find((s) => s.id === scenarioId);
            const monthStartDay = scenario?.monthStartDay || 1;

            const now = new Date();
            let labelMonth = now.getMonth() + 1;
            let labelYear = now.getFullYear();

            // If we are past the month start day, the current budget period
            // is labeled as the next calendar month.
            if (now.getDate() >= monthStartDay && monthStartDay > 1) {
                labelMonth++;
                if (labelMonth > 12) {
                    labelMonth = 1;
                    labelYear++;
                }
            }

            const currentPrefix = `${labelYear}-${String(labelMonth).padStart(2, "0")}`;

            let projectionMonths = 1;
            if (scenario?.startDate) {
                const start = new Date(scenario.startDate);
                const diffYears = labelYear - start.getFullYear();
                const diffMonths = labelMonth - (start.getMonth() + 1);
                const totalDiff = diffYears * 12 + diffMonths;
                if (totalDiff > 0) {
                    projectionMonths = totalDiff + 1;
                }
            }

            const callResult = wsCall(
                "scenarios::projection",
                ScenarioSchema,
                { id: scenarioId, projectionMonths },
                [
                    ProjectionMonthSchema,
                    YieldMapSchema,
                    PerformanceMetricsSchema,
                    ErrorSchema,
                ],
            );

            let isCancelled = false;
            await new Promise<void>(async (resolve, reject) => {
                try {
                    for await (const [message, error] of callResult.many()) {
                        if (isCancelled) break;
                        if (error) {
                            reject(error);
                            break;
                        }
                        if (message) {
                            const typeName = (message as any).$typeName;
                            if (typeName === "api.ProjectionMonth") {
                                const month = message as any;
                                if (month.date.startsWith(currentPrefix)) {
                                    currentMonthSheet = month;
                                    isCancelled = true;
                                    resolve();
                                    break;
                                }
                                if (!currentMonthSheet) {
                                    currentMonthSheet = month;
                                }
                            }
                        }
                    }
                    resolve();
                } catch (e) {
                    reject(e);
                }
            });
        } catch (err) {
            console.error("Failed to run projection for dashboard", err);
        } finally {
            isProjecting = false;
        }
    }

    async function fetchScenarios() {
        try {
            const [resp, err] = await wsCall("scenarios::list", null, null, [
                ScenarioListSchema,
            ]).one();
            if (err) throw err;
            scenarios = resp?.scenarios ?? [];

            if (scenarios.length > 0 && !selectedScenarioId) {
                const active = resolveActiveScenario(scenarios);
                selectedScenarioId = active?.id || scenarios[0]?.id || null;
            }
            if (selectedScenarioId) {
                await loadCurrentMonthBudgetSheet(selectedScenarioId);
            }
        } catch (err) {
            console.error(err);
        } finally {
            isLoading = false;
        }
    }

    async function handleScenarioChange(id: string) {
        selectedScenarioId = id;
        savePreferredScenario(id);

        // Persist to user profile if logged in
        if (auth.isAuthenticated) {
            try {
                await wsCall("user::dashboard", ScenarioSchema, { id }, [
                    AuthSuccessResponseSchema,
                ]).one();
            } catch (e) {
                console.error("Failed to persist dashboard config", e);
            }
        }
        await loadCurrentMonthBudgetSheet(id);
    }

    onMount(fetchScenarios);
</script>

<svelte:head>
    <title>Dashboard — BudgetScript</title>
</svelte:head>

<div class="space-y-12">
    <!-- Header Section -->
    <header
        class="flex flex-col md:flex-row md:items-end justify-between gap-6"
    >
        <div class="space-y-2">
            <h1 class="text-5xl font-black tracking-tight text-slate-900">
                Wealth <span class="gradient-text">Nodes</span>.
            </h1>
            <p class="text-slate-500 font-medium text-lg">
                Integrated control node for user <span
                    class="font-black text-slate-700"
                    >{auth.user?.username}</span
                >.
            </p>
        </div>

        <div
            class="flex items-center gap-4 bg-white p-2 rounded-3xl shadow-sm border border-slate-100"
        >
            <div class="pl-4 pr-2 py-2">
                <div class="flex items-center gap-2">
                    <div
                        class="w-2 h-2 rounded-full bg-emerald-500 animate-pulse mt-4"
                    ></div>
                    <SearchableDropdown
                        label="Active Scenario"
                        options={scenarios.map((s) => ({
                            id: s.id,
                            label: s.name,
                        }))}
                        bind:value={selectedScenarioId}
                        onchange={() =>
                            handleScenarioChange(selectedScenarioId || "")}
                    />
                </div>
            </div>
        </div>
    </header>

    {#if isLoading}
        <div
            class="glass-card p-20 flex flex-col items-center justify-center space-y-4"
        >
            <Loader2 class="w-12 h-12 text-indigo-600 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]"
            >
                Assembling Node...
            </p>
        </div>
    {:else if scenarios.length === 0}
        <div class="glass-card p-20 text-center space-y-8">
            <div
                class="inline-flex p-6 bg-indigo-50 rounded-[40px] text-indigo-600 shadow-inner"
            >
                <Activity class="w-12 h-12" />
            </div>
            <div class="space-y-2">
                <h2 class="text-3xl font-black text-slate-900">
                    Awaiting Base Scenario
                </h2>
                <p class="text-slate-500 max-w-sm mx-auto font-medium text-lg">
                    You need to initialize at least one scenario before you can
                    manage your wealth nodes.
                </p>
            </div>
            <a
                href="/scenarios"
                class="btn-primary px-12 py-5 text-xl shadow-2xl shadow-indigo-100"
            >
                Initialize Base Scenario
                <ArrowRight class="w-6 h-6" />
            </a>
        </div>
    {:else}
        <div class="space-y-12">
            <!-- Global Entity Editors Navigation -->
            <div class="glass-card p-6 flex flex-wrap items-center gap-3">
                <span
                    class="text-[10px] font-black uppercase tracking-widest text-slate-400 mr-2"
                    >Entity Editors:</span
                >
                <a
                    href="/timeline"
                    class="px-4 py-2.5 bg-indigo-50 hover:bg-indigo-100 border border-indigo-200 hover:border-indigo-600 text-indigo-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm flex items-center gap-2 font-bold"
                >
                    Timeline Editor
                </a>
                <a
                    href="/dashboard/assets"
                    class="px-4 py-2.5 bg-white hover:bg-emerald-50 border border-slate-200 hover:border-emerald-600 hover:text-emerald-600 text-slate-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm flex items-center gap-2"
                >
                    Assets
                </a>
                <a
                    href="/dashboard/virtual-accounts"
                    class="px-4 py-2.5 bg-white hover:bg-cyan-50 border border-slate-200 hover:border-cyan-600 hover:text-cyan-600 text-slate-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm"
                >
                    Virtual Accounts
                </a>
                <a
                    href="/dashboard/incomes"
                    class="px-4 py-2.5 bg-white hover:bg-indigo-50 border border-slate-200 hover:border-indigo-600 hover:text-indigo-600 text-slate-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm"
                >
                    Incomes
                </a>
                <a
                    href="/dashboard/expenses"
                    class="px-4 py-2.5 bg-white hover:bg-rose-50 border border-slate-200 hover:border-rose-600 hover:text-rose-600 text-slate-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm"
                >
                    Expenses
                </a>
                <a
                    href="/dashboard/bills"
                    class="px-4 py-2.5 bg-white hover:bg-orange-50 border border-slate-200 hover:border-orange-600 hover:text-orange-600 text-slate-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm"
                >
                    Bills
                </a>
                <a
                    href="/dashboard/loans"
                    class="px-4 py-2.5 bg-white hover:bg-amber-50 border border-slate-200 hover:border-amber-600 hover:text-amber-600 text-slate-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm"
                >
                    Loans
                </a>
                <a
                    href="/dashboard/modifications"
                    class="px-4 py-2.5 bg-white hover:bg-purple-50 border border-slate-200 hover:border-purple-600 hover:text-purple-600 text-slate-700 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm"
                >
                    Modifications
                </a>
            </div>

            <!-- Current Month's Budget Sheet -->
            {#if currentMonthSheet}
                <div class="glass-card p-8 space-y-6" transition:fade>
                    <div
                        class="flex flex-col lg:flex-row lg:items-center justify-between gap-6 border-b border-slate-100 pb-6"
                    >
                        <div>
                            <h2
                                class="text-2xl font-black tracking-tight uppercase"
                            >
                                Current Month Overview
                            </h2>
                            <p class="text-slate-500 font-medium">
                                Real-time projected budget sheet for active
                                scenario.
                            </p>
                        </div>

                        <button
                            onclick={() =>
                                loadCurrentMonthBudgetSheet(
                                    selectedScenarioId!,
                                )}
                            disabled={isProjecting}
                            class="px-4 py-2.5 bg-slate-50 hover:bg-purple-50 border border-slate-200/60 hover:border-purple-600 hover:text-purple-600 text-slate-600 rounded-xl text-[10px] font-black uppercase tracking-widest transition-all shadow-sm flex items-center gap-2"
                        >
                            <RefreshCw
                                class="w-3.5 h-3.5 {isProjecting
                                    ? 'animate-spin text-purple-600'
                                    : ''}"
                            />
                            Refresh Sheet
                        </button>
                    </div>

                    <BudgetSheet
                        date={currentMonthSheet.date}
                        breakdown={currentMonthSheet.breakdown}
                        totalIncome={currentMonthSheet.income}
                        totalBills={currentMonthSheet.bills}
                        totalExpenses={currentMonthSheet.expenses}
                        totalAssets={currentMonthSheet.assets}
                        totalLoans={currentMonthSheet.loans}
                        remainder={currentMonthSheet.remainder}
                        virtualAccounts={currentMonthSheet.virtualAccounts}
                    />
                </div>
            {:else if isProjecting}
                <div
                    class="glass-card p-12 flex flex-col items-center justify-center space-y-3"
                    transition:fade
                >
                    <Loader2 class="w-8 h-8 text-purple-600 animate-spin" />
                    <p
                        class="text-slate-400 font-black uppercase tracking-[0.2em] text-[9px]"
                    >
                        Calculating current month budget sheet...
                    </p>
                </div>
            {/if}
        </div>
    {/if}
</div>
