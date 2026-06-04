<script lang="ts">
    import { Plus, Trash2, Calendar, Euro, Clock, Pencil, Text } from "@lucide/svelte";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";
    import { slide, fade } from "svelte/transition";

    interface TimeSlice {
        id?: string;
        amount: number;
        intervalMonths: number;
        startDate: string;
        endDate: string | null;
        description: string;
    }

    let { slices = $bindable(), label = "Time Slices" } = $props<{
        slices: TimeSlice[];
        label?: string;
    }>();

    let showAddForm = $state(false);
    let editingIndex = $state<number | null>(null);

    // Form state
    let amountInput = $state("");
    let intervalMonths = $state(1);
    let startMonth = $state("");
    let endMonth = $state("");
    let description = $state("");

    function resetForm() {
        amountInput = "";
        intervalMonths = 1;
        startMonth = "";
        endMonth = "";
        description = "";
        editingIndex = null;
    }

    function addSlice() {
        const slice: TimeSlice = {
            amount: parseGermanAmount(amountInput),
            intervalMonths,
            startDate: fromInputMonth(startMonth),
            endDate: endMonth ? fromInputMonth(endMonth) : null,
            description
        };

        if (editingIndex !== null) {
            slices[editingIndex] = slice;
        } else {
            slices.push(slice);
        }

        resetForm();
        showAddForm = false;
    }

    function editSlice(index: number) {
        const slice = slices[index];
        amountInput = formatGermanAmount(slice.amount);
        intervalMonths = slice.intervalMonths;
        startMonth = toInputMonth(slice.startDate);
        endMonth = toInputMonth(slice.endDate);
        description = slice.description;
        editingIndex = index;
        showAddForm = true;
    }

    function removeSlice(index: number) {
        slices.splice(index, 1);
    }

    function toInputMonth(isoStr: string | null): string {
        if (!isoStr) return "";
        return isoStr.substring(0, 7); // "YYYY-MM"
    }

    function fromInputMonth(val: string): string {
        if (!val) return "";
        return val + "-01T00:00:00Z";
    }

    function formatDate(dateStr: string | null) {
        if (!dateStr) return "Ongoing";
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            year: "numeric",
            month: "2-digit",
        });
    }
</script>

