<script lang="ts">
    import { fade, slide } from "svelte/transition";
    import {
        Euro,
        Shield,
        Zap,
        Boxes,
        History,
        Settings2,
        Waves,
        X,
        CheckCircle2,
        Plus,
        ChevronUp,
        ChevronDown,
        Activity,
    } from "@lucide/svelte";

    let {
        open = $bindable(false),
        activeScenario = $bindable<any>(null),
        scopeTab = $bindable<"INCOME" | "BILL" | "EXPENSE" | "ASSET" | "LOAN" | "MODIFICATION" | "WATERFALL">("INCOME"),
        allIncomes = [],
        allBills = [],
        allExpenses = [],
        allAssets = [],
        allLoans = [],
        allModifications = [],
        onSave = async () => {},
        selectAllOfType = (type: string) => {},
        deselectAllOfType = (type: string) => {},
        toggleEntity = (id: string, type: string, versionId: string) => {},
        toggleInRemainderOrder = (id: string) => {},
        moveInRemainderOrder = (idx: number, dir: "up" | "down") => {},
        getID = (entity: any) => entity?.id || "",
        getName = (entity: any) => entity?.name || "",
        getActiveVersion = (entity: any) => entity?.activeVersion || null,
        getSubAssets = (entity: any) => entity?.activeVersion?.subAssets || [],
    } = $props<{
        open: boolean;
        activeScenario: any;
        scopeTab: "INCOME" | "BILL" | "EXPENSE" | "ASSET" | "LOAN" | "MODIFICATION" | "WATERFALL";
        allIncomes: any[];
        allBills: any[];
        allExpenses: any[];
        allAssets: any[];
        allLoans: any[];
        allModifications: any[];
        onSave: () => Promise<void>;
        selectAllOfType: (type: string) => void;
        deselectAllOfType: (type: string) => void;
        toggleEntity: (id: string, type: string, versionId: string) => void;
        toggleInRemainderOrder: (id: string) => void;
        moveInRemainderOrder: (idx: number, dir: "up" | "down") => void;
        getID?: (entity: any) => string;
        getName?: (entity: any) => string;
        getActiveVersion?: (entity: any) => any;
        getSubAssets?: (entity: any) => any[];
    }>();
</script>

