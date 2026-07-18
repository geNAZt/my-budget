<script lang="ts">
    import { Sparkles, PieChart, LineChart } from "@lucide/svelte";
    import { Line } from "svelte-chartjs";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    let {
        timeHorizonYears = 30,
        selectedScenarioIds = [],
        scenarios = [],
        projections = {},
        availableAssets = [],
        selectedAssetScenarioId = $bindable(""),
        selectedAssetName = $bindable(""),
        assetSummary = null as any,
        selectedAssetInfo = null as any,
        finalRealSplit = null as any,
        trackerCumulativeFlows = {} as any,
        selectedTrackerRange = $bindable("max"),
        assetChartData = null,
        assetChartOptions = null,
        trackerChartOptions = null,
        getTrackerChartData = (tracker: string) => null as any,
        formatGermanAmount = (v: number) => v.toFixed(2),
    } = $props<{
        timeHorizonYears: number;
        selectedScenarioIds: string[];
        scenarios: any[];
        projections: Record<string, any>;
        availableAssets: string[];
        selectedAssetScenarioId: string;
        selectedAssetName: string;
        assetSummary: any;
        selectedAssetInfo: any;
        finalRealSplit: any;
        trackerCumulativeFlows: any;
        selectedTrackerRange: string;
        assetChartData: any;
        assetChartOptions: any;
        trackerChartOptions: any;
        getTrackerChartData: (tracker: string) => any;
        formatGermanAmount: (v: number) => string;
    }>();

    function getID(entity: any): string {
        return entity?.id || entity?.Id || entity?.ID || "";
    }

    function getName(entity: any): string {
        return entity?.name || entity?.Name || "";
    }
</script>

<div class="flex flex-col md:flex-row md:items-center justify-between gap-6 pb-6 border-b border-slate-100 dark:border-slate-800">
    <div class="space-y-1">
        <div class="flex items-center gap-2 text-indigo-605 dark:text-indigo-400">
            <Sparkles class="w-4 h-4" />
            <span class="text-xs font-black uppercase tracking-[0.2em]">Asset Drill-down</span>
        </div>
        <h3 class="text-3xl font-black text-slate-900 dark:text-slate-100 tracking-tight">
            Asset Details <span class="text-transparent bg-clip-text bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500">Explorer</span>
        </h3>
        <p class="text-slate-500 dark:text-slate-400 font-medium text-sm">
            Analyze the month-by-month projection details of a specific asset (Balance, Net Flow, Growth, and Penalties).
        </p>
    </div>

    <div class="flex flex-wrap gap-4 items-center">
        <!-- Scenario Selector -->
        <div class="flex flex-col space-y-1.5">
            <SearchableDropdown
                label="Scenario"
                options={selectedScenarioIds.map((id: string) => ({
                    id,
                    label: scenarios.find((s: any) => s.id === id)?.name || id,
                }))}
                bind:value={selectedAssetScenarioId}
            />
        </div>

        <!-- Asset Selector -->
        <div class="flex flex-col space-y-1.5">
            <SearchableDropdown
                label="Asset"
                options={availableAssets.map((name: string) => ({
                    id: name,
                    label: name,
                }))}
                bind:value={selectedAssetName}
                placeholder={availableAssets.length === 0 ? "No Assets Available" : "Select Asset..."}
            />
        </div>
    </div>
</div>

