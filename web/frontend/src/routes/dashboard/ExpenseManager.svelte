<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        ExpenseListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        ExpenseSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";
    const decode = (obj: any) => {
        if (!obj) return obj;
        try {
            return structuredClone(obj);
        } catch (e) {
            return JSON.parse(JSON.stringify(obj));
        }
    };

    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Calendar,
        Euro,
        ArrowRight,
        Clock,
        Loader2,
        History,
        Archive,
        Undo2,
        Pencil,
        CreditCard,
        ClipboardPaste,
        CheckCircle2,
        AlertCircle,
        Check,
        Globe,
        Languages,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";
    import TimeSliceManager from "$lib/components/TimeSliceManager.svelte";

    interface TimeSlice {
        id?: string;
        amount: number;
        intervalMonths: number;
        startDate: string;
        endDate: string | null;
        description: string;
    }

    interface ExpenseVersion {
        id?: string;
        expenseId?: string;
        amount: number;
        dueDate: string;
        createdAt?: string;
        slices: TimeSlice[];
    }

    interface Expense {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        linkToScenarios?: string[];
        activeVersion?: ExpenseVersion;
        import_selected?: boolean; // UI only
    }

    let expenses = $state<(Expense & { activeVersion: ExpenseVersion })[]>([]);
    let pools = $state<any[]>([]);
    let virtualAccounts = $state<any[]>([]);
    let isLoading = $state(true);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

    const poolOptions = $derived([
        { id: "", label: "None / Uncategorized" },
        ...(pools || []).map((p) => ({
            id: p.id,
            label: p.name,
        })),
    ]);

    const virtualAccountOptions = $derived([
        { id: "", label: "None / General" },
        ...(virtualAccounts || []).map((va) => ({
            id: va.id,
            label: va.name,
        })),
    ]);

    const virtualAccountMultiOptions = $derived(
        (virtualAccounts || []).map((va) => ({
            id: va.id,
            label: va.name,
        })),
    );

    // Modal State
    let showAddModal = $state(false);
    let showDeleteConfirm = $state(false);
    let currentExpense = $state<Expense & { activeVersion: ExpenseVersion }>(createNewExpense() as any);
    let amountInput = $state("");
    let expenseToDelete = $state<string | null>(null);

    function createNewExpense(): Expense & { activeVersion: ExpenseVersion } {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            name: "",
            poolId: null,
            accountIds: [],
            activeVersion: {
                amount: 0,
                dueDate: monthStr,
                slices: [],
            },
        } as any;
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [eR, pR, vaR] = await Promise.all([
                wsCall("expenses::list", null, null, [ExpenseListSchema]).one(),
                wsCall("pools::list", null, null, [
                    TransactionPoolListSchema,
                ]).one(),
                wsCall("virtualaccounts::list", null, null, [
                    VirtualAccountListSchema,
                ]).one(),
            ]);

            if (eR[1]) throw eR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];

            expenses = (eR[0]?.expenses ?? []) as any;
            pools = pR[0]?.pools ?? [];
            virtualAccounts = vaR[0]?.virtualAccounts ?? [];
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    async function saveExpense() {
        if (!currentExpense.name) return;
        isSaving = true;
        try {
            if (!currentExpense.activeVersion) {
                currentExpense.activeVersion = {
                    amount: 0,
                    dueDate: new Date().toISOString(),
                    slices: [],
                };
            }
            currentExpense.activeVersion.amount =
                parseGermanAmount(amountInput);
            const [, err] = await wsCall(
                "expenses::save",
                ExpenseSchema,
                {
                    id: currentExpense.id || "",
                    name: currentExpense.name,
                    poolId: currentExpense.poolId || "",
                    accountIds: currentExpense.accountIds || [],
                    activeVersion: {
                        id: currentExpense.activeVersion.id || "",
                        expenseId:
                            currentExpense.activeVersion.expenseId || "",
                        amount: currentExpense.activeVersion.amount || 0,
                        dueDate: currentExpense.activeVersion.dueDate || "",
                        createdAt:
                            currentExpense.activeVersion.createdAt || "",
                        slices: (currentExpense.activeVersion.slices || []).map(
                            (s) => ({
                                id: s.id || "",
                                amount: s.amount,
                                intervalMonths: s.intervalMonths,
                                startDate: s.startDate,
                                endDate: s.endDate || "",
                                description: s.description,
                            }),
                        ),
                    },
                    linkToScenarios: currentExpense.linkToScenarios || [],
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showAddModal = false;
            await fetchData();
        } catch (err: any) {
            error = err.message;
        } finally {
            isSaving = false;
        }
    }

    function editExpense(expense: Expense) {
        currentExpense = decode(expense);
        if (!currentExpense.activeVersion) {
            currentExpense.activeVersion = {
                amount: 0,
                dueDate: new Date().toISOString(),
                slices: [],
            };
        }
        if (!currentExpense.activeVersion.slices) {
            currentExpense.activeVersion.slices = [];
        }
        amountInput = formatGermanAmount(currentExpense.activeVersion.amount);
        showAddModal = true;
    }

    function triggerDelete(id: string) {
        expenseToDelete = id;
        showDeleteConfirm = true;
    }

    async function confirmDelete(mode: "revert" | "full") {
        if (!expenseToDelete) return;
        try {
            const [, err] = await wsCall(
                "expenses::delete",
                GenericIDSchema,
                { id: expenseToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            expenseToDelete = null;
        } catch (err: any) {
            alert(err.message);
        }
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

    onMount(() => {
        fetchData();
    });
</script>

<svelte:window onkeydown={(e) => {
    if (e.key === 'Escape') {
        showAddModal = false;
        showDeleteConfirm = false;
    }
}} />

<div class="space-y-8">
    <!-- Header -->
    <div
        class="flex flex-col md:flex-row md:items-center justify-between gap-6"
    >
        <div>
            <h2
                class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500"
            >
                Variable Expenses
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                One-time deterministic costs and variable liabilities.
            </p>
        </div>
        <div class="flex gap-4">
            <button
                onclick={() => {
                    currentExpense = createNewExpense();
                    amountInput = "";
                    showAddModal = true;
                }}
                class="btn-primary bg-rose-600 hover:bg-rose-700 shadow-rose-200"
            >
                <Plus class="w-5 h-5" />
                Add Expense
            </button>
        </div>
    </div>

    {#if error}
        <div
            transition:fade
            class="glass-card p-6 border-rose-200 bg-rose-50/50 flex items-center gap-4 text-rose-600"
        >
            <AlertCircle class="w-6 h-6 flex-shrink-0" />
            <div class="flex-1">
                <p class="text-xs font-black uppercase tracking-widest">
                    Node Engine Error
                </p>
                <p class="text-sm font-bold">{error}</p>
            </div>
            <button
                onclick={fetchData}
                class="px-4 py-2 bg-rose-600 text-white rounded-xl text-[10px] font-black uppercase tracking-widest hover:bg-rose-700 transition-colors shadow-lg shadow-rose-200"
            >
                Retry
            </button>
        </div>
    {/if}

    {#if isLoading}
        <div class="flex flex-col items-center justify-center py-20 space-y-4">
            <Loader2 class="w-10 h-10 text-rose-600 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]"
            >
                Syncing Events...
            </p>
        </div>
    {:else if expenses.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div
                class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner"
            >
                <CreditCard class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Expenses Logged
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Add one-time events like furniture, travel, or down
                    payments.
                </p>
            </div>
            <button
                onclick={() => (showAddModal = true)}
                class="btn-secondary mx-auto"
            >
                Initialize First Entry
            </button>
        </div>
    {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {#each expenses as expense (expense.id)}
                <div
                    transition:fade
                    class="glass-card p-8 group hover:border-rose-200/50 transition-all duration-300 relative overflow-hidden"
                >
                    <div
                        class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-rose-500/0 via-rose-500/20 to-rose-500/0 opacity-0 group-hover:opacity-100 transition-opacity"
                    ></div>

                    <div class="flex justify-between items-start mb-8">
                        <div class="space-y-1">
                            <h3
                                class="text-xl font-black tracking-tight text-slate-900"
                            >
                                {expense.name}
                            </h3>
                            <div class="flex items-center gap-2">
                                <span
                                    class="px-2 py-0.5 bg-rose-50 text-rose-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]"
                                >
                                    One-Time
                                </span>
                                <span
                                    class="px-2 py-0.5 bg-slate-100 text-slate-400 rounded-md text-[9px] font-black uppercase tracking-[0.2em] flex items-center gap-1"
                                >
                                    <History class="w-2.5 h-2.5" /> Latest
                                </span>
                            </div>
                        </div>
                        <div class="flex gap-2">
                            <button
                                onclick={() => editExpense(expense)}
                                class="p-2.5 text-slate-400 hover:text-rose-600 hover:bg-rose-50 rounded-xl transition-all border border-transparent hover:border-rose-100"
                                title="Edit (Create New Version)"
                            >
                                <Pencil class="w-4 h-4" />
                            </button>
                            <button
                                onclick={() => {
                                    expenseToDelete = expense.id!;
                                    showDeleteConfirm = true;
                                }}
                                class="p-2.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-xl transition-all border border-transparent hover:border-red-100"
                                title="Delete/Revert"
                            >
                                <Trash2 class="w-4 h-4" />
                            </button>
                        </div>
                    </div>

                    <div class="space-y-6">
                        <div>
                            <p
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 mb-1"
                            >
                                Value
                            </p>
                            <p class="text-3xl font-black text-slate-900">
                                {formatGermanAmount(
                                    expense.activeVersion?.amount,
                                )} €
                            </p>
                        </div>

                        <div
                            class="flex items-center gap-6 pt-6 border-t border-slate-100"
                        >
                            <div class="space-y-1">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400 flex items-center gap-1"
                                >
                                    <Calendar class="w-3 h-3" /> Due Date
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {formatDate(expense.activeVersion?.dueDate)}
                                </p>
                            </div>
                        </div>
                    </div>
                </div>
            {/each}
        </div>
    {/if}
</div>

<!-- Add/Edit Modal -->
{#if showAddModal}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-lg bg-white rounded-[30px] shadow-2xl relative overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <button
                onclick={() => (showAddModal = false)}
                class="absolute top-6 right-6 text-slate-400 hover:text-slate-900 transition-colors"
            >
                <Plus class="w-6 h-6 rotate-45" />
            </button>

            <div class="p-10 space-y-10">
                <div>
                    <h3
                        class="text-2xl font-black text-slate-900 tracking-tight"
                    >
                        {currentExpense.id ? "Refine" : "New"} One-Time Expense
                    </h3>
                    <p class="text-slate-500 font-medium text-sm">
                        {currentExpense.id
                            ? "Changes will be saved as a new immutable version."
                            : "Define parameters for this deterministic event."}
                    </p>
                </div>

                <form
                    onsubmit={(e) => {
                        e.preventDefault();
                        saveExpense();
                    }}
                    class="space-y-8"
                >
                    <div class="space-y-6">
                        <div class="grid grid-cols-2 gap-6">
                            <div class="space-y-2">
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                    >Label</label
                                >
                                <input
                                    bind:value={currentExpense.name}
                                    placeholder="e.g. New Furniture"
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                                    required
                                />
                            </div>
                            <SearchableMultiSelect
                                label="Planned Account Link"
                                options={virtualAccountMultiOptions}
                                bind:values={currentExpense.accountIds}
                                placeholder="Select accounts..."
                            />
                        </div>
                        <div class="grid grid-cols-2 gap-6">
                            <SearchableDropdown
                                label="Realtime Pool Link"
                                options={poolOptions}
                                bind:value={currentExpense.poolId}
                                placeholder="None / Uncategorized"
                            />
                        </div>
                    </div>

                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Amount (€)</label
                            >
                            <div class="relative">
                                <div
                                    class="absolute inset-y-0 left-0 pl-4 flex items-center pointer-events-none"
                                >
                                    <Euro class="w-4 h-4 text-slate-400" />
                                </div>
                                <input
                                    type="text"
                                    bind:value={amountInput}
                                    placeholder="1.234,56"
                                    class="block w-full pl-10 pr-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                                    required
                                />
                            </div>
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Due Month</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentExpense.activeVersion.dueDate,
                                )}
                                oninput={(e: any) =>
                                    (currentExpense.activeVersion.dueDate =
                                        fromInputMonth(e.target.value))}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                                required
                            />
                        </div>
                    </div>

                    <div class="border-t border-slate-100 pt-8">
                        <TimeSliceManager
                            bind:slices={currentExpense.activeVersion.slices}
                            label="Time Variations (Makes Recurring)"
                        />
                    </div>

                    <div class="pt-6">
                        <button
                            disabled={isSaving}
                            class="btn-primary w-full py-4 text-lg shadow-2xl shadow-indigo-100 bg-indigo-600 hover:bg-indigo-700"
                        >
                            {#if isSaving}
                                <Loader2 class="w-6 h-6 animate-spin" />
                                <span>Versioning Data...</span>
                            {:else}
                                <span>Save as New Version</span>
                            {/if}
                        </button>
                    </div>
                </form>
            </div>
        </div>
    </div>
{/if}

<!-- Deletion Confirmation Modal -->
{#if showDeleteConfirm}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-md bg-white rounded-[30px] shadow-2xl p-10 relative overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="text-center space-y-2 mb-8">
                <h3 class="text-2xl font-black text-slate-900">
                    Manage Lifecycle
                </h3>
                <p class="text-slate-500 font-medium text-sm">
                    How should the WealthEngine handle this deletion?
                </p>
            </div>

            <div class="grid grid-cols-1 gap-4">
                <button
                    onclick={() => confirmDelete("revert")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-slate-50 hover:border-indigo-100 hover:bg-indigo-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-indigo-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Undo2 class="w-6 h-6 text-indigo-600" />
                    </div>
                    <div>
                        <p class="font-black text-slate-900 leading-tight">
                            Revert to Previous
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Delete only the latest version record.
                        </p>
                    </div>
                </button>

                <button
                    onclick={() => confirmDelete("full")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-slate-50 hover:border-rose-100 hover:bg-rose-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-rose-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Archive class="w-6 h-6 text-rose-600" />
                    </div>
                    <div>
                        <p class="font-black text-slate-900 leading-tight">
                            Full Archive
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Hide this expense and all versions.
                        </p>
                    </div>
                </button>
            </div>

            <button
                onclick={() => {
                    showDeleteConfirm = false;
                    expenseToDelete = null;
                }}
                class="w-full mt-8 py-3 text-slate-400 font-black uppercase tracking-[0.2em] text-[10px] hover:text-slate-900 transition-colors"
            >
                Cancel Action
            </button>
        </div>
    </div>
{/if}