<div class="space-y-4">
    <div class="flex items-center justify-between">
        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">
            {label}
        </label>
        {#if !showAddForm}
            <button
                type="button"
                onclick={() => { showAddForm = true; resetForm(); }}
                class="flex items-center gap-1 text-[10px] font-black uppercase tracking-widest text-indigo-600 hover:text-indigo-700 transition-colors"
            >
                <Plus class="w-3 h-3" /> Add Slice
            </button>
        {/if}
    </div>

    {#if showAddForm}
        <div transition:slide class="glass-card p-6 bg-slate-50/50 space-y-4 border-indigo-100">
            <div class="grid grid-cols-2 gap-4">
                <div class="space-y-1">
                    <label class="text-[9px] font-black uppercase tracking-wider text-slate-400 ml-1">Amount (€)</label>
                    <div class="relative">
                        <Euro class="absolute left-3 top-1/2 -translate-y-1/2 w-3 h-3 text-slate-400" />
                        <input
                            type="text"
                            bind:value={amountInput}
                            placeholder="1.234,56"
                            class="block w-full pl-8 pr-3 py-2 bg-white border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 outline-none transition-all"
                        />
                    </div>
                </div>
                <div class="space-y-1">
                    <label class="text-[9px] font-black uppercase tracking-wider text-slate-400 ml-1">Interval</label>
                    <select
                        bind:value={intervalMonths}
                        class="block w-full px-3 py-2 bg-white border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 outline-none transition-all appearance-none cursor-pointer"
                    >
                        <option value={1}>Monthly</option>
                        <option value={3}>Quarterly</option>
                        <option value={12}>Yearly</option>
                    </select>
                </div>
            </div>

            <div class="grid grid-cols-2 gap-4">
                <div class="space-y-1">
                    <label class="text-[9px] font-black uppercase tracking-wider text-slate-400 ml-1">Start Month</label>
                    <input
                        type="month"
                        bind:value={startMonth}
                        class="block w-full px-3 py-2 bg-white border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 outline-none transition-all"
                    />
                </div>
                <div class="space-y-1">
                    <label class="text-[9px] font-black uppercase tracking-wider text-slate-400 ml-1">End Month</label>
                    <input
                        type="month"
                        bind:value={endMonth}
                        class="block w-full px-3 py-2 bg-white border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 outline-none transition-all"
                    />
                </div>
            </div>

            <div class="space-y-1">
                <label class="text-[9px] font-black uppercase tracking-wider text-slate-400 ml-1">Description / Reason</label>
                <div class="relative">
                    <Text class="absolute left-3 top-1/2 -translate-y-1/2 w-3 h-3 text-slate-400" />
                    <input
                        type="text"
                        bind:value={description}
                        placeholder="e.g. Contractual adjustment"
                        class="block w-full pl-8 pr-3 py-2 bg-white border border-slate-200 rounded-lg text-xs font-bold focus:ring-2 focus:ring-indigo-500/20 focus:border-indigo-500 outline-none transition-all"
                    />
                </div>
            </div>

            <div class="flex gap-2 pt-2">
                <button
                    type="button"
                    onclick={addSlice}
                    class="flex-1 py-2 bg-indigo-600 text-white rounded-lg text-[10px] font-black uppercase tracking-widest hover:bg-indigo-700 transition-all shadow-lg shadow-indigo-100"
                >
                    {editingIndex !== null ? 'Update' : 'Add'} Slice
                </button>
                <button
                    type="button"
                    onclick={() => { showAddForm = false; resetForm(); }}
                    class="px-4 py-2 text-slate-400 hover:text-slate-600 text-[10px] font-black uppercase tracking-widest transition-all"
                >
                    Cancel
                </button>
            </div>
        </div>
    {/if}

    <div class="space-y-2">
        {#each slices as slice, i}
            <div transition:fade class="flex items-center gap-4 p-4 bg-white border border-slate-100 rounded-xl hover:border-slate-200 transition-all group">
                <div class="flex-1 min-w-0">
                    <div class="flex items-center gap-2 mb-1">
                        <span class="text-sm font-black text-slate-900">{formatGermanAmount(slice.amount)} €</span>
                        <span class="px-1.5 py-0.5 bg-slate-50 text-slate-400 rounded text-[8px] font-black uppercase tracking-wider">
                            {slice.intervalMonths === 1 ? 'Monthly' : slice.intervalMonths === 3 ? 'Quarterly' : 'Yearly'}
                        </span>
                    </div>
                    {#if slice.description}
                        <p class="text-[10px] text-slate-500 font-medium truncate mb-1">{slice.description}</p>
                    {/if}
                    <div class="flex items-center gap-2 text-[9px] text-slate-400 font-bold">
                        <Calendar class="w-2.5 h-2.5" />
                        <span>{formatDate(slice.startDate)}</span>
                        <Clock class="w-2.5 h-2.5 ml-1" />
                        <span>{formatDate(slice.endDate)}</span>
                    </div>
                </div>
                <div class="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                    <button
                        type="button"
                        onclick={() => editSlice(i)}
                        class="p-1.5 text-slate-300 hover:text-indigo-600 hover:bg-indigo-50 rounded-lg transition-all"
                    >
                        <Pencil class="w-3.5 h-3.5" />
                    </button>
                    <button
                        type="button"
                        onclick={() => removeSlice(i)}
                        class="p-1.5 text-slate-300 hover:text-rose-600 hover:bg-rose-50 rounded-lg transition-all"
                    >
                        <Trash2 class="w-3.5 h-3.5" />
                    </button>
                </div>
            </div>
        {/each}
        {#if slices.length === 0 && !showAddForm}
            <p class="text-[10px] text-slate-400 font-medium italic text-center py-4 bg-slate-50/50 rounded-xl border border-dashed border-slate-200">
                No time-based variations defined.
            </p>
        {/if}
    </div>
</div>