{#if open && activeScenario}
    <div
        class="fixed inset-0 bg-slate-900/80 z-50 flex items-center justify-center p-4 md:p-8"
        transition:fade={{ duration: 150 }}
    >
        <!-- Modal Container -->
        <div
            class="bg-white dark-budget-modal rounded-[32px] shadow-2xl border border-slate-100 dark:border-slate-800 max-w-6xl w-full max-h-[85vh] relative overflow-hidden flex flex-row"
            transition:slide={{ duration: 200 }}
        >
            <!-- Navigation Sidebar -->
            <div class="w-72 bg-slate-50/50 dark:bg-slate-900/50 border-r border-slate-100 dark:border-slate-800 flex flex-col">
                <div class="p-8">
                    <h3 class="text-xl font-black text-slate-900 dark:text-white leading-none">
                        Logic <span class="text-indigo-600">Scope</span>.
                    </h3>
                    <p class="text-[10px] text-slate-400 font-bold uppercase tracking-wider mt-2">
                        Fine-tune deterministic reach
                    </p>
                </div>

                <nav class="flex-1 px-4 space-y-1">
                    {#each [
                        { id: 'INCOME', label: 'Incomes', icon: Euro, items: allIncomes },
                        { id: 'BILL', label: 'Bills', icon: Shield, items: allBills },
                        { id: 'EXPENSE', label: 'Expenses', icon: Zap, items: allExpenses },
                        { id: 'ASSET', label: 'Assets', icon: Boxes, items: allAssets },
                        { id: 'LOAN', label: 'Loans', icon: History, items: allLoans },
                        { id: 'MODIFICATION', label: 'Modifications', icon: Settings2, items: allModifications },
                        { id: 'WATERFALL', label: 'Waterfall', icon: Waves, items: activeScenario.remainderOrder }
                    ] as tab}
                        <button
                            onclick={() => scopeTab = tab.id as any}
                            class="w-full flex items-center justify-between px-4 py-3 rounded-2xl transition-all group
                                {scopeTab === tab.id 
                                ? 'bg-white dark:bg-indigo-600 shadow-sm border border-slate-100 dark:border-indigo-500 text-indigo-600 dark:text-white' 
                                : 'text-slate-500 dark:text-slate-400 hover:bg-white/50 dark:hover:bg-slate-800 hover:text-slate-900 dark:hover:text-slate-200'}"
                        >
                            <div class="flex items-center gap-3">
                                <tab.icon class="w-4 h-4 {scopeTab === tab.id ? 'text-indigo-600 dark:text-white' : 'text-slate-400 group-hover:text-slate-600 dark:group-hover:text-slate-300'}" />
                                <span class="text-xs font-black uppercase tracking-wider">{tab.label}</span>
                            </div>
                            
                            {#if tab.id !== 'WATERFALL'}
                                {@const activeCount = activeScenario.entities.length === 0 
                                    ? tab.items.length 
                                    : tab.items.filter((i: any) => activeScenario.entities.some((e: any) => e.entityId === getID(i) && e.entityType === tab.id)).length}
                                <span class="text-[10px] font-black {scopeTab === tab.id ? 'text-indigo-400 dark:text-indigo-200' : 'text-slate-300 dark:text-slate-600'}">
                                    {activeCount}/{tab.items.length}
                                </span>
                            {:else}
                                <span class="text-[10px] font-black {scopeTab === tab.id ? 'text-indigo-400 dark:text-indigo-200' : 'text-slate-300 dark:text-slate-600'}">
                                    {activeScenario.remainderOrder.length}
                                </span>
                            {/if}
                        </button>
                    {/each}
                </nav>

                <div class="p-8 border-t border-slate-100 dark:border-slate-800">
                    <button
                        onclick={async () => {
                            await onSave();
                            open = false;
                        }}
                        class="w-full py-4 bg-slate-900 dark:bg-indigo-600 text-white rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] hover:bg-indigo-600 dark:hover:bg-indigo-700 transition-all shadow-xl shadow-slate-200 dark:shadow-none"
                    >
                        Apply & Run
                    </button>
                </div>
            </div>

            <!-- Content Area -->
            <div class="flex-1 flex flex-col min-w-0 bg-white dark:bg-[#090d16]">
                <header class="px-10 py-8 border-b border-slate-50 dark:border-slate-800 flex items-center justify-between">
                    <div>
                        <h4 class="text-xs font-black uppercase tracking-[0.2em] text-slate-400">
                            Current Scope: <span class="text-slate-900 dark:text-white">{scopeTab}</span>
                        </h4>
                    </div>
                    
                    {#if scopeTab !== 'WATERFALL'}
                        <div class="flex items-center gap-4">
                            <button 
                                onclick={() => selectAllOfType(scopeTab)}
                                class="text-[10px] font-black uppercase tracking-wider text-indigo-600 dark:text-indigo-400 hover:text-indigo-700 dark:hover:text-indigo-300 cursor-pointer px-3 py-1.5 rounded-lg hover:bg-indigo-50 dark:hover:bg-indigo-900/30 transition-all"
                            >
                                Include All
                            </button>
                            <div class="w-px h-4 bg-slate-100 dark:bg-slate-800"></div>
                            <button 
                                onclick={() => deselectAllOfType(scopeTab)}
                                class="text-[10px] font-black uppercase tracking-wider text-slate-400 dark:text-slate-500 hover:text-rose-600 dark:hover:text-rose-400 cursor-pointer px-3 py-1.5 rounded-lg hover:bg-rose-50 dark:hover:bg-rose-900/30 transition-all"
                            >
                                Exclude All
                            </button>
                        </div>
                    {/if}

                    <button
                        onclick={() => open = false}
                        class="p-2 rounded-xl hover:bg-slate-100 dark:hover:bg-slate-800 text-slate-400 hover:text-slate-600 dark:hover:text-slate-200 transition-all cursor-pointer"
                    >
                        <X class="w-5 h-5" />
                    </button>
                </header>

                <div class="flex-1 overflow-y-auto p-10 scrollbar-thin">
                    {#if scopeTab === 'INCOME'}
                        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            {#each allIncomes as inc}
                                {@const isIncluded = activeScenario.entities.length === 0 || activeScenario.entities.some((e: any) => e.entityId === getID(inc) && e.entityType === 'INCOME')}
                                <button
                                    onclick={() => toggleEntity(getID(inc), 'INCOME', inc.activeVersion?.id || "")}
                                    class="flex items-center justify-between p-4 rounded-2xl border transition-all text-left group
                                        {isIncluded 
                                        ? 'bg-indigo-600 border-indigo-500 text-white shadow-lg shadow-indigo-200 dark:shadow-none' 
                                        : 'bg-white dark:bg-slate-900 border-slate-100 dark:border-slate-800 text-slate-500 dark:text-slate-400 hover:border-indigo-200 dark:hover:border-indigo-500'}"
                                >
                                    <span class="text-xs font-bold truncate pr-4">{getName(inc)}</span>
                                    <div class="w-5 h-5 rounded-full border-2 flex items-center justify-center transition-all
                                        {isIncluded ? 'bg-white border-white text-indigo-600' : 'border-slate-200 dark:border-slate-700 group-hover:border-indigo-300'}">
                                        {#if isIncluded}<CheckCircle2 class="w-3 h-3" />{/if}
                                    </div>
                                </button>
                            {/each}
                        </div>
                    {:else if scopeTab === 'BILL'}
                        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            {#each allBills as bill}
                                {@const isIncluded = activeScenario.entities.length === 0 || activeScenario.entities.some((e: any) => e.entityId === getID(bill) && e.entityType === 'BILL')}
                                <button
                                    onclick={() => toggleEntity(getID(bill), 'BILL', bill.activeVersion?.id || "")}
                                    class="flex items-center justify-between p-4 rounded-2xl border transition-all text-left group
                                        {isIncluded 
                                        ? 'bg-indigo-600 border-indigo-500 text-white shadow-lg shadow-indigo-200 dark:shadow-none' 
                                        : 'bg-white dark:bg-slate-900 border-slate-100 dark:border-slate-800 text-slate-500 dark:text-slate-400 hover:border-indigo-200 dark:hover:border-indigo-500'}"
                                >
                                    <span class="text-xs font-bold truncate pr-4">{getName(bill)}</span>
                                    <div class="w-5 h-5 rounded-full border-2 flex items-center justify-center transition-all
                                        {isIncluded ? 'bg-white border-white text-indigo-600' : 'border-slate-200 dark:border-slate-700 group-hover:border-indigo-300'}">
                                        {#if isIncluded}<CheckCircle2 class="w-3 h-3" />{/if}
                                    </div>
                                </button>
                            {/each}
                        </div>
                    {:else if scopeTab === 'EXPENSE'}
                        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            {#each allExpenses as exp}
                                {@const isIncluded = activeScenario.entities.length === 0 || activeScenario.entities.some((e: any) => e.entityId === getID(exp) && e.entityType === 'EXPENSE')}
                                <button
                                    onclick={() => toggleEntity(getID(exp), 'EXPENSE', getActiveVersion(exp)?.id || "")}
                                    class="flex items-center justify-between p-4 rounded-2xl border transition-all text-left group
                                        {isIncluded 
                                        ? 'bg-indigo-600 border-indigo-500 text-white shadow-lg shadow-indigo-200 dark:shadow-none' 
                                        : 'bg-white dark:bg-slate-900 border-slate-100 dark:border-slate-800 text-slate-500 dark:text-slate-400 hover:border-indigo-200 dark:hover:border-indigo-500'}"
                                >
                                    <span class="text-xs font-bold truncate pr-4">{getName(exp)}</span>
                                    <div class="w-5 h-5 rounded-full border-2 flex items-center justify-center transition-all
                                        {isIncluded ? 'bg-white border-white text-indigo-600' : 'border-slate-200 dark:border-slate-700 group-hover:border-indigo-300'}">
                                        {#if isIncluded}<CheckCircle2 class="w-3 h-3" />{/if}
                                    </div>
                                </button>
                            {/each}
                        </div>
                    {:else if scopeTab === 'ASSET'}
                        <div class="space-y-6">
                            {#each allAssets as asset}
                                {@const assetID = getID(asset)}
                                {@const subAssets = getSubAssets(asset)}
                                {@const isIncluded = activeScenario.entities.length === 0 || activeScenario.entities.some((e: any) => e.entityId === assetID && e.entityType === 'ASSET')}
                                <div class="space-y-3">
                                    <button
                                        onclick={() => toggleEntity(assetID, 'ASSET', getActiveVersion(asset)?.id || getActiveVersion(asset)?.Id || "")}
                                        class="w-full flex items-center justify-between p-4 rounded-2xl border transition-all text-left group
                                            {isIncluded 
                                            ? 'bg-indigo-600 border-indigo-500 text-white shadow-md shadow-indigo-100 dark:shadow-none' 
                                            : 'bg-white dark:bg-slate-900 border-slate-100 dark:border-slate-800 text-slate-500 dark:text-slate-400 hover:border-indigo-200'}"
                                    >
                                        <div class="flex items-center gap-3">
                                            <div class="p-2 rounded-xl {isIncluded ? 'bg-white/20 text-white' : 'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-500'}">
                                                <Boxes class="w-4 h-4" />
                                            </div>
                                            <span class="text-xs font-black uppercase tracking-wider">{getName(asset)}</span>
                                        </div>
                                        <div class="w-5 h-5 rounded-full border-2 flex items-center justify-center transition-all
                                            {isIncluded ? 'bg-white border-white text-indigo-600' : 'border-slate-200 dark:border-slate-700 group-hover:border-indigo-300'}">
                                            {#if isIncluded}<CheckCircle2 class="w-3 h-3" />{/if}
                                        </div>
                                    </button>

                                    {#if subAssets.length > 0}
                                        <div class="pl-14 grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3">
                                            {#each subAssets as sa}
                                                {@const saID = getID(sa)}
                                                {@const saIncluded = activeScenario.entities.length === 0 || activeScenario.entities.some((e: any) => e.entityId === saID && e.entityType === 'SUB_ASSET')}
                                                <button
                                                    onclick={() => toggleEntity(saID, 'SUB_ASSET', "")}
                                                    class="flex items-center justify-between p-3 rounded-xl border transition-all text-left group
                                                        {saIncluded 
                                                        ? 'bg-emerald-600 border-emerald-500 text-white shadow-sm' 
                                                        : 'bg-white dark:bg-slate-900 border-slate-100 dark:border-slate-800 text-slate-400 dark:text-slate-500 opacity-60 hover:opacity-100'}"
                                                >
                                                    <span class="text-[10px] font-bold truncate pr-3">{getName(sa)}</span>
                                                    <div class="w-4 h-4 rounded-full border flex items-center justify-center transition-all
                                                        {saIncluded ? 'bg-white border-white text-emerald-600' : 'border-slate-200 dark:border-slate-700 group-hover:border-indigo-300'}">
                                                        {#if saIncluded}<CheckCircle2 class="w-2.5 h-2.5" />{/if}
                                                    </div>
                                                </button>
                                            {/each}
                                        </div>
                                    {/if}
                                </div>
                            {/each}
                        </div>
                    {:else if scopeTab === 'LOAN'}
                        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            {#each allLoans as loan}
                                {@const isIncluded = activeScenario.entities.length === 0 || activeScenario.entities.some((e: any) => e.entityId === getID(loan) && e.entityType === 'LOAN')}
                                <button
                                    onclick={() => toggleEntity(getID(loan), 'LOAN', loan.activeVersion?.id || "")}
                                    class="flex items-center justify-between p-4 rounded-2xl border transition-all text-left group
                                        {isIncluded 
                                        ? 'bg-indigo-600 border-indigo-500 text-white shadow-lg shadow-indigo-200 dark:shadow-none' 
                                        : 'bg-white dark:bg-slate-900 border-slate-100 dark:border-slate-800 text-slate-500 dark:text-slate-400 hover:border-indigo-200 dark:hover:border-indigo-500'}"
                                >
                                    <span class="text-xs font-bold truncate pr-4">{getName(loan)}</span>
                                    <div class="w-5 h-5 rounded-full border-2 flex items-center justify-center transition-all
                                        {isIncluded ? 'bg-white border-white text-indigo-600' : 'border-slate-200 dark:border-slate-700 group-hover:border-indigo-300'}">
                                        {#if isIncluded}<CheckCircle2 class="w-3 h-3" />{/if}
                                    </div>
                                </button>
                            {/each}
                        </div>
                    {:else if scopeTab === 'MODIFICATION'}
                        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                            {#each allModifications as mod}
                                {@const isIncluded = activeScenario.entities.length === 0 || activeScenario.entities.some((e: any) => e.entityId === getID(mod) && e.entityType === 'MODIFICATION')}
                                <button
                                    onclick={() => toggleEntity(getID(mod), 'MODIFICATION', mod.activeVersion?.id || "")}
                                    class="flex items-center justify-between p-4 rounded-2xl border transition-all text-left group
                                        {isIncluded 
                                        ? 'bg-indigo-600 border-indigo-500 text-white shadow-lg shadow-indigo-200 dark:shadow-none' 
                                        : 'bg-white dark:bg-slate-900 border-slate-100 dark:border-slate-800 text-slate-500 dark:text-slate-400 hover:border-indigo-200 dark:hover:border-indigo-500'}"
                                >
                                    <span class="text-xs font-bold truncate pr-4">{mod.description}</span>
                                    <div class="w-5 h-5 rounded-full border-2 flex items-center justify-center transition-all
                                        {isIncluded ? 'bg-white border-white text-indigo-600' : 'border-slate-200 dark:border-slate-700 group-hover:border-indigo-300'}">
                                        {#if isIncluded}<CheckCircle2 class="w-3 h-3" />{/if}
                                    </div>
                                </button>
                            {/each}
                        </div>
                    {:else if scopeTab === 'WATERFALL'}
                        <div class="space-y-6">
                            <div class="grid grid-cols-1 md:grid-cols-2 gap-10">
                                <!-- Available for Waterfall -->
                                <div class="space-y-4">
                                    <span class="text-[10px] font-black uppercase tracking-[0.15em] text-slate-400 dark:text-slate-500 ml-1">Available Reservoir Targets</span>
                                    <div class="flex flex-wrap gap-2.5">
                                        {#each [...allAssets, ...allLoans].filter(entity => !activeScenario.remainderOrder.includes(getID(entity))) as entity}
                                            <button
                                                onclick={() => toggleInRemainderOrder(getID(entity))}
                                                class="px-4 py-3 rounded-2xl border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 text-slate-600 dark:text-slate-400 text-xs font-bold hover:border-indigo-200 dark:hover:border-indigo-500 hover:bg-indigo-50/30 dark:hover:bg-indigo-900/10 transition-all cursor-pointer flex items-center gap-3 shadow-sm"
                                            >
                                                <Plus class="w-3.5 h-3.5 text-slate-400" />
                                                {getName(entity)}
                                            </button>
                                        {/each}
                                    </div>
                                </div>

                                <!-- Active Waterfall Order -->
                                <div class="space-y-4">
                                    <span class="text-[10px] font-black uppercase tracking-[0.15em] text-slate-400 dark:text-slate-500 ml-1">Active Priority Sequence</span>
                                    <div class="space-y-3">
                                        {#each activeScenario.remainderOrder as entityId, i}
                                            {@const entity = [...allAssets, ...allLoans].find(e => getID(e) === entityId)}
                                            {#if entity}
                                                <div class="flex items-center gap-4 p-4 bg-slate-50 dark:bg-slate-900/50 border border-slate-200 dark:border-slate-800 rounded-3xl group hover:border-indigo-200 dark:hover:border-indigo-500 transition-all shadow-sm">
                                                    <div class="flex flex-col gap-1.5">
                                                        <button
                                                            onclick={() => moveInRemainderOrder(i, 'up')}
                                                            disabled={i === 0}
                                                            class="p-1 hover:text-indigo-600 dark:hover:text-indigo-400 disabled:opacity-30 transition-colors"
                                                        >
                                                            <ChevronUp class="w-4 h-4" />
                                                        </button>
                                                        <button
                                                            onclick={() => moveInRemainderOrder(i, 'down')}
                                                            disabled={i === activeScenario.remainderOrder.length - 1}
                                                            class="p-1 hover:text-indigo-600 dark:hover:text-indigo-400 disabled:opacity-30 transition-colors"
                                                        >
                                                            <ChevronDown class="w-4 h-4" />
                                                        </button>
                                                    </div>
                                                    
                                                    <div class="w-8 h-8 rounded-xl bg-indigo-600 text-white flex items-center justify-center text-xs font-black shadow-lg shadow-indigo-200 dark:shadow-none">
                                                        {i + 1}
                                                    </div>

                                                    <span class="flex-1 text-xs font-black text-slate-900 dark:text-white uppercase truncate">
                                                        {getName(entity)}
                                                    </span>

                                                    <button
                                                        onclick={() => toggleInRemainderOrder(entityId)}
                                                        class="p-2 text-slate-400 dark:text-slate-500 hover:text-rose-600 dark:hover:text-rose-400 transition-colors"
                                                    >
                                                        <X class="w-5 h-5" />
                                                    </button>
                                                </div>
                                            {/if}
                                        {/each}
                                        {#if activeScenario.remainderOrder.length === 0}
                                            <div class="p-12 border-2 border-dashed border-slate-100 dark:border-slate-800 rounded-[40px] flex flex-col items-center justify-center space-y-4 opacity-50">
                                                <Activity class="w-8 h-8 text-slate-205 dark:text-slate-700" />
                                                <p class="text-[10px] font-black uppercase tracking-widest text-slate-400 text-center">
                                                    No priority order defined.<br/>Funds will remain unassigned.
                                                </p>
                                            </div>
                                        {/if}
                                    </div>
                                </div>
                            </div>
                        </div>
                    {/if}
                </div>
            </div>
        </div>
    </div>
{/if}
