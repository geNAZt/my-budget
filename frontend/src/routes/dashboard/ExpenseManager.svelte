<script lang="ts">
    import { wsCall, decode } from "$lib/utils/ws_fetch";
    import {
        ExpenseListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        ExpenseSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount } from "$lib/utils/format";
    import { toInputMonth, fromInputMonth } from "$lib/utils/date";


    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Calendar,
        Undo2,
        Archive,
        Pencil,
        CreditCard,
        AlertCircle,
    } from "@lucide/svelte";
    import { fade } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";
    import TimeSliceManager from "$lib/components/TimeSliceManager.svelte";
    import Button from "$lib/components/ui/Button.svelte";
    import Input from "$lib/components/ui/Input.svelte";
    import CurrencyInput from "$lib/components/ui/CurrencyInput.svelte";
    import Badge from "$lib/components/ui/Badge.svelte";
    import Modal from "$lib/components/ui/Modal.svelte";
    import ConfirmModal from "$lib/components/ui/ConfirmModal.svelte";
    import Table from "$lib/components/ui/Table.svelte";

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
    let sortedExpenses = $derived(
        [...expenses].sort((a, b) => {
            const dateA = a.activeVersion?.dueDate || "";
            const dateB = b.activeVersion?.dueDate || "";
            if (dateA !== dateB) {
                return dateA.localeCompare(dateB);
            }
            return (a.name || "").localeCompare(b.name || "");
        })
    );
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

<div class="space-y-8">
    <!-- Header -->
    <div class="flex flex-col md:flex-row md:items-center justify-between gap-6">
        <div>
            <h2 class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500">
                Variable Expenses
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                One-time deterministic costs and variable liabilities.
            </p>
        </div>
        <div>
            <Button
                onclick={() => {
                    currentExpense = createNewExpense();
                    showAddModal = true;
                }}
                class="bg-rose-600 hover:bg-rose-700 shadow-rose-100"
            >
                <Plus class="w-5 h-5" />
                Add Expense
            </Button>
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
            <Button
                onclick={fetchData}
                class="bg-rose-600 text-white hover:bg-rose-700 shadow-rose-100"
            >
                Retry
            </Button>
        </div>
    {/if}

    {#if isLoading}
        <div class="flex flex-col items-center justify-center py-20 space-y-4">
            <div class="w-10 h-10 border-4 border-t-rose-600 border-rose-100 rounded-full animate-spin"></div>
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]">
                Syncing Events...
            </p>
        </div>
    {:else if expenses.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner">
                <CreditCard class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Expenses Logged
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Add one-time events like furniture, travel, or down payments.
                </p>
            </div>
            <Button
                variant="secondary"
                onclick={() => (showAddModal = true)}
                class="mx-auto"
            >
                Initialize First Entry
            </Button>
        </div>
    {:else}
        <Table>
            {#snippet header()}
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Name</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Type</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Due Date</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Value</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
            {/snippet}
            {#snippet body()}
                {#each sortedExpenses as expense (expense.id)}
                    <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                        <td class="px-6 py-4 font-bold text-slate-800">{expense.name}</td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            <Badge variant="error">One-Time</Badge>
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatDate(expense.activeVersion?.dueDate)}</td>
                        <td class="px-6 py-4">
                            <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900">
                                <span>€</span>
                                <span>{formatGermanAmount(expense.activeVersion?.amount)}</span>
                            </div>
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="inline-flex gap-2">
                                <Button
                                    variant="ghost"
                                    onclick={() => editExpense(expense)}
                                    title="Edit (Create New Version)"
                                    class="hover:text-rose-600 hover:bg-rose-50 hover:border-rose-100"
                                >
                                    <Pencil class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => triggerDelete(expense.id!)}
                                    title="Delete/Revert"
                                    class="hover:text-red-600 hover:bg-red-50 hover:border-red-100"
                                >
                                    <Trash2 class="w-4 h-4" />
                                </Button>
                            </div>
                        </td>
                    </tr>
                {/each}
            {/snippet}
        </Table>
    {/if}
</div>

<!-- Add/Edit Modal -->
<Modal
    bind:open={showAddModal}
    title="{currentExpense.id ? 'Refine' : 'New'} One-Time Expense"
    subtitle={currentExpense.id ? 'Changes will be saved as a new immutable version.' : 'Define parameters for this deterministic event.'}
>
    <form
        onsubmit={(e) => {
            e.preventDefault();
            saveExpense();
        }}
        class="space-y-8"
    >
        <div class="space-y-6">
            <div class="grid grid-cols-2 gap-6">
                <Input
                    label="Label"
                    bind:value={currentExpense.name}
                    placeholder="e.g. New Furniture"
                    required
                />
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
            <CurrencyInput
                label="Amount (€)"
                bind:value={currentExpense.activeVersion.amount}
                required
            />
            <Input
                type="month"
                label="Due Month"
                value={toInputMonth(currentExpense.activeVersion.dueDate)}
                oninput={(e: any) => (currentExpense.activeVersion.dueDate = fromInputMonth(e.target.value))}
                required
            />
        </div>

        <div class="border-t border-slate-100 pt-8 dark:border-slate-800">
            <TimeSliceManager
                bind:slices={currentExpense.activeVersion.slices}
                label="Time Variations (Makes Recurring)"
            />
        </div>

        <div class="pt-6">
            <Button
                type="submit"
                variant="primary"
                loading={isSaving}
                loadingLabel="Versioning Data..."
                class="w-full py-4 text-lg"
            >
                Save as New Version
            </Button>
        </div>
    </form>
</Modal>

<!-- Deletion Confirmation Modal -->
<ConfirmModal
    bind:open={showDeleteConfirm}
    title="Manage Lifecycle"
    description="How should the WealthEngine handle this deletion?"
>
    <div class="grid grid-cols-1 gap-4">
        <button
            onclick={() => confirmDelete("revert")}
            class="flex items-center gap-4 p-5 rounded-2xl border-2 border-slate-50 hover:border-indigo-100 hover:bg-indigo-50 dark:border-slate-800 dark:hover:bg-slate-800/50 transition-all text-left group"
        >
            <div class="p-3 bg-indigo-100 dark:bg-indigo-500/20 rounded-xl group-hover:scale-110 transition-transform">
                <Undo2 class="w-6 h-6 text-indigo-600 dark:text-indigo-400" />
            </div>
            <div>
                <p class="font-black text-slate-900 dark:text-slate-100 leading-tight">
                    Revert to Previous
                </p>
                <p class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Delete only the latest version record.
                </p>
            </div>
        </button>

        <button
            onclick={() => confirmDelete("full")}
            class="flex items-center gap-4 p-5 rounded-2xl border-2 border-slate-50 hover:border-rose-100 hover:bg-rose-50 dark:border-slate-800 dark:hover:bg-slate-800/50 transition-all text-left group"
        >
            <div class="p-3 bg-rose-100 dark:bg-rose-500/20 rounded-xl group-hover:scale-110 transition-transform">
                <Archive class="w-6 h-6 text-rose-600 dark:text-rose-400" />
            </div>
            <div>
                <p class="font-black text-slate-900 dark:text-slate-100 leading-tight">
                    Full Archive
                </p>
                <p class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Hide this expense and all versions.
                </p>
            </div>
        </button>
    </div>

    <Button
        variant="secondary"
        onclick={() => {
            showDeleteConfirm = false;
            expenseToDelete = null;
        }}
        class="mt-8 w-full"
    >
        Cancel Action
    </Button>
</ConfirmModal>
