<script lang="ts">
    import { Loader2 } from "@lucide/svelte";
    import { Line } from "svelte-chartjs";
    import { fade } from "svelte/transition";

    let {
        chartLabels = [],
        loadingProjections = {},
        chartData = null,
        chartOptions = null,
        selectedScenarioIds = [],
        scenarios = [],
        projections = {},
        activeMetric = "net_worth",
        formatCurrency = (v: number) => `€ ${v.toFixed(2)}`,
        getScenarioEvents = (proj: any, metric: string) => ({ events: [] }),
        PALETTE = [],
    } = $props<{
        chartLabels: any[];
        loadingProjections: Record<string, boolean>;
        chartData: any;
        chartOptions: any;
        selectedScenarioIds: string[];
        scenarios: any[];
        projections: Record<string, any>;
        activeMetric: string;
        formatCurrency: (v: number) => string;
        getScenarioEvents: (proj: any, metric: string) => { events: any[] };
        PALETTE: any[];
    }>();
</script>

<div class="flex items-center justify-between">
    <div>
        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Time Series Projection</label>
        <h3 class="text-2xl font-black text-slate-900 dark:text-slate-100 tracking-tight capitalize">
            Plotted Delta: {activeMetric.replace("_", " ")}
        </h3>
    </div>
</div>

<div class="flex-1 relative min-h-[350px]">
    {#if Object.values(loadingProjections).some((x) => x)}
        <div class="absolute inset-0 flex flex-col items-center justify-center space-y-4 bg-white/90 dark:bg-slate-900/90 z-10 rounded-2xl">
            <Loader2 class="w-10 h-10 text-indigo-600 animate-spin" />
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[9px] animate-pulse">
                Running Projections...
            </p>
        </div>
    {/if}

    {#if chartLabels.length > 0 && chartData}
        <Line data={chartData} options={chartOptions} />
    {:else}
        <div class="absolute inset-0 flex items-center justify-center text-slate-400 font-bold text-sm">
            Loading Projection Data...
        </div>
    {/if}
</div>

<!-- Interactive Timeline Section -->
{#if chartLabels.length > 0}
    {@const activeEvents = selectedScenarioIds.map((id: string, idx: number) => {
        const scenario = scenarios.find((s: any) => s.id === id);
        const proj = projections[id];
        const { events } = getScenarioEvents(proj, activeMetric);
        return {
            scenarioName: scenario?.name || "Scenario",
            color: PALETTE[idx % PALETTE.length],
            events
        };
    }).filter((x: any) => x.events.length > 0)}
    
    {#if activeEvents.length > 0}
        <div class="mt-8 border-t border-slate-100 dark:border-slate-800/40 pt-6 animate-fade-in">
            <h4 class="text-xs font-black uppercase tracking-[0.2em] text-slate-400 mb-4 ml-1">
                Significant Projection Milestones
            </h4>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
                {#each activeEvents as scEvents}
                    <div class="bg-slate-50/50 dark:bg-slate-900/20 border border-slate-100 dark:border-slate-800/40 rounded-3xl p-5 space-y-4">
                        <div class="flex items-center gap-2">
                            <div class="w-2.5 h-2.5 rounded-full" style="background-color: {scEvents.color.border}"></div>
                            <span class="text-xs font-black uppercase tracking-wider text-slate-900 dark:text-slate-100">{scEvents.scenarioName}</span>
                        </div>
                        <div class="relative pl-4 border-l border-slate-200/60 dark:border-slate-800/40 space-y-5 ml-1">
                            {#each scEvents.events as ev}
                                <div class="relative group">
                                    <!-- Timeline Dot -->
                                    <div class="absolute -left-[21px] top-1 w-2.5 h-2.5 rounded-full border bg-white dark:bg-slate-950 transition-all group-hover:scale-125" style="border-color: {scEvents.color.border}; background-color: {scEvents.color.border}"></div>
                                    <div class="space-y-1">
                                        <div class="flex items-center justify-between">
                                            <span class="text-[10px] font-black text-slate-900 dark:text-slate-200 uppercase tracking-tight">{ev.title}</span>
                                            <span class="text-[9px] font-black text-slate-400 uppercase tracking-widest">{ev.dateLabel}</span>
                                        </div>
                                        <p class="text-[10px] text-slate-500 dark:text-slate-400 tracking-tight leading-relaxed">{ev.description}</p>
                                        <div class="text-[9px] font-black uppercase tracking-tight {ev.changeAmount >= 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-600 dark:text-rose-400'}">
                                            {ev.changeAmount >= 0 ? '+' : ''}{formatCurrency(ev.changeAmount)} €
                                        </div>
                                    </div>
                                </div>
                            {/each}
                        </div>
                    </div>
                {/each}
            </div>
        </div>
    {/if}
{/if}
