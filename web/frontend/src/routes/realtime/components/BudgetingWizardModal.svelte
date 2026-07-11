<script lang="ts">
    import {
        AlertTriangle,
        Loader2,
        Check,
    } from "@lucide/svelte";
    import Modal from "$lib/components/Modal.svelte";

    let {
        open = $bindable(false),
        wizardError = "",
        wizardPoolName = $bindable(""),
        editAmountInput = 0,
        transactionToEdit = null as any,
        activeScenario = null as any,
        monthStartDay = 1,
        getPeriodBoundsForDate = (date: Date, startDay: number) => ({ start: new Date() }),
        runWizard = () => {},
        isWizardSaving = false,
    } = $props<{
        open: boolean;
        wizardError: string;
        wizardPoolName: string;
        editAmountInput: number;
        transactionToEdit: any;
        activeScenario: any;
        monthStartDay: number;
        getPeriodBoundsForDate: (date: Date, startDay: number) => { start: Date };
        runWizard: () => void;
        isWizardSaving: boolean;
    }>();
</script>

{#if open}
    <Modal
        bind:open={open}
        title="Budgeting Wizard"
        subtitle="Create a pool, link this transaction, and setup a matching expense."
        maxWidthClass="max-w-md"
        zIndexClass="z-[110]"
    >
        {#if wizardError}
            <div class="p-4 bg-rose-50 border border-rose-100 rounded-2xl flex items-start gap-2.5 text-rose-600 text-xs font-semibold">
                <AlertTriangle class="w-4 h-4 shrink-0 mt-0.5" />
                <p>{wizardError}</p>
            </div>
        {/if}

        <div class="space-y-4">
            <div class="space-y-2">
                <label
                    for="wizard-pool-name"
                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                >
                    Pool & Expense Name
                </label>
                <div class="flex items-center gap-3 p-3 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all">
                    <input
                        id="wizard-pool-name"
                        type="text"
                        bind:value={wizardPoolName}
                        placeholder="Enter budget / pool name..."
                        class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                    />
                </div>
            </div>

            <div class="p-5 bg-slate-50 border border-slate-100 rounded-2xl space-y-3.5 text-xs font-medium text-slate-655 leading-relaxed">
                <div class="flex justify-between items-center text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">
                    <span>Summary of Actions</span>
                    <span class="text-indigo-600">Automated</span>
                </div>
                <ul class="space-y-2.5">
                    <li class="flex items-start gap-2">
                        <span class="w-1.5 h-1.5 rounded-full bg-indigo-650 mt-1.5 shrink-0"></span>
                        <span>Create pool <strong class="text-slate-800">"{wizardPoolName || '...'}"</strong></span>
                    </li>
                    <li class="flex items-start gap-2">
                        <span class="w-1.5 h-1.5 rounded-full bg-indigo-650 mt-1.5 shrink-0"></span>
                        <span>Attach transaction to pool using rule parameter <strong class="text-slate-800">TRANSACTION_ID</strong></span>
                    </li>
                    <li class="flex items-start gap-2">
                        <span class="w-1.5 h-1.5 rounded-full bg-indigo-650 mt-1.5 shrink-0"></span>
                        <span>
                            Create expense <strong class="text-slate-800">"{wizardPoolName || '...'}"</strong> matching the transaction amount of
                            <strong class="text-slate-800">€{Math.abs(editAmountInput).toLocaleString("de-DE", { minimumFractionDigits: 2 })}</strong>
                        </span>
                    </li>
                    <li class="flex items-start gap-2">
                        <span class="w-1.5 h-1.5 rounded-full bg-indigo-650 mt-1.5 shrink-0"></span>
                        <span>
                            Set expense date to active month start day:
                            {#if transactionToEdit?.createdAt}
                                <strong class="text-slate-800">
                                    {getPeriodBoundsForDate(new Date(transactionToEdit.createdAt), monthStartDay).start.toLocaleDateString("de-DE")}
                                </strong>
                            {/if}
                        </span>
                    </li>
                    <li class="flex items-start gap-2">
                        <span class="w-1.5 h-1.5 rounded-full bg-indigo-650 mt-1.5 shrink-0"></span>
                        <span>
                            Link expense to active scenario:
                            <strong class="text-slate-800">"{activeScenario?.name || 'Default Scenario'}"</strong>
                        </span>
                    </li>
                </ul>
            </div>
        </div>

        <div class="flex gap-4 mt-6">
            <button
                type="button"
                onclick={() => (open = false)}
                class="px-6 py-4 bg-slate-50 hover:bg-slate-100 text-slate-500 border border-slate-200/50 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all flex-1 text-center shadow-sm"
                disabled={isWizardSaving}
            >
                Cancel
            </button>
            <button
                type="button"
                onclick={runWizard}
                class="px-6 py-4 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-3 flex-[2] bg-indigo-600 hover:bg-indigo-700 text-white shadow-lg shadow-indigo-200"
                disabled={isWizardSaving || !wizardPoolName.trim()}
            >
                {#if isWizardSaving}
                    <Loader2 class="w-4 h-4 animate-spin" />
                    <span>Processing...</span>
                {:else}
                    <Check class="w-4 h-4" />
                    <span>Confirm & Generate</span>
                {/if}
            </button>
        </div>
    </Modal>
{/if}