{#if availableAssets.length > 0 && selectedAssetName}
    <!-- Quick Summary Metrics -->
    {#if assetSummary}
        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-6">
            <div class="glass-card p-6 border border-slate-100 dark:border-slate-850 flex flex-col space-y-2 shadow-sm">
                <span class="text-[9px] font-black text-emerald-650 uppercase tracking-[0.2em]">Ending Balance</span>
                <span class="font-black text-slate-900 dark:text-slate-100 text-2xl">
                    € {formatGermanAmount(assetSummary.endingBalance)}
                </span>
                <span class="text-[10px] text-slate-400 font-medium">Accumulated value at {timeHorizonYears}y</span>
            </div>

            <div class="glass-card p-6 border border-slate-100 dark:border-slate-850 flex flex-col space-y-2 shadow-sm">
                <span class="text-[9px] font-black text-indigo-650 uppercase tracking-[0.2em]">Total Contributions</span>
                <span class="font-black text-slate-900 dark:text-slate-100 text-2xl">
                    € {formatGermanAmount(assetSummary.totalContributions)}
                </span>
                <span class="text-[10px] text-slate-400 font-medium">Total capital deposited</span>
            </div>

            <div class="glass-card p-6 border border-slate-100 dark:border-slate-850 flex flex-col space-y-2 shadow-sm">
                <span class="text-[9px] font-black text-amber-655 uppercase tracking-[0.2em]">Total Interest / Growth</span>
                <span class="font-black text-emerald-600 dark:text-emerald-455 text-2xl">
                    € {formatGermanAmount(assetSummary.totalInterest)}
                </span>
                <span class="text-[10px] text-slate-400 font-medium">Total return generated over time</span>
            </div>

            <div class="glass-card p-6 border border-slate-100 dark:border-slate-850 flex flex-col space-y-2 shadow-sm">
                <span class="text-[9px] font-black text-cyan-650 uppercase tracking-[0.2em]">Total Withdrawals</span>
                <span class="font-black text-slate-900 dark:text-slate-100 text-2xl">
                    € {formatGermanAmount(assetSummary.totalWithdrawals)}
                </span>
                <span class="text-[10px] text-slate-400 font-medium">Total capital withdrawn / paid out</span>
            </div>

            <div class="glass-card p-6 border border-slate-100 dark:border-slate-850 flex flex-col space-y-2 shadow-sm">
                <span class="text-[9px] font-black text-rose-500 uppercase tracking-[0.2em]">Total Penalties Paid</span>
                <span class="font-black text-rose-600 dark:text-rose-455 text-2xl">
                    € {formatGermanAmount(assetSummary.totalPenalties)}
                </span>
                <span class="text-[10px] text-slate-400 font-medium">Fees incurred from early withdrawal</span>
            </div>
        </div>
    {/if}

    <!-- ETF Trackers Allocations Card Grid -->
    {#if selectedAssetInfo?.activeVersion?.type === "ETF" && selectedAssetInfo?.activeVersion?.etfConfig?.length > 0}
        <div class="glass-card p-8 border border-emerald-100/50 bg-emerald-50/5 dark:border-emerald-950/20 dark:bg-emerald-950/5 space-y-6 shadow-sm">
            <div class="flex items-center justify-between border-b border-slate-100 dark:border-slate-800 pb-4">
                <div class="flex items-center gap-2.5">
                    <div class="p-2 bg-emerald-100 dark:bg-emerald-900/30 rounded-xl">
                        <PieChart class="w-4 h-4 text-emerald-600 dark:text-emerald-400" />
                    </div>
                    <div>
                        <h4 class="text-xs font-black uppercase tracking-[0.2em] text-slate-900 dark:text-slate-100">
                            ETF Tracker Allocation Details
                        </h4>
                        <p class="text-[10px] text-slate-450 dark:text-slate-400 font-semibold uppercase tracking-wider mt-0.5">
                            Distribution of Worth Contribution
                        </p>
                    </div>
                </div>
                <div class="flex items-center gap-3">
                    <div class="flex bg-slate-100 dark:bg-slate-800 p-1 rounded-lg">
                        {#each ['max', '1w', '1d'] as range}
                            <button
                                class="px-2 py-1 rounded-md text-[9px] font-black uppercase tracking-wider transition-all {selectedTrackerRange === range ? 'bg-white text-emerald-600 shadow-sm dark:bg-slate-700 dark:text-emerald-400' : 'text-slate-400 hover:text-slate-600'}"
                                onclick={() => selectedTrackerRange = range}
                            >
                                {range}
                            </button>
                        {/each}
                    </div>
                    <span class="px-2.5 py-1 rounded-full text-[9px] font-black uppercase tracking-wider bg-emerald-50 text-emerald-700 border border-emerald-100 dark:bg-emerald-950/40 dark:text-emerald-400 dark:border-emerald-900/50">
                        {#if finalRealSplit}
                            Drifted Real Split
                        {:else}
                            Target Allocation
                        {/if}
                    </span>
                </div>
            </div>
            <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
                {#each selectedAssetInfo.activeVersion.etfConfig as tracker}
                    {@const percentage = finalRealSplit && finalRealSplit[tracker.tracker] !== undefined ? finalRealSplit[tracker.tracker] : tracker.percentage}
                    {@const endingWorth = (assetSummary?.endingBalance || 0) * percentage}
                    {@const totalCont = trackerCumulativeFlows[tracker.tracker] !== undefined ? trackerCumulativeFlows[tracker.tracker] : (assetSummary?.totalContributions || 0) * percentage}
                    {@const totalGrowth = endingWorth - totalCont}
                    {@const trackerYield = projections[selectedAssetScenarioId]?.simulated_yields?.[`${selectedAssetInfo.id}_${tracker.tracker}`]}
                    {@const trackerData = getTrackerChartData(tracker.tracker)}
                    <div class="bg-white dark:bg-slate-800 border border-slate-100 dark:border-slate-700/50 rounded-2xl p-5 shadow-sm space-y-4 hover:border-emerald-200/50 dark:hover:border-emerald-900/40 hover:shadow-md transition-all duration-300 relative overflow-hidden group">
                        <div class="absolute top-0 left-0 w-full h-1 bg-gradient-to-r from-emerald-400 to-teal-500 opacity-0 group-hover:opacity-100 transition-opacity"></div>
                        <div class="flex items-start justify-between gap-4">
                            <div class="min-w-0 flex-1">
                                <h5 class="font-black text-slate-900 dark:text-slate-100 truncate text-sm" title={tracker.tracker}>
                                    {tracker.tracker || "Unnamed Tracker"}
                                </h5>
                                <p class="text-[9px] text-slate-400 font-bold uppercase tracking-wider mt-0.5">
                                    TER: {tracker.ter || 0}%
                                    {#if trackerYield !== undefined}
                                        <span class="mx-1 text-slate-350">•</span>
                                        <span class="text-emerald-600 dark:text-emerald-455 font-black">Yield: {trackerYield.toFixed(2)}%</span>
                                    {/if}
                                </p>
                            </div>
                            <span class="px-2 py-0.5 rounded-lg text-xs font-black bg-emerald-50 text-emerald-700 border border-emerald-100/50 dark:bg-emerald-950/20 dark:text-emerald-400 dark:border-emerald-900/50">
                                {(percentage * 100).toFixed(0)}%
                            </span>
                        </div>

                        <!-- Progress Split Bar -->
                        <div class="space-y-1.5">
                            <div class="w-full h-1.5 bg-slate-100 dark:bg-slate-700 rounded-full overflow-hidden">
                                <div
                                    class="h-full bg-emerald-500 rounded-full transition-all duration-500"
                                    style="width: {percentage * 100}%"
                                ></div>
                            </div>
                        </div>

                        <div class="grid grid-cols-3 gap-2 pt-3 border-t border-slate-50 dark:border-slate-700 text-left">
                            <div class="space-y-0.5">
                                <span class="text-slate-405 dark:text-slate-450 font-bold block text-[9px] uppercase tracking-wider">Ending Worth</span>
                                <span class="font-black text-slate-900 dark:text-slate-100 text-[10px] sm:text-xs truncate block" title="€ {formatGermanAmount(endingWorth)}">
                                    € {formatGermanAmount(endingWorth)}
                                </span>
                            </div>
                            <div class="space-y-0.5">
                                <span class="text-slate-405 dark:text-slate-450 font-bold block text-[9px] uppercase tracking-wider">Total Inflow</span>
                                <span class="font-black text-indigo-600 dark:text-indigo-400 text-[10px] sm:text-xs truncate block" title="€ {formatGermanAmount(totalCont)}">
                                    € {formatGermanAmount(totalCont)}
                                </span>
                            </div>
                            <div class="space-y-0.5">
                                <span class="text-slate-405 dark:text-slate-450 font-bold block text-[9px] uppercase tracking-wider">Growth Share</span>
                                <span class="font-black text-emerald-650 dark:text-emerald-455 text-[10px] sm:text-xs truncate block" title="€ {formatGermanAmount(totalGrowth)}">
                                    € {formatGermanAmount(totalGrowth)}
                                </span>
                            </div>
                        </div>

                        <!-- Chart Section -->
                        {#if trackerData}
                            <div class="pt-3 border-t border-slate-50 dark:border-slate-700 space-y-1.5">
                                <div class="flex items-center justify-between">
                                    <span class="text-slate-400 font-bold block text-[9px] uppercase tracking-wider text-left">Historical Performance (Base 100)</span>
                                    <div class="flex items-center space-x-2.5 text-[8px] font-bold uppercase tracking-wider">
                                        <span class="flex items-center text-emerald-600"><span class="w-1.5 h-1.5 rounded-full bg-emerald-500 mr-1"></span>History</span>
                                        {#if selectedTrackerRange === 'max'}
                                            <span class="flex items-center text-indigo-600"><span class="w-1.5 h-1.5 rounded-full bg-indigo-500 mr-1"></span>Monte Carlo</span>
                                        {/if}
                                    </div>
                                </div>
                                <div class="h-28 relative">
                                    <Line data={trackerData} options={trackerChartOptions} />
                                </div>
                            </div>
                        {:else}
                            <div class="pt-3 border-t border-slate-50 dark:border-slate-700 text-center py-4 text-[9px] text-slate-400 font-bold uppercase tracking-[0.2em]">
                                Loading Chart Data...
                            </div>
                        {/if}
                    </div>
                {/each}
            </div>
        </div>
    {/if}

    <div class="flex-1 relative min-h-[350px]">
        {#if assetChartData}
            <Line data={assetChartData} options={assetChartOptions} />
        {:else}
            <div class="absolute inset-0 flex items-center justify-center text-slate-400 font-bold text-sm">
                Preparing Asset Chart...
            </div>
        {/if}
    </div>
{:else}
    <div class="flex flex-col items-center justify-center p-20 border border-dashed border-slate-200 rounded-3xl space-y-4">
        <LineChart class="w-12 h-12 text-slate-300" />
        <div class="text-center space-y-1">
            <h4 class="font-black text-slate-900 dark:text-slate-100 text-sm uppercase tracking-wider">
                No Active Asset for Detailed Exploration
            </h4>
            <p class="text-slate-405 dark:text-slate-400 font-medium text-xs max-w-md">
                Please select a scenario that contains active asset allocations to analyze their specific growth, interest, and penalty behavior over time.
            </p>
        </div>
    </div>
{/if}
