<script lang="ts">
    import { Trash2 } from "@lucide/svelte";

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

    let { etfConfig = $bindable([]) } = $props<{ etfConfig: ETFTracker[] }>();
</script>

<div class="space-y-4">
    <div class="flex items-center justify-between">
        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1">
            ETF Tracker Nodes
        </label>
        <button
            type="button"
            onclick={() => {
                if (!etfConfig) {
                    etfConfig = [];
                }
                etfConfig.push({
                    tracker: "",
                    historicalTracker: "",
                    conversionTracker: "",
                    historyProvider: "",
                    percentage: 0.7,
                    ter: 0.2,
                    stitchingSegments: [],
                });
            }}
            class="text-[9px] font-black text-emerald-600 hover:underline uppercase"
        >
            + Add Tracker
        </button>
    </div>

    {#each etfConfig as etf, i}
        <div class="p-4 bg-slate-50 border border-slate-200 rounded-xl space-y-3">
            <div class="grid grid-cols-12 gap-2 items-center">
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
                    onclick={() => etfConfig.splice(i, 1)}
                    class="col-span-1 text-rose-400 hover:text-rose-600 transition-colors"
                >
                    <Trash2 class="w-4 h-4 mx-auto" />
                </button>
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
                    >
                        + Add Stitching Segment
                    </button>
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
                            >
                                ✕
                            </button>
                        </div>
                    {/each}
                    <p class="text-[8px] text-slate-400 italic">
                        Segments are stitched chronologically. Primary (top) runs first, and older/missing history is filled by subsequent backfill segments scaled at overlap dates.
                    </p>
                {/if}
            </div>
        </div>
    {/each}
</div>
