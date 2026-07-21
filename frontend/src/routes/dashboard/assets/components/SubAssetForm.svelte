<script lang="ts">
    import { Plus, Trash2 } from "@lucide/svelte";
    import { slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import { formatGermanAmount } from "$lib/utils/format";

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

    interface Loan {
        id: string;
        name: string;
    }

    interface Expense {
        id: string;
        name: string;
    }

    let {
        subAssets = $bindable([]),
        loans = [],
        expenses = [],
        interestInput = "0",
        assetType = "STATIC",
        startDate = "",
        toInputMonth,
        fromInputMonth,
        calculateRequiredRate,
        parseNumeric,
    } = $props<{
        subAssets: SubAsset[];
        loans: Loan[];
        expenses: Expense[];
        interestInput: string;
        assetType: "STATIC" | "ETF";
        startDate: string;
        toInputMonth: (d: any) => string;
        fromInputMonth: (val: string) => string;
        calculateRequiredRate: (targetVal: string, start: string, end: string | null, interestRate: number) => number;
        parseNumeric: (val: string | number, locale: "DE" | "US") => number;
    }>();

    const expenseOptions = $derived(
        (expenses || []).map((e: any) => ({
            id: e.id,
            label: e.name,
        })),
    );
</script>

<div class="space-y-4 p-6 bg-white rounded-2xl border border-slate-100 shadow-sm">
    <div class="flex items-center justify-between">
        <div class="space-y-0.5">
            <label class="text-sm font-black text-slate-900">Logical Sub-Assets / Targets</label>
            <p class="text-[10px] font-medium text-slate-500">
                Define multiple logical target sub-assets sharing this same account.
            </p>
        </div>
        <button
            type="button"
            onclick={() => {
                if (!subAssets) {
                    subAssets = [];
                }
                subAssets.push({
                    id: crypto.randomUUID(),
                    name: "",
                    targetValue: "0",
                    amountPerMonth: 0,
                    isRemainderConsumer: false,
                    remainderStartDate: null,
                    dumpingLoanId: null,
                    startDate: startDate || new Date().toISOString(),
                    endDate: null,
                    earliestDumpDate: null,
                    expenseId: null,
                    remainderPriority: 0,
                });
            }}
            class="px-3 py-1.5 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg text-[10px] font-black uppercase tracking-wider transition-colors shadow-sm flex items-center gap-1"
        >
            <Plus class="w-3 h-3" /> Add Target
        </button>
    </div>

    {#if subAssets && subAssets.length > 0}
        <div class="space-y-4 pt-2">
            {#each subAssets as target, i}
                <div
                    class="p-4 bg-white border border-slate-100 rounded-xl space-y-3 relative shadow-sm"
                    transition:slide
                >
                    <div class="flex justify-between items-center">
                        <span class="px-2 py-0.5 bg-indigo-50 text-indigo-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]">
                            Target #{i + 1}
                        </span>
                        <button
                            type="button"
                            onclick={() => subAssets.splice(i, 1)}
                            class="text-rose-400 hover:text-rose-600 transition-colors"
                            title="Remove Target"
                        >
                            <Trash2 class="w-4 h-4" />
                        </button>
                    </div>

                    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
                        <div class="space-y-1">
                            <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Target Name</label>
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
                                <div class="flex items-center justify-between">
                                    <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Monthly savings (€)</label>
                                    {#if !target.isRemainderConsumer}
                                        <button
                                            type="button"
                                            onclick={() => {
                                                const rate = calculateRequiredRate(
                                                    String(target.targetValue),
                                                    target.startDate,
                                                    target.endDate || null,
                                                    assetType === "STATIC" ? parseNumeric(interestInput, "DE") : 0,
                                                );
                                                target.amountPerMonth = Math.round(rate * 100) / 100;
                                            }}
                                            class="text-[8px] font-black text-emerald-600 hover:underline uppercase flex items-center gap-0.5"
                                        >
                                            Recalc
                                        </button>
                                    {/if}
                                </div>
                                <input
                                    type="number"
                                    bind:value={target.amountPerMonth}
                                    step="0.01"
                                    placeholder="150"
                                    disabled={target.isRemainderConsumer}
                                    class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all disabled:opacity-50"
                                    required={!target.isRemainderConsumer}
                                />
                            </div>
                            <div class="space-y-1">
                                <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Goal target (€)</label>
                                <input
                                    type="text"
                                    bind:value={target.targetValue}
                                    placeholder="5000"
                                    class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                    required
                                />
                                <span class="text-[9px] text-slate-400 text-right block pr-1">
                                    Goal: {formatGermanAmount(parseFloat(String(target.targetValue)) || 0)} €
                                </span>
                            </div>
                        </div>
                    </div>

                    <div class="grid grid-cols-1 md:grid-cols-3 gap-4 pt-1">
                        <div class="space-y-1">
                            <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Start Month</label>
                            <input
                                type="month"
                                value={toInputMonth(target.startDate)}
                                oninput={(e: any) => (target.startDate = fromInputMonth(e.target.value))}
                                class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                required
                            />
                        </div>
                        <div class="space-y-1">
                            <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Remainder Start</label>
                            <input
                                type="month"
                                value={toInputMonth(target.remainderStartDate)}
                                oninput={(e: any) => (target.remainderStartDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                                class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                            />
                        </div>
                        <div class="space-y-1">
                            <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Payout Month (Optional)</label>
                            <input
                                type="month"
                                value={toInputMonth(target.endDate)}
                                oninput={(e: any) => (target.endDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                                class="block w-full px-3 py-2 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                            />
                        </div>
                    </div>

                    {#if !target.isRemainderConsumer}
                        <div class="flex justify-end pt-1">
                            <button
                                type="button"
                                onclick={() => {
                                    const rate = calculateRequiredRate(
                                        String(target.targetValue),
                                        target.startDate,
                                        target.endDate || null,
                                        assetType === "STATIC" ? parseNumeric(interestInput, "DE") : 0,
                                    );
                                    target.amountPerMonth = Math.round(rate * 100) / 100;
                                }}
                                class="px-2.5 py-1 bg-indigo-50 hover:bg-indigo-100 text-indigo-600 rounded-lg text-[9px] font-black uppercase tracking-wider transition-colors shadow-sm flex items-center gap-1"
                            >
                                Recalculate Rate
                            </button>
                        </div>
                    {/if}

                    <div class="grid grid-cols-1 md:grid-cols-2 gap-4 pt-1 items-center font-bold">
                        <div class="space-y-1">
                            <div class="flex items-center gap-2">
                                <input
                                    type="checkbox"
                                    id="dump_target_{target.id}"
                                    checked={target.dumpingLoanId !== null && target.dumpingLoanId !== ""}
                                    onchange={(e: any) => {
                                        target.dumpingLoanId = e.target.checked ? (loans[0]?.id || "") : null;
                                        if (!e.target.checked) {
                                            target.earliestDumpDate = null;
                                        }
                                    }}
                                    class="rounded border-slate-300 text-emerald-600 focus:ring-emerald-500"
                                />
                                <label
                                    for="dump_target_{target.id}"
                                    class="text-[10px] font-bold text-slate-600 cursor-pointer"
                                >
                                    Enable Target Loan Dumping
                                </label>
                            </div>
                            <div class="flex items-center gap-4 justify-between">
                                <div class="flex items-center gap-2">
                                    <input
                                        type="checkbox"
                                        id="remainder_target_{target.id}"
                                        checked={target.isRemainderConsumer}
                                        onchange={(e: any) => {
                                            target.isRemainderConsumer = e.target.checked;
                                            if (e.target.checked) {
                                                target.amountPerMonth = 0;
                                            }
                                        }}
                                        class="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                                    />
                                    <label
                                        for="remainder_target_{target.id}"
                                        class="text-[10px] font-bold text-slate-600 cursor-pointer"
                                    >
                                        Enable Remainder Consumption
                                    </label>
                                </div>
                                {#if target.isRemainderConsumer}
                                    <div class="flex items-center gap-1.5 shrink-0">
                                        <span class="text-[9px] font-black uppercase text-slate-400">Prio</span>
                                        <input
                                            type="number"
                                            bind:value={target.remainderPriority}
                                            min="0"
                                            class="w-12 px-2 py-1 rounded-lg border border-slate-200 text-xs font-bold text-center outline-none text-slate-700 focus:border-indigo-500 bg-white"
                                            placeholder="0"
                                        />
                                    </div>
                                {/if}
                            </div>
                            <div class="flex items-center gap-2">
                                <input
                                    type="checkbox"
                                    id="expense_target_{target.id}"
                                    checked={target.expenseId !== null && target.expenseId !== ""}
                                    onchange={(e: any) => {
                                        target.expenseId = e.target.checked ? (expenses[0]?.id || "") : null;
                                    }}
                                    class="rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                                />
                                <label
                                    for="expense_target_{target.id}"
                                    class="text-[10px] font-bold text-slate-600 cursor-pointer"
                                >
                                    Enable Expense Funding
                                </label>
                            </div>
                        </div>
                        <div>
                            {#if target.expenseId !== null && target.expenseId !== ""}
                                <div class="pt-1" transition:slide>
                                    <SearchableDropdown
                                        label="Target Expense"
                                        options={expenseOptions}
                                        bind:value={target.expenseId}
                                        placeholder="Select Expense..."
                                    />
                                </div>
                            {/if}
                            {#if target.dumpingLoanId !== null && target.dumpingLoanId !== ""}
                                <div class="grid grid-cols-1 md:grid-cols-2 gap-3 pt-1" transition:slide>
                                    <div class="space-y-1">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Target Loan</label>
                                        <select
                                            bind:value={target.dumpingLoanId}
                                            class="block w-full px-3 py-1.5 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold appearance-none cursor-pointer focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                        >
                                            <option value="" disabled>Select Loan...</option>
                                            {#each loans as loan}
                                                <option value={loan.id}>{loan.name}</option>
                                            {/each}
                                        </select>
                                    </div>
                                    <div class="space-y-1">
                                        <label class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Earliest Dump Month (Optional)</label>
                                        <input
                                            type="month"
                                            value={toInputMonth(target.earliestDumpDate)}
                                            oninput={(e: any) => (target.earliestDumpDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                                            class="block w-full px-3 py-1.5 bg-slate-50 border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-emerald-500/10 focus:border-emerald-500 outline-none transition-all"
                                        />
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
