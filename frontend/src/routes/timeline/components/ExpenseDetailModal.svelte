<script lang="ts">
    import { Calendar, Link2, Unlock, Lock, Plus, Trash2, X, DollarSign, Info, CheckCircle2 } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import Modal from "$lib/components/ui/Modal.svelte";
    import Button from "$lib/components/ui/Button.svelte";
    import Input from "$lib/components/ui/Input.svelte";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    let {
        open = $bindable(false),
        selectedExpenseObj = $bindable(),
        months = [],
        activeAssets = [],
        isProjecting = $bindable(false),
        onSave = () => {},
        onCreateFundingSubAsset = (assetId: string, remainderConsumer: boolean) => Promise.resolve(),
        formatCurrency = (v: number) => `€ ${v.toFixed(2)}`,
        parseMonthYear = (d: string) => d,
        getMonthKey = (d: string) => d,
        getMonthsBetween = (d1: string, d2: string) => 0,
        isFlexibleExpense = (name: string) => false,
        cleanExpenseName = (name: string) => name,
        updateExpenseDetails = () => {},
    } = $props<{
        open: boolean;
        selectedExpenseObj: any;
        months: any[];
        activeAssets: any[];
        isProjecting: boolean;
        onSave: () => Promise<void>;
        onCreateFundingSubAsset: (assetId: string, remainderConsumer: boolean) => Promise<void>;
        formatCurrency?: (v: number) => string;
        parseMonthYear?: (d: string) => string;
        getMonthKey?: (d: string) => string;
        getMonthsBetween?: (d1: string, d2: string) => number;
        isFlexibleExpense?: (name: string) => boolean;
        cleanExpenseName?: (name: string) => string;
        updateExpenseDetails?: (exp: any, name: string, due: string) => Promise<void>;
    }>();

    let modalMode = $state<"edit" | "funding">("edit");
    let isFlexExpenseState = $state(false);
    let fundingAssetId = $state("");
    let fundingSubAssetCreated = $state(false);
    let fundingRemainderConsumer = $state(false);
    let fundingMessage = $state<string | null>(null);

    $effect(() => {
        if (open && selectedExpenseObj) {
            isFlexExpenseState = isFlexibleExpense(selectedExpenseObj.name);
            modalMode = "edit";
            fundingSubAssetCreated = false;
            fundingRemainderConsumer = false;
            fundingMessage = null;
            if (activeAssets.length > 0) {
                const firstAsset = activeAssets[0];
                fundingAssetId = firstAsset.id || firstAsset.Id || firstAsset.ID || "";
            } else {
                fundingAssetId = "";
            }
        }
    });

    function toggleExpenseFlexibility() {
        let newName = selectedExpenseObj.name;
        const isFlex = isFlexibleExpense(newName);
        if (isFlex) {
            newName = cleanExpenseName(newName);
        } else {
            newName = newName + " (Flexible)";
        }
        selectedExpenseObj.name = newName;
        isFlexExpenseState = !isFlex;
    }

    function addSubExpense() {
        if (!selectedExpenseObj.activeVersion.subExpenses) {
            selectedExpenseObj.activeVersion.subExpenses = [];
        }
        selectedExpenseObj.activeVersion.subExpenses.push({
            description: "",
            amount: 0,
            metadataList: []
        });
    }

    function removeSubExpense(idx: number) {
        selectedExpenseObj.activeVersion.subExpenses.splice(idx, 1);
        selectedExpenseObj.activeVersion.subExpenses = [...selectedExpenseObj.activeVersion.subExpenses];
    }

    function updateSubExpenseAmount(idx: number, amt: number) {
        selectedExpenseObj.activeVersion.subExpenses[idx].amount = amt;
        selectedExpenseObj.activeVersion.subExpenses = [...selectedExpenseObj.activeVersion.subExpenses];
    }

    function addMetadataField(subIdx: number) {
        const sub = selectedExpenseObj.activeVersion.subExpenses[subIdx];
        if (!sub.metadataList) sub.metadataList = [];
        sub.metadataList.push({ key: "", value: "" });
        selectedExpenseObj.activeVersion.subExpenses = [...selectedExpenseObj.activeVersion.subExpenses];
    }

    function removeMetadataField(subIdx: number, fieldIdx: number) {
        const sub = selectedExpenseObj.activeVersion.subExpenses[subIdx];
        sub.metadataList.splice(fieldIdx, 1);
        selectedExpenseObj.activeVersion.subExpenses = [...selectedExpenseObj.activeVersion.subExpenses];
    }

    async function handleSave() {
        await onSave();
    }

    async function handleCreateFundingPlan() {
        try {
            await onCreateFundingSubAsset(fundingAssetId, fundingRemainderConsumer);
            fundingSubAssetCreated = true;
            fundingMessage = "Sub-Asset savings target successfully created and linked to this expense node!";
        } catch (e: any) {
            alert("Failed to create funding plan: " + e.message);
        }
    }

    function getID(entity: any): string {
        return entity?.id || entity?.Id || entity?.ID || "";
    }

    function getName(entity: any): string {
        return entity?.name || entity?.Name || "";
    }
