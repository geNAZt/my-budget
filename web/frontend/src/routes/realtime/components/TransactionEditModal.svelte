<script lang="ts">
    import {
        X,
        User,
        Zap,
        Hash,
        Search,
        AlertTriangle,
        Check,
        Trash2,
        Activity,
        ShieldCheck,
    } from "@lucide/svelte";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";
    import Modal from "$lib/components/Modal.svelte";

    let {
        open = $bindable(false),
        transactionToEdit = $bindable<any>(null),
        accountOptions = [],
        poolOptions = [],
        editReceiverInput = "",
        editReceiverIbanInput = "",
        editAmountInput = 0,
        editDescriptionInput = $bindable(""),
        editTagsInput = $bindable(""),
        tagSearchQuery = $bindable(""),
        filteredTags = () => [] as string[],
        showAddTagOption = () => false,
        selectTag = (tag: string) => {},
        addAndSelectTag = (tag: string) => {},
        startWizard = () => {},
        markAsNotDuplicate = () => {},
        deniedTransactions = [],
        allowDuplicate = (id: string) => {},
        showRawData = $bindable(false),
        integrations = [],
        deleteTransaction = () => {},
        unlinkTransaction = (id: string) => {},
        saveTransactionEdit = () => {},
    } = $props<{
        open: boolean;
        transactionToEdit: any;
        accountOptions: any[];
        poolOptions: any[];
        editReceiverInput: string;
        editReceiverIbanInput: string;
        editAmountInput: number;
        editDescriptionInput: string;
        editTagsInput: string;
        tagSearchQuery: string;
        filteredTags: () => string[];
        showAddTagOption: () => boolean;
        selectTag: (tag: string) => void;
        addAndSelectTag: (tag: string) => void;
        startWizard: () => void;
        markAsNotDuplicate: () => void;
        deniedTransactions: any[];
        allowDuplicate: (id: string) => void;
        showRawData: boolean;
        integrations: any[];
        deleteTransaction: () => void;
        unlinkTransaction: (id: string) => void;
        saveTransactionEdit: () => void;
    }>();
</script>

<Modal
    bind:open={open}
    title="Modify Transaction Flow"
    subtitle="Update account mapping and deterministic metadata."
    maxWidthClass="max-w-xl"
    zIndexClass="z-[100]"
