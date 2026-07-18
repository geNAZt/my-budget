<script lang="ts">
    import { HelpCircle } from "@lucide/svelte";
    import { Bar } from "svelte-chartjs";

    let {
        monthsWithRealData = [],
        realChartData = null,
        realChartOptions = null,
        selectedRealMonthIndex = $bindable(0),
        realVsPlannedItems = {
            plannedNet: 0,
            realNet: 0,
            varianceNet: 0,
            totalRealIncome: 0,
            totalRealSpending: 0,
            incomes: [] as any[],
            spendings: [] as any[],
        },
        formatGermanAmount = (v: number) => v.toFixed(2),
    } = $props<{
        monthsWithRealData: any[];
        realChartData: any;
        realChartOptions: any;
        selectedRealMonthIndex: number;
        realVsPlannedItems: {
            plannedNet: number;
            realNet: number;
            varianceNet: number;
            totalRealIncome: number;
            totalRealSpending: number;
            incomes: any[];
            spendings: any[];
        };
        formatGermanAmount: (v: number) => string;
    }>();
</script>

{#if monthsWithRealData.length === 0}
    <div class="flex flex-col items-center justify-center p-16 border border-dashed border-slate-200 rounded-3xl space-y-6 text-center">
        <div class="p-4 bg-slate-50 border border-slate-100 rounded-3xl shadow-sm dark:bg-slate-800 dark:border-slate-700">
            <HelpCircle class="w-8 h-8 text-slate-300 dark:text-slate-500" />
        </div>
        <div class="space-y-2 max-w-md">
            <h4 class="font-black text-slate-900 dark:text-slate-100 text-base uppercase tracking-wider">
                No Real-World Transaction Data Found
            </h4>
            <p class="text-slate-500 dark:text-slate-400 font-medium text-xs">
                Actual transaction balance sync has not been established yet. To track real spending vs planned budget, please connect a financial integration or import transactions.
            </p>
        </div>
        <a href="/scenarios" class="px-6 py-3 bg-slate-900 text-white hover:bg-indigo-600 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all active:scale-95 inline-flex">
            Setup Financial Integration
        </a>
    </div>
{:else}
    <!-- Bar chart comparison -->
    <div class="grid grid-cols-1 lg:grid-cols-3 gap-8">
        <div class="lg:col-span-2 space-y-4">
            <h4 class="text-xs font-black uppercase tracking-[0.2em] text-slate-900 dark:text-slate-100">
                Historical Planned vs. Real Execution
            </h4>
            <div class="h-[220px] relative">
                {#if realChartData}
                    <Bar data={realChartData} options={realChartOptions} />
                {/if}
            </div>
        </div>

        <!-- Selected Month switcher and KPI summary cards -->
        <div class="glass-card p-6 border border-slate-100 dark:border-slate-800 flex flex-col justify-between space-y-4">
            <div class="space-y-3">
                <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 block ml-1">Select Month to Detail</label>
                <select
                    bind:value={selectedRealMonthIndex}
                    class="w-full p-2.5 text-xs font-bold text-slate-700 bg-slate-50 border border-slate-200 rounded-xl outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-100 transition-all dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                >
                    {#each monthsWithRealData as m, idx}
                        <option value={idx}>
                            {new Date(m.date).toLocaleDateString("de-DE", {
                                month: "long",
                                year: "numeric",
                            })}
                        </option>
                    {/each}
                </select>
            </div>

            <div class="space-y-3 pt-3 border-t border-slate-100 dark:border-slate-800">
                <!-- Net Variance Card -->
                <div class="flex items-center justify-between text-xs">
                    <span class="text-slate-400 font-bold">Planned Net:</span>
                    <span class="font-black text-slate-900 dark:text-slate-100">€ {formatGermanAmount(realVsPlannedItems.plannedNet)}</span>
                </div>
                <div class="flex items-center justify-between text-xs">
                    <span class="text-slate-400 font-bold">Real Net:</span>
                    <span class="font-black text-slate-900 dark:text-slate-100">€ {formatGermanAmount(realVsPlannedItems.realNet)}</span>
                </div>
                <div class="flex items-center justify-between text-xs pt-2 border-t border-dashed border-slate-100 dark:border-slate-800">
                    <span class="text-slate-500 font-black dark:text-slate-350">Net Variance:</span>
                    <span class="font-black {realVsPlannedItems.varianceNet >= 0 ? 'text-emerald-600' : 'text-rose-600'}">
                        € {realVsPlannedItems.varianceNet >= 0 ? "+" : ""}{formatGermanAmount(realVsPlannedItems.varianceNet)}
                    </span>
                </div>
            </div>
        </div>
    </div>

    <!-- Itemized Category Breakdown Tables -->
    <div class="space-y-6 pt-4">
        <div class="flex items-center justify-between">
            <h4 class="text-xs font-black uppercase tracking-[0.2em] text-slate-900 dark:text-slate-100">
                Itemized Category Breakdown
            </h4>
        </div>

        <div class="grid grid-cols-1 xl:grid-cols-2 gap-8">
            <!-- Incomes Table -->
            <div class="glass-card border border-slate-100 dark:border-slate-800 overflow-hidden">
                <div class="px-6 py-4 bg-slate-50 dark:bg-slate-900/50 border-b border-slate-100 dark:border-slate-800 flex justify-between items-center">
                    <h5 class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-900 dark:text-slate-100">
                        Income Sources
                    </h5>
                    <span class="text-[10px] font-black text-emerald-600">Real Total: € {formatGermanAmount(realVsPlannedItems.totalRealIncome)}</span>
                </div>
                <div class="overflow-x-auto max-h-[300px] overflow-y-auto">
                    <table class="w-full text-left border-collapse table-fixed">
                        <colgroup>
                            <col class="w-auto" />
                            <col class="w-20" />
                            <col class="w-20" />
                            <col class="w-24" />
                        </colgroup>
                        <thead>
                            <tr class="bg-slate-50/50 dark:bg-slate-900/30 border-b border-slate-100 dark:border-slate-800 text-[9px] font-black uppercase tracking-[0.1em] text-slate-400">
                                <th class="px-6 py-3">Category</th>
                                <th class="px-3 py-3 text-right">Planned</th>
                                <th class="px-3 py-3 text-right">Real</th>
                                <th class="px-6 py-3 text-right">Variance</th>
                            </tr>
                        </thead>
                        <tbody class="divide-y divide-slate-50 dark:divide-slate-800 text-[11px]">
                            {#each realVsPlannedItems.incomes as item}
                                <tr class="hover:bg-slate-50/50 dark:hover:bg-slate-900/20 transition-colors">
                                    <td class="px-6 py-3.5 font-bold text-slate-700 dark:text-slate-300 truncate pr-2" title={item.name}>
                                        {item.name}
                                    </td>
                                    <td class="px-3 py-3.5 text-right font-bold text-slate-400">
                                        €{formatGermanAmount(item.planned)}
                                    </td>
                                    <td class="px-3 py-3.5 text-right font-black text-slate-900 dark:text-slate-100">
                                        €{formatGermanAmount(item.real)}
                                    </td>
                                    <td class="px-6 py-3.5 text-right font-bold">
                                        <span class={item.variance >= 0 ? "text-emerald-600" : "text-rose-500"}>
                                            {item.variance >= 0 ? "+" : ""}{formatGermanAmount(item.variance)}
                                        </span>
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </div>

            <!-- Spendings Table -->
            <div class="glass-card border border-slate-100 dark:border-slate-800 overflow-hidden">
                <div class="px-6 py-4 bg-slate-50 dark:bg-slate-900/50 border-b border-slate-100 dark:border-slate-800 flex justify-between items-center">
                    <h5 class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-900 dark:text-slate-100">
                        Spending & Savings
                    </h5>
                    <span class="text-[10px] font-black text-rose-600">Real Total: € {formatGermanAmount(realVsPlannedItems.totalRealSpending)}</span>
                </div>
                <div class="overflow-x-auto max-h-[300px] overflow-y-auto">
                    <table class="w-full text-left border-collapse table-fixed">
                        <colgroup>
                            <col class="w-auto" />
                            <col class="w-16" />
                            <col class="w-20" />
                            <col class="w-20" />
                            <col class="w-24" />
                        </colgroup>
                        <thead>
                            <tr class="bg-slate-50/50 dark:bg-slate-900/30 border-b border-slate-100 dark:border-slate-800 text-[9px] font-black uppercase tracking-[0.1em] text-slate-400">
                                <th class="px-6 py-3">Category</th>
                                <th class="px-2 py-3">Type</th>
                                <th class="px-3 py-3 text-right">Planned</th>
                                <th class="px-3 py-3 text-right">Real</th>
                                <th class="px-6 py-3 text-right">Variance</th>
                            </tr>
                        </thead>
                        <tbody class="divide-y divide-slate-50 dark:divide-slate-800 text-[11px]">
                            {#each realVsPlannedItems.spendings as item}
                                <tr class="hover:bg-slate-50/50 dark:hover:bg-slate-900/20 transition-colors">
                                    <td class="px-6 py-3.5 font-bold text-slate-700 dark:text-slate-300 truncate pr-2" title={item.name}>
                                        {item.name}
                                    </td>
                                    <td class="px-2 py-3.5 font-black text-[9px] uppercase tracking-tighter text-slate-400">
                                        {item.type}
                                    </td>
                                    <td class="px-3 py-3.5 text-right font-bold text-slate-400">
                                        €{formatGermanAmount(item.planned)}
                                    </td>
                                    <td class="px-3 py-3.5 text-right font-black text-slate-900 dark:text-slate-100">
                                        €{formatGermanAmount(item.real)}
                                    </td>
                                    <td class="px-6 py-3.5 text-right font-bold">
                                        <span class={item.variance <= 0 ? "text-emerald-600" : "text-rose-500"}>
                                            {item.variance > 0 ? "+" : ""}{formatGermanAmount(item.variance)}
                                        </span>
                                    </td>
                                </tr>
                            {/each}
                        </tbody>
                    </table>
                </div>
            </div>
        </div>
    </div>
{/if}