</script>

<Modal
    bind:open
    title={selectedExpenseObj?.id ? "Configure Expense Node" : "Create Expense Node"}
    subtitle="Define parameters for this deterministic event."
>
    {#if selectedExpenseObj}
        <div class="flex border-b border-slate-100 dark:border-slate-800 -mx-6 -mt-6 mb-6">
            <button
                type="button"
                onclick={() => modalMode = "edit"}
                class="flex-1 py-3 text-center text-xs font-black uppercase tracking-wider border-b-2
                    {modalMode === 'edit' ? 'border-indigo-650 text-indigo-650 dark:border-indigo-400 dark:text-indigo-400' : 'border-transparent text-slate-400 hover:text-slate-650'}"
            >
                Reschedule &amp; Settings
            </button>
            <button
                type="button"
                onclick={() => { modalMode = "funding"; fundingSubAssetCreated = false; fundingMessage = null; }}
                class="flex-1 py-3 text-center text-xs font-black uppercase tracking-wider border-b-2 flex items-center justify-center gap-1.5
                    {modalMode === 'funding' ? 'border-indigo-650 text-indigo-650 dark:border-indigo-400 dark:text-indigo-400' : 'border-transparent text-slate-400 hover:text-slate-650'}"
                disabled={!selectedExpenseObj.id}
                title={!selectedExpenseObj.id ? "Save the expense first to configure funding" : ""}
                class:opacity-50={!selectedExpenseObj.id}
            >
                <Link2 class="w-4 h-4" /> Fund Later (Sub-Asset)
            </button>
        </div>

        <div class="space-y-6">
            {#if modalMode === 'edit'}
                <div class="space-y-4">
                    <Input
                        label="Expense Node Name"
                        bind:value={selectedExpenseObj.name}
                        placeholder="e.g. Car Purchase"
                    />

                    <div class="grid grid-cols-2 gap-4">
                        <Input
                            type="number"
                            label="Payment Cost"
                            bind:value={selectedExpenseObj.activeVersion.amount}
                            placeholder="0"
                        />
                        <div class="space-y-1">
                            <span class="text-[10px] font-black uppercase tracking-wider text-slate-400 block ml-1">Due Period</span>
                            <div class="text-sm font-bold text-slate-800 dark:text-slate-200 bg-slate-50 dark:bg-slate-800 px-4 py-3 rounded-xl border dark:border-slate-700 h-[46px] flex items-center shadow-inner">
                                {parseMonthYear(selectedExpenseObj.activeVersion?.dueDate || "")}
                            </div>
                        </div>
                    </div>

                    <div class="space-y-1.5">
                        <span class="text-[10px] font-black uppercase tracking-wider text-slate-400 block ml-1">Select Target Month</span>
                        <select
                            value={getMonthKey(selectedExpenseObj.activeVersion?.dueDate || "")}
                            onchange={(e) => {
                                const newDate = `${e.currentTarget.value}-01T00:00:00Z`;
                                if (selectedExpenseObj.id) {
                                    updateExpenseDetails(selectedExpenseObj!, selectedExpenseObj!.name, newDate);
                                } else {
                                    selectedExpenseObj.activeVersion.dueDate = newDate;
                                }
                            }}
                            class="w-full px-4 py-3 rounded-xl border border-slate-200 bg-white text-sm font-bold outline-none text-slate-700 focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                        >
                            {#each months as m}
                                <option value={getMonthKey(m.date)}>{parseMonthYear(m.date)}</option>
                            {/each}
                        </select>
                    </div>

                    <div class="flex items-center justify-between p-4 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 mt-4 shadow-sm">
                        <div class="space-y-0.5">
                            <span class="text-xs font-black text-slate-800 dark:text-slate-100 block">Flexibility Settings</span>
                            <p class="text-[10px] text-slate-400 font-medium">
                                Flexible expenses can be dragged around and rescheduled automatically by the optimizer.
                            </p>
                        </div>
                        <Button
                            onclick={toggleExpenseFlexibility}
                            class="px-4 py-2 font-black text-[10px] uppercase tracking-wider rounded-xl transition-all flex items-center gap-1.5 shadow-sm
                                {isFlexExpenseState 
                                    ? 'bg-amber-500 text-white hover:bg-amber-600 border-amber-500 hover:border-amber-600' 
                                    : 'bg-white hover:bg-slate-50 border text-slate-650 dark:bg-slate-750 dark:text-slate-200 dark:border-slate-650'}"
                        >
                            {#if isFlexExpenseState}
                                <Unlock class="w-3.5 h-3.5" /> Flexible
                            {:else}
                                <Lock class="w-3.5 h-3.5" /> Fixed
                            {/if}
                        </Button>
                    </div>

                    <!-- Sub Expenses Section -->
                    <div class="space-y-3 pt-4 border-t border-slate-150 dark:border-slate-800">
                        <div class="flex justify-between items-center">
                            <span class="text-[10px] font-black uppercase tracking-wider text-slate-400">Sub Expenses</span>
                            <button
                                type="button"
                                onclick={addSubExpense}
                                class="text-indigo-650 hover:text-indigo-750 dark:text-indigo-400 dark:hover:text-indigo-300 text-xs font-black uppercase tracking-wider flex items-center gap-1 transition-all active:scale-95 cursor-pointer"
                            >
                                <Plus class="w-3.5 h-3.5" /> Add Sub Expense
                            </button>
                        </div>

                        {#if !selectedExpenseObj.activeVersion?.subExpenses || selectedExpenseObj.activeVersion.subExpenses.length === 0}
                            <div class="p-4 bg-slate-50/50 dark:bg-slate-800/20 rounded-xl border border-dashed border-slate-200 dark:border-slate-700 text-center text-xs font-medium text-slate-450">
                                No sub expenses added yet. Itemize this expense to track parts of it.
                            </div>
                        {:else}
                            <div class="space-y-3 max-h-[260px] overflow-y-auto pr-1">
                                {#each selectedExpenseObj.activeVersion.subExpenses as sub, subIdx}
                                    <div class="p-4 bg-slate-50 dark:bg-slate-800/40 rounded-xl border border-slate-100 dark:border-slate-800 relative space-y-3" transition:slide>
                                        <button
                                            type="button"
                                            onclick={() => removeSubExpense(subIdx)}
                                            class="absolute top-3 right-3 text-slate-400 hover:text-rose-500 p-1 transition-colors cursor-pointer"
                                            title="Delete Sub Expense"
                                        >
                                            <Trash2 class="w-4 h-4" />
                                        </button>

                                        <div class="grid grid-cols-3 gap-3">
                                            <div class="col-span-2 space-y-1">
                                                <span class="text-[9px] font-black uppercase tracking-wider text-slate-400 block">Description</span>
                                                <input
                                                    type="text"
                                                    bind:value={sub.description}
                                                    class="w-full px-3 py-2 rounded-xl border border-slate-200 bg-white text-xs font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                                    placeholder="e.g. Charger cable"
                                                />
                                            </div>
                                            <div class="space-y-1">
                                                <span class="text-[9px] font-black uppercase tracking-wider text-slate-400 block">Amount</span>
                                                <input
                                                    type="number"
                                                    value={sub.amount}
                                                    oninput={(e) => updateSubExpenseAmount(subIdx, Number(e.currentTarget.value) || 0)}
                                                    class="w-full px-3 py-2 rounded-xl border border-slate-200 bg-white text-xs font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                                    placeholder="0.00"
                                                />
                                            </div>
                                        </div>

                                        <!-- Custom KV Storage / Metadata -->
                                        <div class="space-y-2 border-t border-slate-100 dark:border-slate-700 pt-2">
                                            <div class="flex justify-between items-center">
                                                <span class="text-[9px] font-black uppercase tracking-wider text-slate-400">Additional Info (e.g. Buy Link)</span>
                                                <button
                                                    type="button"
                                                    onclick={() => addMetadataField(subIdx)}
                                                    class="text-[9px] font-black text-indigo-500 hover:text-indigo-650 uppercase tracking-wider flex items-center gap-0.5 cursor-pointer"
                                                >
                                                    <Plus class="w-3 h-3" /> Add Detail
                                                </button>
                                            </div>

                                            {#if sub.metadataList && sub.metadataList.length > 0}
                                                <div class="space-y-2">
                                                    {#each sub.metadataList as pair, pairIdx}
                                                        <div class="flex items-center gap-2">
                                                            <input
                                                                type="text"
                                                                bind:value={pair.key}
                                                                class="w-1/3 px-3 py-1.5 rounded-lg border border-slate-200 bg-white text-[11px] font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                                                placeholder="Label (e.g. Link)"
                                                            />
                                                            <input
                                                                type="text"
                                                                bind:value={pair.value}
                                                                class="flex-1 px-3 py-1.5 rounded-lg border border-slate-200 bg-white text-[11px] font-bold outline-none text-slate-700 focus:border-indigo-500 dark:bg-slate-800 dark:border-slate-700 dark:text-slate-200"
                                                                placeholder="Value (e.g. url)"
                                                            />
                                                            <button
                                                                type="button"
                                                                onclick={() => removeMetadataField(subIdx, pairIdx)}
                                                                class="text-slate-400 hover:text-rose-500 p-1 cursor-pointer"
                                                                title="Remove detail"
                                                            >
                                                                <X class="w-3.5 h-3.5" />
                                                            </button>
                                                        </div>
                                                    {/each}
                                                </div>
                                            {/if}
                                        </div>
                                    </div>
                                {/each}
                            </div>
                        {/if}
                    </div>
                </div>
            {/if}

            {#if modalMode === 'funding'}
                <div class="space-y-4">
                    <div class="p-4 bg-indigo-50/50 border border-indigo-100 dark:bg-indigo-950/20 dark:border-indigo-900/40 rounded-2xl space-y-2">
                        <h4 class="text-xs font-black text-indigo-950 dark:text-indigo-300 uppercase tracking-wider flex items-center gap-1">
                            <Info class="w-4 h-4 text-indigo-550" /> Funding Plan Calculator
                        </h4>
                        <p class="text-[11px] text-slate-500 leading-normal font-medium dark:text-slate-400">
                            Allocate savings to a money market fund or ETF asset sub-account. The system will accumulate cash monthly to guarantee your due date is funded.
                        </p>
                    </div>

                    {#if fundingMessage}
                        <div class="p-4 bg-emerald-50 border border-emerald-255 text-emerald-800 rounded-2xl text-[11px] leading-relaxed font-bold flex gap-2">
                            <CheckCircle2 class="w-5 h-5 text-emerald-600 shrink-0 mt-0.5" />
                            <span>{fundingMessage}</span>
                        </div>
                    {:else}
                        {@const monthsLeft = Math.max(1, getMonthsBetween(new Date().toISOString(), selectedExpenseObj.activeVersion?.dueDate || ""))}
                        {@const monthlyRate = fundingRemainderConsumer ? 0 : (selectedExpenseObj.activeVersion!.amount / monthsLeft)}

                        <div class="grid grid-cols-2 gap-4 text-center">
                            <div class="p-3 bg-slate-50 dark:bg-slate-800 rounded-xl border dark:border-slate-700 shadow-inner">
                                <span class="text-[9px] font-black uppercase text-slate-400 block">Months Remaining</span>
                                <span class="text-lg font-black text-slate-800 dark:text-slate-200">{monthsLeft}</span>
                            </div>
                            <div class="p-3 bg-slate-50 dark:bg-slate-800 rounded-xl border dark:border-slate-700 shadow-inner">
                                <span class="text-[9px] font-black uppercase text-slate-400 block">Monthly Savings Required</span>
                                <span class="text-lg font-black text-indigo-600 dark:text-indigo-400">{formatCurrency(monthlyRate)}</span>
                            </div>
                        </div>

                        {#if activeAssets.length === 0}
                            <div class="p-4 text-center text-xs font-medium text-slate-450">
                                No investment assets found. Please create an asset first.
                            </div>
                        {:else}
                            <div class="space-y-4">
                                <div class="space-y-1.5">
                                    <SearchableDropdown
                                        label="Select Target Asset Account"
                                        options={activeAssets.map((asset: any) => ({ id: getID(asset), label: getName(asset) }))}
                                        bind:value={fundingAssetId}
                                        placeholder="Choose asset account..."
                                    />
                                </div>

                                <div class="p-4 bg-white border border-slate-200 dark:bg-slate-800 dark:border-slate-700 rounded-2xl flex items-center justify-between gap-4 shadow-sm">
                                    <div class="space-y-0.5">
                                        <span class="text-xs font-black text-slate-700 dark:text-slate-200 block">Fund via Asset Remainder</span>
                                        <span class="text-[10px] text-slate-400 leading-normal block">
                                            Uses monthly scenario remainder. Enables flexible target date (triggers when fully funded).
                                        </span>
                                    </div>
                                    <input
                                        type="checkbox"
                                        bind:checked={fundingRemainderConsumer}
                                        class="w-5 h-5 accent-indigo-650 rounded-lg cursor-pointer"
                                    />
                                </div>
                            </div>

                            <Button
                                onclick={handleCreateFundingPlan}
                                class="w-full py-4 text-xs tracking-wider"
                            >
                                <DollarSign class="w-4 h-4" /> Confirm &amp; Create Sub-Asset Plan
                            </Button>
                        {/if}
                    {/if}
                </div>
            {/if}
        </div>

        <div class="pt-6 flex justify-end gap-3 border-t border-slate-100 dark:border-slate-800 -mx-6 -mb-6 p-6 mt-6 bg-slate-50 dark:bg-slate-900/50">
            <Button variant="secondary" onclick={() => open = false} class="px-6">
                Close
            </Button>
            <Button
                onclick={handleSave}
                loading={isProjecting}
                loadingLabel="Saving..."
                class="px-6"
            >
                {selectedExpenseObj.id ? "Save Changes" : "Create Expense"}
            </Button>
        </div>
    {/if}
</Modal>