>
    {#if transactionToEdit}
        <!-- Source / Destination Accounts Selection -->
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
            <SearchableDropdown
                label="Source Account"
                options={accountOptions}
                bind:value={transactionToEdit.sourceAccountId}
                placeholder="Select source..."
            />

            <SearchableDropdown
                label="Destination Account"
                options={accountOptions}
                bind:value={transactionToEdit.destinationAccountId}
                placeholder="Select destination..."
            />
        </div>

        <!-- Read-Only Transaction Info (Amount, Peer Name, Peer IBAN) -->
        <div class="p-5 bg-slate-50 border border-slate-100 rounded-2xl space-y-4">
            <div class="flex justify-between items-center">
                <div class="space-y-1">
                    <span class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400">Peer Name / IBAN</span>
                    <p class="text-xs font-black text-slate-800 flex items-center gap-1.5">
                        <User class="w-3.5 h-3.5 text-slate-400" />
                        {editReceiverInput || "—"}
                    </p>
                    {#if editReceiverIbanInput}
                        <p class="text-[10px] font-bold text-slate-500 font-mono tracking-wider">
                            {editReceiverIbanInput}
                        </p>
                    {/if}
                </div>
                <div class="text-right">
                    <span class="block text-[9px] font-black uppercase tracking-[0.2em] text-slate-400">Amount</span>
                    <span class="text-lg font-black tracking-tight tabular-nums {editAmountInput < 0 ? 'text-rose-600' : 'text-emerald-600'}">
                        {editAmountInput < 0 ? '-' : ''}{Math.abs(editAmountInput).toLocaleString("de-DE", { minimumFractionDigits: 2 })} €
                    </span>
                </div>
            </div>

            <!-- Wizard Trigger Button -->
            <div class="pt-3 border-t border-slate-200/60 flex justify-end">
                <button
                    type="button"
                    onclick={startWizard}
                    class="px-3 py-1.5 bg-indigo-50 hover:bg-indigo-100 text-indigo-650 hover:text-indigo-700 rounded-xl font-black text-[9px] uppercase tracking-wider transition-all flex items-center gap-1.5 shadow-sm shadow-indigo-100/10 border border-indigo-100"
                >
                    <Zap class="w-3 h-3" />
                    <span>Create Pool & Expense Wizard</span>
                </button>
            </div>
        </div>

        <!-- Reference / Description & Assigned Pools selection -->
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div class="space-y-2">
                <label
                    for="edit-description-input"
                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                    >Reference / Description</label
                >
                <div
                    class="flex items-center gap-3 p-3 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all"
                >
                    <Hash class="w-4 h-4 text-slate-400" />
                    <input
                        id="edit-description-input"
                        type="text"
                        bind:value={editDescriptionInput}
                        placeholder="..."
                        class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                    />
                </div>
            </div>

            <SearchableMultiSelect
                label="Assigned Pools"
                options={poolOptions}
                bind:values={transactionToEdit.poolIds}
                placeholder="Select pools..."
            />
        </div>

        <!-- Single Tag Cloud Selector -->
        <div class="space-y-3">
            <label
                for="tag-search-input"
                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                >Deterministic Tag</label
            >
            <div class="space-y-3 p-4 bg-slate-50 border border-slate-100 rounded-2xl">
                <div
                    class="flex items-center gap-3 p-2.5 bg-white border border-slate-200 rounded-xl focus-within:ring-4 focus-within:ring-indigo-500/10 focus-within:border-indigo-500 transition-all"
                >
                    <Search class="w-3.5 h-3.5 text-slate-400" />
                    <input
                        id="tag-search-input"
                        type="text"
                        bind:value={tagSearchQuery}
                        placeholder="Search or add tag..."
                        class="bg-transparent border-none outline-none text-xs font-black w-full text-slate-900 placeholder:text-slate-300"
                    />
                    {#if tagSearchQuery}
                        <button 
                            type="button"
                            onclick={() => tagSearchQuery = ""}
                            class="text-slate-400 hover:text-slate-655"
                        >
                            <X class="w-3.5 h-3.5" />
                        </button>
                    {/if}
                </div>

                <div class="flex flex-wrap gap-2 max-h-32 overflow-y-auto custom-scrollbar pr-1 py-1">
                    {#each filteredTags() as tag}
                        {@const isSelected = editTagsInput === tag}
                        <button
                            type="button"
                            onclick={() => selectTag(tag)}
                            class="px-3 py-1.5 rounded-xl text-[10px] font-black uppercase tracking-wider transition-all border {isSelected 
                                ? 'bg-indigo-600 border-indigo-600 text-white shadow-md shadow-indigo-105' 
                                : 'bg-white border-slate-200 text-slate-600 hover:bg-slate-105 hover:border-slate-300 hover:text-slate-900'}"
                        >
                            {tag}
                        </button>
                    {/each}

                    {#if showAddTagOption()}
                        <button
                            type="button"
                            onclick={() => addAndSelectTag(tagSearchQuery)}
                            class="px-3 py-1.5 rounded-xl text-[10px] font-black uppercase tracking-wider transition-all border bg-emerald-50 border-emerald-200 text-emerald-600 hover:bg-emerald-100 hover:border-emerald-300"
                        >
                            + Add "{tagSearchQuery.trim()}"
                        </button>
                    {/if}
                </div>

                {#if !filteredTags().length && !showAddTagOption()}
                    <p class="text-[10px] font-black text-slate-400 uppercase text-center py-2">
                        No tags found. Type above to add one.
                    </p>
                {/if}
            </div>
        </div>

        {#if transactionToEdit.isPotentialDuplicate}
            <div
                class="p-6 bg-amber-50/50 rounded-2xl border border-amber-200/50 flex items-center justify-between group hover:border-amber-300 transition-all shadow-sm"
            >
                <div class="space-y-1 pr-4">
                    <div
                        class="flex items-center gap-1.5 text-[10px] font-black uppercase tracking-[0.2em] text-amber-600"
                    >
                        <AlertTriangle class="w-3.5 h-3.5" />
                        <span>Duplicate Detected</span>
                    </div>
                    <p class="text-xs font-medium text-slate-655">
                        An identical transaction was flagged. Declare them distinct.
                    </p>
                </div>
                <button
                    onclick={markAsNotDuplicate}
                    class="px-4 py-2.5 bg-amber-600 hover:bg-amber-700 text-white rounded-xl font-black text-[9px] uppercase tracking-wider transition-all flex items-center gap-1.5 shadow-sm shadow-amber-200"
                >
                    <Check class="w-3.5 h-3.5" />
                    <span>Not a Duplicate</span>
                </button>
            </div>
        {/if}

        {#if deniedTransactions.length > 0}
            <div class="space-y-3">
                <span
                    class="block text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1"
                    >Denied Duplicate Links</span
                >
                <div class="space-y-2.5">
                    {#each deniedTransactions as dt (dt.id)}
                        <div
                            class="p-4 bg-slate-50 border border-slate-200/60 rounded-2xl flex items-center justify-between group hover:border-slate-300 transition-all shadow-sm"
                        >
                            <div class="space-y-1">
                                <p
                                    class="text-xs font-black text-slate-800"
                                >
                                    {dt.data?.remittanceInformationUnstructured || dt.data?.creditorName || "Untitled Merchant"}
                                </p>
                                <div
                                    class="flex items-center gap-2 text-[10px] font-black text-slate-400 tracking-wider"
                                >
                                    <span>{new Date(dt.createdAt).toLocaleDateString()}</span>
                                    <span>•</span>
                                    <span
                                        class={dt.data?.transactionAmount?.amount < 0
                                            ? "text-rose-500"
                                            : "text-emerald-500"}
                                    >
                                        {dt.data?.transactionAmount?.amount} {dt.data?.transactionAmount?.currency || "EUR"}
                                    </span>
                                </div>
                            </div>
                            <button
                                onclick={() => allowDuplicate(dt.id)}
                                class="p-2.5 bg-white hover:bg-rose-50 border border-slate-200 hover:border-rose-200 text-slate-400 hover:text-rose-600 rounded-xl transition-all shadow-sm"
                                title="Allow duplicate matching again"
                            >
                                <Trash2 class="w-4 h-4" />
                            </button>
                        </div>
                    {/each}
                </div>
            </div>
        {/if}

        <div class="space-y-3">
            <div class="flex items-center justify-between">
                <span class="block text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1">
                    Database Payload
                </span>
                <button
                    type="button"
                    onclick={() => showRawData = !showRawData}
                    class="text-[9px] font-black uppercase tracking-wider text-indigo-650 hover:text-indigo-800 transition-colors cursor-pointer"
                >
                    {showRawData ? 'Hide Raw Data' : 'Show Raw Data'}
                </button>
            </div>
            {#if showRawData}
                <div class="p-6 bg-white border border-slate-200/60 rounded-2xl shadow-sm overflow-hidden">
                    <pre class="text-[10px] font-mono text-slate-700 whitespace-pre-wrap break-all select-all leading-relaxed max-h-60 overflow-y-auto custom-scrollbar">{JSON.stringify(transactionToEdit, null, 4)}</pre>
                </div>
            {/if}
        </div>

        <div class="text-[10px] text-slate-400 flex items-center justify-center gap-1 font-medium italic">
            <ShieldCheck class="w-3.5 h-3.5 text-slate-400" />
            <span>Anchored Integration:</span>
            <span class="text-slate-500">
                {integrations.find(
                    (i: any) => i.integrationId === transactionToEdit.integrationId,
                )?.integrationName || "Unknown Integration"}
            </span>
        </div>

        <div class="flex gap-4">
            <button
                onclick={deleteTransaction}
                class="px-6 py-4 bg-rose-50 hover:bg-rose-100 text-rose-600 border border-rose-200/50 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-2 flex-1 shadow-sm hover:shadow-rose-100"
            >
                <Trash2 class="w-4 h-4" />
                <span>Delete</span>
            </button>
            {#if transactionToEdit.isLinkConfirmed}
                <button
                    onclick={() => unlinkTransaction(transactionToEdit.id)}
                    class="px-6 py-4 bg-amber-50 hover:bg-amber-100 text-amber-600 border border-amber-200/50 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-2 flex-1 shadow-sm hover:shadow-amber-100"
                >
                    <Activity class="w-4 h-4" />
                    <span>Unlink</span>
                </button>
            {/if}
            <button
                onclick={saveTransactionEdit}
                class="px-6 py-4 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] transition-all flex items-center justify-center gap-3 flex-[2] bg-indigo-600 hover:bg-indigo-700 text-white shadow-lg shadow-indigo-200"
            >
                <Check class="w-4 h-4" />
                <span>Apply Flow Changes</span>
            </button>
        </div>
    {/if}
</Modal>
