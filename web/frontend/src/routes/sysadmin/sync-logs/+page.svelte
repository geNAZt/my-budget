<script lang="ts">
    import { onMount } from "svelte";
    import { wsCall } from "$lib/utils/ws_fetch";
    import * as api from "$lib/gen/api_pb.js";
    import { 
        Terminal, 
        Search, 
        ChevronLeft,
        ShieldAlert,
        Activity,
        FileText,
        ChevronRight,
        Calendar,
        CheckCircle2,
        XCircle,
        AlertCircle,
        ExternalLink,
        DollarSign,
        Layers,
        Filter
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";

    let runs = $state<any[]>([]);
    let metrics = $state<any>(null);
    let offset = $state(0);
    const limit = 50;
    let hasMore = $state(true);

    function formatBytes(bytes: number | bigint): string {
        const b = Number(bytes);
        if (b === 0) return "0 B";
        const k = 1024;
        const sizes = ["B", "KB", "MB", "GB"];
        const i = Math.floor(Math.log(b) / Math.log(k));
        return parseFloat((b / Math.pow(k, i)).toFixed(1)) + " " + sizes[i];
    }

    function lazyLoadNextPage(node: HTMLElement) {
        const observer = new IntersectionObserver(entries => {
            if (entries[0].isIntersecting && hasMore && !isLoadingRuns) {
                offset += limit;
                loadSyncRuns();
            }
        }, { threshold: 0.1 });
        observer.observe(node);
        return {
            destroy() {
                observer.disconnect();
            }
        };
    }

    let selectedCorrelationId = $state<string>("");
    let selectedRunDetails = $state<any>(null);
    let isLoadingRuns = $state(false);
    let isLoadingDetails = $state(false);
    let selectedIntegrationFilter = $state<string>("ALL");
    let activeTab = $state<"transactions" | "logs">("transactions");
    let selectedRawLogFile = $state<any>(null);
    let expandedTxId = $state<string>("");

    function getStored(key: string, def: any) {
        if (typeof localStorage === "undefined") return def;
        const val = localStorage.getItem(key);
        if (val === null || val === "") return def;
        if (typeof def === "number" || (def === null && key.includes("Value"))) {
            const num = Number(val);
            return isNaN(num) ? def : num;
        }
        return val;
    }

    let filterTxsValue = $state<number | null>(getStored("sync_logs_filterTxsValue", null));
    let filterTxsOperator = $state<string>(getStored("sync_logs_filterTxsOperator", ">="));
    let showTxsPopover = $state(false);

    $effect(() => {
        if (typeof localStorage !== "undefined") {
            localStorage.setItem("sync_logs_filterTxsValue", filterTxsValue !== null ? String(filterTxsValue) : "");
            localStorage.setItem("sync_logs_filterTxsOperator", filterTxsOperator);
        }
    });

    // Get unique integrations from loaded runs for the filter dropdown
    let integrations = $derived(() => {
        const unique = new Map<string, string>();
        runs.forEach(r => {
            if (r.integrationId && r.integrationName) {
                unique.set(r.integrationId, r.integrationName);
            }
        });
        return Array.from(unique.entries()).map(([id, name]) => ({ id, name }));
    });

    // Filter runs
    let filteredRuns = $derived(() => {
        let list = runs;
        if (selectedIntegrationFilter !== "ALL") {
            list = list.filter(r => r.integrationId === selectedIntegrationFilter);
        }

        if (filterTxsValue !== null) {
            const limit = filterTxsValue;
            list = list.filter(r => {
                const count = r.transactionCount || 0;
                switch (filterTxsOperator) {
                    case ">": return count > limit;
                    case "<": return count < limit;
                    case "=": return count === limit;
                    case ">=": return count >= limit;
                    case "<=": return count <= limit;
                    default: return true;
                }
            });
        }

        return list;
    });

    onMount(async () => {
        await loadSyncRuns();
    });

    async function loadSyncRuns() {
        isLoadingRuns = true;
        if (offset === 0) {
            runs = [];
            metrics = null;
            hasMore = true;
        }
        try {
            const callResult = wsCall(
                "system::sync_runs",
                api.SyncRunsRequestSchema,
                { offset, limit },
                [api.SyncRunSchema],
                { timeout: 60000 }
            );
            let receivedCount = 0;
            for await (const [run, err] of callResult.many()) {
                if (err) {
                    console.error("[SYNC_LOGS] Failed to load sync runs chunk:", err);
                    break;
                }
                if (run) {
                    if (run.isMetrics) {
                        metrics = run.metrics;
                    } else {
                        receivedCount++;
                        const existingIdx = runs.findIndex(r => r.correlationId === run.correlationId);
                        if (existingIdx !== -1) {
                            runs[existingIdx].transactionCount = run.transactionCount;
                            runs[existingIdx].status = run.status;
                        } else {
                            runs.push(run);
                        }
                        runs = [...runs].sort((a, b) => {
                            const tA = a.timestamp ? new Date(a.timestamp).getTime() : 0;
                            const tB = b.timestamp ? new Date(b.timestamp).getTime() : 0;
                            return tB - tA;
                        });
                    }
                }
            }
            if (receivedCount < limit) {
                hasMore = false;
            } else {
                hasMore = true;
            }
        } catch (err) {
            console.error("[SYNC_LOGS] Failed to load sync runs:", err);
        } finally {
            isLoadingRuns = false;
        }
    }

    async function loadRunDetails(correlationId: string) {
        selectedCorrelationId = correlationId;
        isLoadingDetails = true;
        selectedRunDetails = null;
        selectedRawLogFile = null;
        expandedTxId = "";
        try {
            const [res, err] = await wsCall(
                "system::sync_run_details",
                api.SyncRunDetailsRequestSchema,
                { correlationId },
                [api.SyncRunDetailsResponseSchema],
                { timeout: 60000 }
            ).one();
            if (err) {
                console.error("[SYNC_LOGS] Failed to load details:", err);
                return;
            }
            if (res) {
                selectedRunDetails = res;
                if (res.rawLogs && res.rawLogs.length > 0) {
                    selectedRawLogFile = res.rawLogs[0];
                }
            }
        } catch (err) {
            console.error("[SYNC_LOGS] Failed to load details:", err);
        } finally {
            isLoadingDetails = false;
        }
    }

    function selectRun(correlationId: string) {
        loadRunDetails(correlationId);
    }

    function toggleTxExpand(txId: string) {
        if (expandedTxId === txId) {
            expandedTxId = "";
        } else {
            expandedTxId = txId;
        }
    }

    function formatAmount(amount: number, currency: string) {
        const formatted = new Intl.NumberFormat("de-DE", {
            minimumFractionDigits: 2,
            maximumFractionDigits: 2
        }).format(amount);
        const symbol = currency === "EUR" || !currency ? "€" : currency;
        return { symbol, formatted };
    }

    function formatDate(rfcString: string) {
        if (!rfcString) return "-";
        try {
            const d = new Date(rfcString);
            return d.toLocaleString("de-DE", {
                day: "2-digit",
                month: "2-digit",
                year: "numeric",
                hour: "2-digit",
                minute: "2-digit",
                second: "2-digit"
            });
        } catch {
            return rfcString;
        }
    }

    function prettyPrintJSON(jsonStr: string): string {
        try {
            const obj = JSON.parse(jsonStr);
            return JSON.stringify(obj, null, 2);
        } catch {
            return jsonStr;
        }
    }
</script>

<div class="min-h-screen bg-slate-950 pb-12 pt-8 px-4 md:px-8 font-sans text-slate-100">
    <div class="max-w-[1600px] mx-auto space-y-8">
        
        <!-- Header -->
        <header class="flex flex-col lg:flex-row lg:items-end justify-between gap-8">
            <div class="space-y-2">
                <a href="/sysadmin" class="flex items-center gap-2 text-slate-400 hover:text-white transition-colors text-xs font-black uppercase tracking-widest mb-4">
                    <ChevronLeft class="w-4 h-4" />
                    Back to Diagnostics
                </a>
                <h1 class="text-5xl font-black tracking-tight text-white">
                    Sync Log <span class="bg-gradient-to-r from-indigo-400 via-purple-400 to-pink-400 bg-clip-text text-transparent">Parser</span>.
                </h1>
                <p class="text-slate-400 text-sm font-medium">
                    Inspect external bank API payloads, trace detected transactions, and audit raw JSON responses.
                </p>
            </div>

            <!-- Integration Filter Dropdown -->
            <div class="flex flex-col sm:flex-row items-stretch sm:items-center gap-4">
                <div class="flex flex-col gap-1.5">
                    <span class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Filter by Integration</span>
                    <div class="relative min-w-[240px]">
                        <select 
                            bind:value={selectedIntegrationFilter}
                            class="w-full bg-slate-900 border border-slate-700/60 rounded-xl px-4 py-2.5 text-sm font-bold text-slate-200 focus:outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500 appearance-none cursor-pointer"
                        >
                            <option value="ALL">All Integrations</option>
                            {#each integrations() as int}
                                <option value={int.id}>{int.name}</option>
                            {/each}
                        </select>
                        <div class="absolute inset-y-0 right-0 flex items-center pr-3 pointer-events-none text-slate-400">
                            <ChevronRight class="w-4 h-4 rotate-90" />
                        </div>
                    </div>
                </div>

                <div class="flex flex-col gap-1.5">
                    <span class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">Filter by Txs Detected</span>
                    <div class="relative inline-block">
                        <button
                            onclick={() => (showTxsPopover = !showTxsPopover)}
                            class="flex items-center gap-2 px-4 py-2.5 bg-slate-900 border {filterTxsValue !== null ? 'border-indigo-500 text-indigo-400 font-black' : 'border-slate-700/60 text-slate-400'} rounded-xl text-sm font-bold hover:border-indigo-500 hover:text-indigo-400 transition-all cursor-pointer shadow-sm shrink-0"
                        >
                            <Filter class="w-4 h-4 {filterTxsValue !== null ? 'text-indigo-400' : 'text-slate-500'}" />
                            <span>
                                {filterTxsValue !== null 
                                    ? `${filterTxsOperator} ${filterTxsValue} Txs`
                                    : "Any Txs Detected"}
                            </span>
                        </button>
                        {#if showTxsPopover}
                            <div
                                role="button"
                                aria-label="Close amount popover"
                                tabindex="-1"
                                class="fixed inset-0 z-40"
                                onclick={() => (showTxsPopover = false)}
                                onkeydown={() => (showTxsPopover = false)}
                            ></div>
                            <div
                                class="absolute top-full right-0 mt-2 w-[240px] bg-slate-900 border border-slate-800 rounded-2xl shadow-2xl p-5 z-50 space-y-4"
                                transition:fade
                            >
                                <div
                                    class="flex items-center justify-between border-b border-slate-800 pb-2"
                                >
                                    <span class="text-[9px] font-black uppercase text-slate-400">Txs Detected Filter</span>
                                    {#if filterTxsValue !== null}
                                        <button
                                            onclick={() => (filterTxsValue = null)}
                                            class="text-[8px] font-black text-rose-500 uppercase hover:underline"
                                        >Clear</button>
                                    {/if}
                                </div>
                                <div class="grid grid-cols-5 gap-1.5">
                                    {#each [">", "<", "=", ">=", "<="] as op}
                                        <button
                                            onclick={() => (filterTxsOperator = op)}
                                            class="px-2 py-2 rounded-lg border text-[10px] font-black transition-all {filterTxsOperator === op
                                                ? 'bg-indigo-600 border-indigo-600 text-white shadow-lg shadow-indigo-900/20'
                                                : 'bg-slate-800 border-slate-700 text-slate-400 hover:border-indigo-500/50 hover:text-indigo-400'}"
                                        >
                                            {op}
                                        </button>
                                    {/each}
                                </div>
                                <div class="relative">
                                    <input
                                        type="number"
                                        bind:value={filterTxsValue}
                                        placeholder="0"
                                        class="w-full px-4 py-2 bg-slate-950 border border-slate-800 rounded-xl font-bold text-xs text-slate-200 outline-none focus:ring-2 focus:ring-indigo-500/40 focus:border-indigo-500 transition-all placeholder:text-slate-600"
                                    />
                                </div>
                            </div>
                        {/if}
                    </div>
                </div>
            </div>
        </header>

        <!-- Main Workspace -->
        <div class="grid grid-cols-1 lg:grid-cols-12 gap-8 items-start">
            
            <!-- Left Pane: Runs List -->
            <div class="lg:col-span-5 bg-slate-900 border border-slate-800/80 rounded-2xl p-6 h-[750px] flex flex-col space-y-4">
                <div class="flex items-center justify-between">
                    <h2 class="text-xs font-black uppercase tracking-[0.2em] text-slate-400">Sync Runs History ({filteredRuns().length})</h2>
                    {#if isLoadingRuns}
                        <div class="flex items-center gap-2 text-indigo-400 text-xs font-bold">
                            <div class="w-3 h-3 border-2 border-indigo-400 border-t-transparent rounded-full animate-spin"></div>
                            Loading...
                        </div>
                    {/if}
                </div>

                {#if metrics}
                    <div class="grid grid-cols-2 gap-3 p-3 bg-slate-950/60 border border-slate-800/60 rounded-xl text-[9px] font-bold text-slate-400 shadow-inner">
                        <div class="space-y-0.5">
                            <div class="text-slate-500 uppercase tracking-wider text-[8px]">CPU Utilization</div>
                            <div class="text-indigo-400 font-mono">{metrics.cpuUtilization.toFixed(1)}%</div>
                        </div>
                        <div class="space-y-0.5">
                            <div class="text-slate-500 uppercase tracking-wider text-[8px]">Mem RSS</div>
                            <div class="text-indigo-400 font-mono">{formatBytes(metrics.memoryRssBytes)}</div>
                        </div>
                        <div class="space-y-0.5">
                            <div class="text-slate-500 uppercase tracking-wider text-[8px]">File IO Ops</div>
                            <div class="text-indigo-400 font-mono">{metrics.fileIoReadOperations} reads ({formatBytes(metrics.fileIoReadBytes)})</div>
                        </div>
                        <div class="space-y-0.5">
                            <div class="text-slate-500 uppercase tracking-wider text-[8px]">IO Wait Time</div>
                            <div class="text-indigo-400 font-mono">{metrics.totalIoDurationMs}ms</div>
                        </div>
                    </div>
                {/if}

                <!-- Run Cards Container -->
                <div class="flex-1 overflow-y-auto space-y-3 pr-1">
                    {#if filteredRuns().length === 0}
                        <div class="h-full flex flex-col items-center justify-center text-center p-8 space-y-3">
                            <AlertCircle class="w-8 h-8 text-slate-500" />
                            <p class="text-sm font-bold text-slate-400">No sync runs found</p>
                        </div>
                    {:else}
                        {#each filteredRuns() as run (run.correlationId)}
                            <button
                                onclick={() => selectRun(run.correlationId)}
                                class="w-full text-left p-4 rounded-xl border transition-all duration-200 flex items-center justify-between gap-4 group
                                    {selectedCorrelationId === run.correlationId 
                                        ? 'bg-indigo-600/15 border-indigo-500/80 shadow-lg shadow-indigo-950/20' 
                                        : 'bg-slate-900/40 border-slate-800/60 hover:bg-slate-800/40 hover:border-slate-700/60'}"
                            >
                                <div class="space-y-1.5 min-w-0">
                                    <div class="flex items-center gap-2">
                                        <span class="font-bold text-sm text-slate-200 truncate group-hover:text-white transition-colors">
                                            {run.integrationName || "Unknown Integration"}
                                        </span>
                                        <span class="text-[9px] font-black uppercase tracking-wider px-1.5 py-0.5 rounded bg-slate-800 border border-slate-700 text-slate-400">
                                            {run.serviceType}
                                        </span>
                                    </div>
                                    <div class="flex items-center gap-3 text-xs text-slate-400">
                                        <span class="flex items-center gap-1">
                                            <Calendar class="w-3.5 h-3.5 text-slate-500" />
                                            {formatDate(run.timestamp)}
                                        </span>
                                    </div>
                                    <div class="text-[10px] font-mono text-slate-500 truncate max-w-[250px]">
                                        CID: {run.correlationId}
                                    </div>
                                </div>

                                <div class="flex flex-col items-end gap-2 shrink-0">
                                    <!-- Status Badge -->
                                    <span class="inline-flex items-center gap-1 text-[10px] font-black uppercase tracking-wider px-2 py-0.5 rounded-full
                                        {run.status === 'COMPLETED' ? 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20' : 
                                         run.status === 'FAILED' ? 'bg-rose-500/10 text-rose-400 border border-rose-500/20' : 
                                         'bg-indigo-500/10 text-indigo-400 border border-indigo-500/20'}">
                                        {#if run.status === 'COMPLETED'}
                                            <CheckCircle2 class="w-3 h-3" />
                                        {:else if run.status === 'FAILED'}
                                            <XCircle class="w-3 h-3" />
                                        {:else}
                                            <Activity class="w-3 h-3 animate-pulse" />
                                        {/if}
                                        {run.status}
                                    </span>

                                    <!-- Transaction Discovered Count -->
                                    {#if run.transactionCount === -1}
                                        <span class="text-[10px] font-bold text-indigo-400 bg-indigo-500/10 border border-indigo-500/20 px-2 py-0.5 rounded-md flex items-center gap-1.5">
                                            <div class="w-2.5 h-2.5 border border-indigo-400 border-t-transparent rounded-full animate-spin"></div>
                                            Scanning...
                                        </span>
                                    {:else if run.transactionCount > 0}
                                        <span class="text-[10px] font-bold text-indigo-300 bg-indigo-500/15 border border-indigo-500/20 px-2 py-0.5 rounded-md">
                                            {run.transactionCount} txs detected
                                        </span>
                                    {:else}
                                        <span class="text-[10px] font-bold text-slate-500 bg-slate-800/40 border border-slate-700/40 px-2 py-0.5 rounded-md">
                                            0 txs
                                        </span>
                                    {/if}
                                </div>
                            </button>
                        {/each}
                        {#if hasMore}
                            <div use:lazyLoadNextPage class="py-4 flex items-center justify-center">
                                <div class="w-5 h-5 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin"></div>
                            </div>
                        {/if}
                    {/if}
                </div>
            </div>

            <!-- Right Pane: Details, Transactions & Raw Logs -->
            <div class="lg:col-span-7 bg-slate-900 border border-slate-800/80 rounded-2xl p-6 h-[750px] flex flex-col">
                {#if isLoadingDetails}
                    <div class="h-full flex flex-col items-center justify-center space-y-4">
                        <div class="w-8 h-8 border-4 border-indigo-500 border-t-transparent rounded-full animate-spin"></div>
                        <p class="text-sm font-bold text-slate-400">Loading sync run details...</p>
                    </div>
                {:else if !selectedRunDetails}
                    <!-- Empty State -->
                    <div class="h-full flex flex-col items-center justify-center text-center p-8 space-y-4">
                        <div class="w-16 h-16 rounded-full bg-slate-850 border border-slate-800 flex items-center justify-center text-slate-400 shadow-inner">
                            <Terminal class="w-8 h-8" />
                        </div>
                        <div class="max-w-md space-y-2">
                            <h3 class="text-lg font-black text-slate-200">No Run Selected</h3>
                            <p class="text-sm text-slate-400 font-medium">
                                Choose a sync run from the history on the left to parse detected transactions, audit JSON request/response payloads, and view transaction occurrences.
                            </p>
                        </div>
                    </div>
                {:else}
                    <!-- Details Header -->
                    <div class="space-y-4 pb-4 border-b border-slate-800/60 shrink-0">
                        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
                            <div class="space-y-1">
                                <h3 class="text-xl font-black text-slate-100 flex items-center gap-2">
                                    {selectedRunDetails.integrationName}
                                    <span class="text-xs font-black uppercase tracking-wider px-2 py-0.5 rounded bg-slate-800 border border-slate-700 text-slate-400">
                                        {selectedRunDetails.serviceType}
                                    </span>
                                </h3>
                                <p class="text-xs font-mono text-slate-500">Correlation ID: {selectedRunDetails.correlationId}</p>
                            </div>
                            <div class="text-right">
                                <span class="text-xs text-slate-400 font-medium block">Timestamp</span>
                                <span class="text-sm font-bold text-slate-200">{formatDate(selectedRunDetails.timestamp)}</span>
                            </div>
                        </div>

                        <!-- Tab Selector -->
                        <div class="flex border-b border-slate-800">
                            <button
                                onclick={() => activeTab = "transactions"}
                                class="px-5 py-2.5 text-xs font-black uppercase tracking-wider border-b-2 transition-all duration-200 flex items-center gap-2
                                    {activeTab === 'transactions' 
                                        ? 'border-indigo-500 text-indigo-400' 
                                        : 'border-transparent text-slate-400 hover:text-slate-200'}"
                            >
                                <Layers class="w-4 h-4" />
                                Detected Transactions ({selectedRunDetails.detectedTransactions?.length || 0})
                            </button>
                            <button
                                onclick={() => activeTab = "logs"}
                                class="px-5 py-2.5 text-xs font-black uppercase tracking-wider border-b-2 transition-all duration-200 flex items-center gap-2
                                    {activeTab === 'logs' 
                                        ? 'border-indigo-500 text-indigo-400' 
                                        : 'border-transparent text-slate-400 hover:text-slate-200'}"
                            >
                                <FileText class="w-4 h-4" />
                                Raw HTTP Payloads ({selectedRunDetails.rawLogs?.length || 0})
                            </button>
                        </div>
                    </div>

                    <!-- Details Body -->
                    <div class="flex-1 min-h-0 overflow-y-auto pt-4">
                        
                        <!-- TAB 1: Detected Transactions -->
                        {#if activeTab === "transactions"}
                            <div class="space-y-4">
                                {#if !selectedRunDetails.detectedTransactions || selectedRunDetails.detectedTransactions.length === 0}
                                    <div class="p-8 text-center bg-slate-900/30 border border-slate-800/40 rounded-xl space-y-2">
                                        <AlertCircle class="w-6 h-6 text-slate-500 mx-auto" />
                                        <p class="text-sm font-bold text-slate-400">No transactions detected in logs</p>
                                        <p class="text-xs text-slate-500">This run completed with no new or pending transactions returned from the provider.</p>
                                    </div>
                                {:else}
                                    {#each selectedRunDetails.detectedTransactions as tx (tx.externalId)}
                                        {@const fmt = formatAmount(tx.amount, tx.currency)}
                                        <div class="bg-slate-900/40 border border-slate-800/60 rounded-xl overflow-hidden transition-all duration-200">
                                            <!-- Transaction Card Row -->
                                            <button 
                                                onclick={() => toggleTxExpand(tx.externalId)}
                                                class="w-full text-left p-4 flex items-center justify-between gap-4 hover:bg-slate-800/30 transition-colors"
                                            >
                                                <div class="min-w-0 space-y-1">
                                                    <p class="text-sm font-bold text-slate-200 truncate">{tx.description || "(No Description)"}</p>
                                                    <p class="text-xs text-slate-400 font-bold flex items-center gap-1.5">
                                                        {#if tx.peer}
                                                            <span class="text-indigo-400">Peer:</span> {tx.peer}
                                                        {/if}
                                                        <span class="text-slate-600">|</span>
                                                        <span class="text-slate-500">{tx.bookingDate || tx.valueDate || "No Date"}</span>
                                                    </p>
                                                </div>

                                                <div class="flex items-center gap-4 shrink-0">
                                                    <!-- Tabular Numbers Right-Aligned -->
                                                    <div class="text-right font-mono tabular-nums">
                                                        <span class="text-[10px] text-slate-400 font-black tracking-widest mr-1">{fmt.symbol}</span>
                                                        <span class="text-base font-black {tx.amount < 0 ? 'text-rose-400' : 'text-emerald-400'}">{fmt.formatted}</span>
                                                    </div>
                                                    <ChevronRight class="w-4 h-4 text-slate-500 transform transition-transform duration-200 {expandedTxId === tx.externalId ? 'rotate-90' : ''}" />
                                                </div>
                                            </button>

                                            <!-- Expanded Content -->
                                            {#if expandedTxId === tx.externalId}
                                                <div class="border-t border-slate-800/60 p-4 bg-slate-950/40 space-y-4" transition:slide={{ duration: 200 }}>
                                                    
                                                    <!-- Raw JSON section -->
                                                    <div class="space-y-2">
                                                        <span class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 block">Raw JSON Payload</span>
                                                        <pre class="bg-slate-950 border border-slate-800 text-xs font-mono p-4 rounded-lg overflow-x-auto max-h-[300px] text-slate-300 select-all">{prettyPrintJSON(tx.rawJson)}</pre>
                                                    </div>

                                                    <!-- Other Sync Runs section -->
                                                    <div class="space-y-2">
                                                        <span class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 block">
                                                            Linked Other Sync Runs (found in {tx.otherSyncRuns?.length || 0} other runs)
                                                        </span>
                                                        {#if !tx.otherSyncRuns || tx.otherSyncRuns.length === 0}
                                                            <p class="text-xs text-slate-500 font-medium">This transaction has only been seen in the current sync run.</p>
                                                        {:else}
                                                            <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
                                                                {#each tx.otherSyncRuns as other}
                                                                    <button
                                                                        onclick={() => selectRun(other.correlationId)}
                                                                        class="flex items-center justify-between p-3 rounded-lg border border-slate-800 bg-slate-900/30 hover:bg-indigo-600/10 hover:border-indigo-500/50 transition-all text-left group"
                                                                    >
                                                                        <div class="min-w-0">
                                                                            <span class="text-xs font-bold text-slate-300 group-hover:text-white block truncate">
                                                                                {other.integrationName}
                                                                            </span>
                                                                            <span class="text-[10px] text-slate-500">
                                                                                {formatDate(other.timestamp)}
                                                                            </span>
                                                                        </div>
                                                                        <ExternalLink class="w-3.5 h-3.5 text-slate-500 group-hover:text-indigo-400 shrink-0" />
                                                                    </button>
                                                                {/each}
                                                            </div>
                                                        {/if}
                                                    </div>

                                                </div>
                                            {/if}
                                        </div>
                                    {/each}
                                {/if}
                            </div>
                        
                        <!-- TAB 2: Raw Log Payloads -->
                        {:else if activeTab === "logs"}
                            {#if !selectedRunDetails.rawLogs || selectedRunDetails.rawLogs.length === 0}
                                <div class="p-8 text-center bg-slate-900/30 border border-slate-800/40 rounded-xl space-y-2">
                                    <AlertCircle class="w-6 h-6 text-slate-500 mx-auto" />
                                    <p class="text-sm font-bold text-slate-400">No log files available</p>
                                </div>
                            {:else}
                                <div class="grid grid-cols-1 md:grid-cols-12 gap-6 h-full min-h-0 items-stretch">
                                    <!-- Log File Selector -->
                                    <div class="md:col-span-4 space-y-2 max-h-[500px] overflow-y-auto pr-1 shrink-0">
                                        <span class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 block">Payload Files</span>
                                        {#each selectedRunDetails.rawLogs as logFile}
                                            <button
                                                onclick={() => selectedRawLogFile = logFile}
                                                class="w-full text-left p-3 rounded-lg border transition-all text-xs font-bold truncate block
                                                    {selectedRawLogFile?.filename === logFile.filename
                                                        ? 'bg-indigo-600/15 border-indigo-500/60 text-indigo-300' 
                                                        : 'bg-slate-900/40 border-slate-800/60 hover:bg-slate-800/40 hover:border-slate-700/60 text-slate-300'}"
                                            >
                                                {logFile.filename}
                                            </button>
                                        {/each}
                                    </div>

                                    <!-- Log Viewer Panel -->
                                    <div class="md:col-span-8 flex flex-col min-h-0 h-full max-h-[500px]">
                                        {#if !selectedRawLogFile}
                                            <div class="h-full flex items-center justify-center text-slate-500 text-xs font-bold border border-dashed border-slate-800 rounded-xl p-6">
                                                Select a request or response file
                                            </div>
                                        {:else}
                                            <div class="flex-1 flex flex-col min-h-0 border border-slate-800 rounded-xl bg-slate-950 overflow-hidden">
                                                <!-- Viewer Header -->
                                                <div class="bg-slate-900/80 px-4 py-2 border-b border-slate-800 flex items-center justify-between shrink-0">
                                                    <span class="text-[10px] font-mono text-slate-400 font-bold">{selectedRawLogFile.filename}</span>
                                                    <span class="text-[9px] font-black uppercase tracking-wider px-2 py-0.5 rounded
                                                        {selectedRawLogFile.isRequest ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20' : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'}">
                                                        {selectedRawLogFile.isRequest ? 'REQUEST' : 'RESPONSE'}
                                                    </span>
                                                </div>
                                                <!-- Viewer Content -->
                                                <div class="flex-1 min-h-0 overflow-auto p-4 text-xs font-mono text-slate-300 select-all">
                                                    <pre class="whitespace-pre">{prettyPrintJSON(selectedRawLogFile.content)}</pre>
                                                </div>
                                            </div>
                                        {/if}
                                    </div>
                                </div>
                            {/if}
                        {/if}

                    </div>
                {/if}
            </div>

        </div>
    </div>
</div>

<style>
    /* Tabular numbers for vertical aligning decimal values */
    .tabular-nums {
        font-variant-numeric: tabular-nums;
    }
</style>
