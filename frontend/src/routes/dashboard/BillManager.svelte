<script lang="ts">
    import { wsCall, decode } from "$lib/utils/ws_fetch";
    import {
        BillListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        BillSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount } from "$lib/utils/format";
    import { toInputMonth, fromInputMonth } from "$lib/utils/date";


    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Undo2,
        Archive,
        Pencil,
        Receipt,
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

    interface BillVersion {
        id?: string;
        billId?: string;
        amount: number;
        startDate: string;
        endDate: string | null;
        intervalMonths: number;
        createdAt?: string;
        slices: TimeSlice[];
    }

    interface Bill {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        linkToScenarios?: string[];
        activeVersion?: BillVersion;
    }

    let bills = $state<(Bill & { activeVersion: BillVersion })[]>([]);
    let sortedBills = $derived(
        [...bills].sort((a, b) => {
            const dateA = a.activeVersion?.startDate || "";
            const dateB = b.activeVersion?.startDate || "";
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
    let currentBill = $state<Bill & { activeVersion: BillVersion }>(createNewBill() as any);
    let billToDelete = $state<string | null>(null);

    function createNewBill(): Bill & { activeVersion: BillVersion } {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            name: "",
            poolId: null,
            accountIds: [],
            activeVersion: {
                amount: 0,
                startDate: monthStr,
                endDate: null,
                intervalMonths: 1,
                slices: [],
            },
        } as any;
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [bR, pR, vaR] = await Promise.all([
                wsCall("bills::list", null, null, [BillListSchema]).one(),
                wsCall("pools::list", null, null, [
                    TransactionPoolListSchema,
                ]).one(),
                wsCall("virtualaccounts::list", null, null, [
                    VirtualAccountListSchema,
                ]).one(),
            ]);

            if (bR[1]) throw bR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];

            bills = (bR[0]?.bills ?? []) as any;
            pools = pR[0]?.pools ?? [];
            virtualAccounts = vaR[0]?.virtualAccounts ?? [];
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    async function saveBill() {
        if (!currentBill.name) return;

        isSaving = true;
        try {
            const [, err] = await wsCall(
                "bills::save",
                BillSchema,
                {
                    id: currentBill.id || "",
                    name: currentBill.name,
                    poolId: currentBill.poolId || "",
                    accountIds: currentBill.accountIds || [],
                    activeVersion: {
                        id: currentBill.activeVersion.id || "",
                        billId: currentBill.activeVersion.billId || "",
                        amount: currentBill.activeVersion.amount || 0,
                        startDate: currentBill.activeVersion.startDate || "",
                        endDate: currentBill.activeVersion.endDate || "",
                        intervalMonths:
                            currentBill.activeVersion.intervalMonths || 1,
                        createdAt: currentBill.activeVersion.createdAt || "",
                        slices: (currentBill.activeVersion.slices || []).map(
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
                    linkToScenarios: currentBill.linkToScenarios || [],
                },
                [BillSchema, ErrorSchema],
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

    function editBill(bill: Bill) {
        currentBill = decode(bill);
        if (!currentBill.activeVersion) {
            currentBill.activeVersion = {
                amount: 0,
                startDate: new Date().toISOString(),
                endDate: null,
                intervalMonths: 1,
                slices: [],
            };
        } else {
            currentBill.activeVersion.amount = currentBill.activeVersion.amount ?? 0;
        }
        if (!currentBill.activeVersion.slices) {
            currentBill.activeVersion.slices = [];
        }
        showAddModal = true;
    }

    function triggerDelete(id: string) {
        billToDelete = id;
        showDeleteConfirm = true;
    }

    async function confirmDelete(mode: "revert" | "full") {
        if (!billToDelete) return;
        try {
            const [, err] = await wsCall(
                "bills::delete",
                GenericIDSchema,
                { id: billToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            billToDelete = null;
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
                Fixed Bills
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Fixed liabilities and recurring contracts.
            </p>
        </div>
        <div>
            <Button
                onclick={() => {
                    currentBill = createNewBill();
                    showAddModal = true;
                }}
                class="bg-amber-600 hover:bg-amber-700 shadow-amber-100"
            >
                <Plus class="w-5 h-5" />
                Add Bill
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
                    Connection Error
                </p>
                <p class="text-sm font-bold">{error}</p>
            </div>
            <Button
                onclick={fetchData}
                class="bg-rose-600 text-white hover:bg-rose-700 shadow-rose-200"
            >
                Retry
            </Button>
        </div>
    {/if}

    {#if isLoading}
        <div class="flex flex-col items-center justify-center py-20 space-y-4">
            <div class="w-10 h-10 border-4 border-t-amber-600 border-amber-100 rounded-full animate-spin"></div>
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]">
                Syncing Debts...
            </p>
        </div>
    {:else if bills.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner">
                <Receipt class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Bills Registered
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Enter recurring expenses like rent, insurance, or subscriptions.
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
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Interval</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Start Date</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">End Date</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Value</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
            {/snippet}
            {#snippet body()}
                {#each sortedBills as bill (bill.id)}
                    <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                        <td class="px-6 py-4 font-bold text-slate-800">{bill.name}</td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            <Badge variant="warning">
                                {bill.activeVersion?.intervalMonths === 1
                                    ? "Monthly"
                                    : bill.activeVersion?.intervalMonths === 3
                                      ? "Quarterly"
                                      : "Yearly"}
                            </Badge>
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatDate(bill.activeVersion?.startDate)}</td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatDate(bill.activeVersion?.endDate)}</td>
                        <td class="px-6 py-4">
                            <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900">
                                <span>€</span>
                                <span>{formatGermanAmount(bill.activeVersion?.amount)}</span>
                            </div>
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="inline-flex gap-2">
                                <Button
                                    variant="ghost"
                                    onclick={() => editBill(bill)}
                                    title="Edit (Create New Version)"
                                    class="hover:text-amber-600 hover:bg-amber-50 hover:border-amber-100"
                                >
                                    <Pencil class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => triggerDelete(bill.id!)}
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
    title="{currentBill.id ? 'Edit' : 'New'} Recurring Bill"
    subtitle={currentBill.id ? 'Changes will be saved as a new immutable version.' : 'Define parameters for this recurring bill.'}
>
    <form
        onsubmit={(e) => {
            e.preventDefault();
            saveBill();
        }}
        class="space-y-8"
    >
        <div class="space-y-6">
            <div class="grid grid-cols-2 gap-6">
                <Input
                    label="Label"
                    bind:value={currentBill.name}
                    placeholder="e.g. Rent"
                    required
                />
                <SearchableMultiSelect
                    label="Planned Account Link"
                    options={virtualAccountMultiOptions}
                    bind:values={currentBill.accountIds}
                    placeholder="Select accounts..."
                />
            </div>
            <div class="grid grid-cols-2 gap-6">
                <SearchableDropdown
                    label="Realtime Pool Link"
                    options={poolOptions}
                    bind:value={currentBill.poolId}
                    placeholder="None / Uncategorized"
                />
            </div>
        </div>

        <div class="grid grid-cols-2 gap-6">
            <CurrencyInput
                label="Amount (€)"
                bind:value={currentBill.activeVersion.amount}
                required
            />
            <div class="space-y-2">
                <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                    Interval
                </label>
                <select
                    bind:value={currentBill.activeVersion.intervalMonths}
                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-amber-500/10 focus:border-amber-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                >
                    <option value={1}>Monthly</option>
                    <option value={3}>Quarterly</option>
                    <option value={12}>Yearly</option>
                </select>
            </div>
        </div>

        <div class="grid grid-cols-2 gap-6">
            <Input
                type="month"
                label="Start Month"
                value={toInputMonth(currentBill.activeVersion.startDate)}
                oninput={(e: any) => (currentBill.activeVersion.startDate = fromInputMonth(e.target.value))}
                required
            />
            <Input
                type="month"
                label="End Month (Optional)"
                value={toInputMonth(currentBill.activeVersion.endDate)}
                oninput={(e: any) => (currentBill.activeVersion.endDate = e.target.value ? fromInputMonth(e.target.value) : null)}
            />
        </div>

        <div class="border-t border-slate-100 dark:border-slate-800 pt-8">
            <TimeSliceManager
                bind:slices={currentBill.activeVersion.slices}
                label="Contractual Variations"
            />
        </div>

        <div class="pt-6">
            <Button
                type="submit"
                variant="primary"
                loading={isSaving}
                loadingLabel="Versioning Data..."
                class="w-full py-4 text-lg bg-amber-600 hover:bg-amber-700 shadow-amber-100"
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
            class="flex items-center gap-4 p-5 rounded-2xl border-2 border-amber-50 hover:border-amber-100 hover:bg-amber-50 dark:border-slate-800 dark:hover:bg-slate-800/50 transition-all text-left group"
        >
            <div class="p-3 bg-amber-100 dark:bg-amber-500/20 rounded-xl group-hover:scale-110 transition-transform">
                <Undo2 class="w-6 h-6 text-amber-600 dark:text-amber-400" />
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
            class="flex items-center gap-4 p-5 rounded-2xl border-2 border-rose-50 hover:border-rose-100 hover:bg-rose-50 dark:border-slate-800 dark:hover:bg-slate-800/50 transition-all text-left group"
        >
            <div class="p-3 bg-rose-100 dark:bg-rose-500/20 rounded-xl group-hover:scale-110 transition-transform">
                <Archive class="w-6 h-6 text-rose-600 dark:text-rose-400" />
            </div>
            <div>
                <p class="font-black text-slate-900 dark:text-slate-100 leading-tight">
                    Full Archive
                </p>
                <p class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Hide this bill and all versions.
                </p>
            </div>
        </button>
    </div>

    <Button
        variant="secondary"
        onclick={() => {
            showDeleteConfirm = false;
            billToDelete = null;
        }}
        class="mt-8 w-full"
    >
        Cancel Action
    </Button>
</ConfirmModal>
