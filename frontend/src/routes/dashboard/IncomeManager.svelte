<script lang="ts">
    import { wsCall, decode } from "$lib/utils/ws_fetch";
    import {
        IncomeListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        ScenarioListSchema,
        ModificationListSchema,
        IncomeSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount } from "$lib/utils/format";
    import { toInputMonth, fromInputMonth } from "$lib/utils/date";


    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Euro,
        Undo2,
        Archive,
        Pencil,
        AlertCircle,
        Layers,
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

    interface IncomeVersion {
        id?: string;
        incomeId?: string;
        amount: number;
        stopModificationId: string | null;
        startDate: string;
        endDate: string | null;
        intervalMonths: number;
        createdAt?: string;
        slices: TimeSlice[];
        intervalIncreasePercentage: number;
        intervalIncreaseMonths: number;
        intervalIncreaseStartDate: string | null;
    }

    interface Income {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        activeVersion?: IncomeVersion;
        linkToScenarios?: string[]; // Selection for new entities
    }

    let incomes = $state<(Income & { activeVersion: IncomeVersion })[]>([]);
    let sortedIncomes = $derived(
        [...incomes].sort((a, b) => {
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

    // Context for linking
    let scenarios = $state<any[]>([]);
    let modifications = $state<any[]>([]);

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
    let currentIncome = $state<Income & { activeVersion: IncomeVersion }>(createNewIncome() as any);
    let incomeToDelete = $state<string | null>(null);

    function createNewIncome(): Income & { activeVersion: IncomeVersion } {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            name: "",
            poolId: null,
            accountIds: [],
            activeVersion: {
                amount: 0,
                stopModificationId: null,
                startDate: monthStr,
                endDate: null,
                intervalMonths: 1,
                slices: [],
                intervalIncreasePercentage: 0,
                intervalIncreaseMonths: 0,
                intervalIncreaseStartDate: null,
            },
            linkToScenarios: [],
        } as any;
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [iR, pR, vaR, sR, mR] = await Promise.all([
                wsCall("incomes::list", null, null, [IncomeListSchema]).one(),
                wsCall("pools::list", null, null, [
                    TransactionPoolListSchema,
                ]).one(),
                wsCall("virtualaccounts::list", null, null, [
                    VirtualAccountListSchema,
                ]).one(),
                wsCall("scenarios::list", null, null, [
                    ScenarioListSchema,
                ]).one(),
                wsCall("modifications::list", null, null, [
                    ModificationListSchema,
                ]).one(),
            ]);

            if (iR[1]) throw iR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];
            if (sR[1]) throw sR[1];
            if (mR[1]) throw mR[1];

            incomes = (iR[0]?.incomes ?? []) as any;
            pools = pR[0]?.pools ?? [];
            virtualAccounts = vaR[0]?.virtualAccounts ?? [];
            scenarios = sR[0]?.scenarios ?? [];
            modifications = mR[0]?.modifications ?? [];
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    async function saveIncome() {
        if (!currentIncome.name || !currentIncome.activeVersion) return;
        isSaving = true;
        try {
            const [, err] = await wsCall(
                "incomes::save",
                IncomeSchema,
                {
                    id: currentIncome.id || "",
                    name: currentIncome.name,
                    poolId: currentIncome.poolId || "",
                    accountIds: currentIncome.accountIds || [],
                    activeVersion: {
                        id: currentIncome.activeVersion.id || "",
                        incomeId: currentIncome.activeVersion.incomeId || "",
                        amount: currentIncome.activeVersion.amount || 0,
                        stopModificationId:
                            currentIncome.activeVersion.stopModificationId ||
                            "",
                        startDate: currentIncome.activeVersion.startDate || "",
                        endDate: currentIncome.activeVersion.endDate || "",
                        intervalMonths:
                            currentIncome.activeVersion.intervalMonths || 1,
                        createdAt: currentIncome.activeVersion.createdAt || "",
                        slices: (currentIncome.activeVersion.slices || []).map(
                            (s) => ({
                                id: s.id || "",
                                amount: s.amount,
                                intervalMonths: s.intervalMonths,
                                startDate: s.startDate,
                                endDate: s.endDate || "",
                                description: s.description,
                            }),
                        ),
                        intervalIncreasePercentage:
                            currentIncome.activeVersion
                                .intervalIncreasePercentage || 0,
                        intervalIncreaseMonths:
                            currentIncome.activeVersion
                                .intervalIncreaseMonths || 0,
                        intervalIncreaseStartDate:
                            currentIncome.activeVersion
                                .intervalIncreaseStartDate || "",
                    },
                    linkToScenarios: currentIncome.linkToScenarios || [],
                },
                [IncomeSchema, ErrorSchema],
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

    function editIncome(income: Income) {
        currentIncome = decode(income);
        if (!currentIncome.activeVersion) {
            currentIncome.activeVersion = {
                amount: 0,
                stopModificationId: null,
                startDate: new Date().toISOString(),
                endDate: null,
                intervalMonths: 1,
                slices: [],
                intervalIncreasePercentage: 0,
                intervalIncreaseMonths: 0,
                intervalIncreaseStartDate: null,
            };
        } else {
            currentIncome.activeVersion.amount = currentIncome.activeVersion.amount ?? 0;
        }
        if (!currentIncome.activeVersion.slices) {
            currentIncome.activeVersion.slices = [];
        }
        if (currentIncome.activeVersion.intervalIncreasePercentage === undefined) {
            currentIncome.activeVersion.intervalIncreasePercentage = 0;
            currentIncome.activeVersion.intervalIncreaseMonths = 0;
            currentIncome.activeVersion.intervalIncreaseStartDate = null;
        }
        showAddModal = true;
    }

    function triggerDelete(id: string) {
        incomeToDelete = id;
        showDeleteConfirm = true;
    }

    async function confirmDelete(mode: "revert" | "full") {
        if (!incomeToDelete) return;
        try {
            const [, err] = await wsCall(
                "incomes::delete",
                GenericIDSchema,
                { id: incomeToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            incomeToDelete = null;
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
                Income Sources
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Fixed revenue streams and monthly income.
            </p>
        </div>
        <div>
            <Button
                onclick={() => {
                    currentIncome = createNewIncome();
                    showAddModal = true;
                }}
            >
                <Plus class="w-5 h-5" />
                Add Income
            </Button>
        </div>
    </div>

    {#if error}
        <div
            transition:fade
            class="glass-card p-6 border-rose-200 bg-rose-50/50 flex items-center gap-4 text-rose-600 animate-fade-in"
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
            <div class="w-10 h-10 border-4 border-t-indigo-600 border-indigo-100 rounded-full animate-spin"></div>
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]">
                Syncing Versions...
            </p>
        </div>
    {:else if incomes.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner">
                <Euro class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Income Active
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Start your financial model by adding a revenue source.
                </p>
            </div>
            <Button
                variant="secondary"
                onclick={() => (showAddModal = true)}
                class="mx-auto"
            >
                Create First Entry
            </Button>
        </div>
    {:else}
        <Table>
            {#snippet header()}
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Name</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Interval</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">From</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">To</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Value</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
            {/snippet}
            {#snippet body()}
                {#each sortedIncomes as income (income.id)}
                    <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                        <td class="px-6 py-4 font-bold text-slate-800">{income.name}</td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            <div class="flex items-center gap-2">
                                <Badge variant="brand">
                                    {income.activeVersion?.intervalMonths === 1
                                        ? "Monthly"
                                        : income.activeVersion?.intervalMonths === 3
                                          ? "Quarterly"
                                          : "Yearly"}
                                </Badge>
                                {#if income.activeVersion?.stopModificationId}
                                    <Badge variant="warning">
                                        Auto-Stop
                                    </Badge>
                                {/if}
                            </div>
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatDate(income.activeVersion?.startDate)}</td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatDate(income.activeVersion?.endDate)}</td>
                        <td class="px-6 py-4">
                            <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900">
                                <span>€</span>
                                <span>{formatGermanAmount(income.activeVersion?.amount)}</span>
                            </div>
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="inline-flex gap-2">
                                <Button
                                    variant="ghost"
                                    onclick={() => editIncome(income)}
                                    title="Edit (Create New Version)"
                                    class="hover:text-indigo-600 hover:bg-indigo-50 hover:border-indigo-100"
                                >
                                    <Pencil class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => triggerDelete(income.id!)}
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
    title="{currentIncome.id ? 'Edit' : 'New'} Income Stream"
    subtitle={currentIncome.id ? 'Changes will be saved as a new immutable version.' : 'Define parameters for this income stream.'}
    maxWidth="max-w-2xl"
>
    {#if currentIncome.activeVersion}
        <form
            onsubmit={(e) => {
                e.preventDefault();
                saveIncome();
            }}
            class="space-y-8"
        >
            <div class="space-y-6">
                <div class="grid grid-cols-2 gap-6">
                    <Input
                        label="Label"
                        bind:value={currentIncome.name}
                        placeholder="e.g. Primary Salary"
                        required
                    />
                    <SearchableMultiSelect
                        label="Planned Account Link"
                        options={virtualAccountMultiOptions}
                        bind:values={currentIncome.accountIds}
                        placeholder="Select accounts..."
                    />
                </div>
                <div class="grid grid-cols-2 gap-6">
                    <SearchableDropdown
                        label="Realtime Pool Link"
                        options={poolOptions}
                        bind:value={currentIncome.poolId}
                        placeholder="None / Uncategorized"
                    />
                </div>
            </div>

            <div class="grid grid-cols-2 gap-6">
                <CurrencyInput
                    label="Amount (€)"
                    bind:value={currentIncome.activeVersion.amount}
                    required
                />
                <div class="space-y-2">
                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                        Interval
                    </label>
                    <div class="relative">
                        <select
                            bind:value={currentIncome.activeVersion.intervalMonths}
                            class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                        >
                            <option value={1}>Monthly</option>
                            <option value={3}>Quarterly</option>
                            <option value={12}>Yearly</option>
                        </select>
                    </div>
                </div>
            </div>

            <div class="p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm space-y-6">
                <div class="flex items-center gap-3">
                    <div class="p-2 bg-indigo-50 dark:bg-indigo-500/10 rounded-lg">
                        <Plus class="w-4 h-4 text-indigo-600 dark:text-indigo-400" />
                    </div>
                    <h4 class="text-sm font-black text-slate-900 dark:text-slate-100 uppercase tracking-widest">
                        Interval Increase
                    </h4>
                </div>

                <div class="grid grid-cols-2 gap-6">
                    <Input
                        type="number"
                        step="0.01"
                        label="Increase (%)"
                        bind:value={currentIncome.activeVersion.intervalIncreasePercentage}
                        placeholder="e.g. 2.0"
                    />
                    <Input
                        type="number"
                        label="Increase Interval (Months)"
                        bind:value={currentIncome.activeVersion.intervalIncreaseMonths}
                        placeholder="e.g. 12"
                    />
                </div>

                <Input
                    type="month"
                    label="Increase Start Month"
                    value={toInputMonth(currentIncome.activeVersion.intervalIncreaseStartDate)}
                    oninput={(e: any) => (currentIncome.activeVersion.intervalIncreaseStartDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                />
            </div>

            <div class="p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm space-y-4">
                <div class="space-y-2">
                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                        End Income Trigger
                    </label>
                    <select
                        bind:value={currentIncome.activeVersion.stopModificationId}
                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                    >
                        <option value={null}>None (Ongoing)</option>
                        {#each modifications.filter((m) => m.activeVersion.withdrawalPercentage > 0) as m}
                            <option value={m.id}>
                                {m.description} ({m.activeVersion.withdrawalPercentage}% Dynamic)
                            </option>
                        {/each}
                    </select>
                    <p class="text-[9px] font-medium text-slate-500 dark:text-slate-400 ml-1">
                        Automatically stop this income once the selected lifestyle adjustment starts.
                    </p>
                </div>
            </div>

            <div class="grid grid-cols-2 gap-6">
                <Input
                    type="month"
                    label="Start Month"
                    value={toInputMonth(currentIncome.activeVersion.startDate)}
                    oninput={(e: any) => (currentIncome.activeVersion.startDate = fromInputMonth(e.target.value))}
                    required
                />
                <Input
                    type="month"
                    label="End Month (Optional)"
                    value={toInputMonth(currentIncome.activeVersion.endDate)}
                    oninput={(e: any) => (currentIncome.activeVersion.endDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                />
            </div>

            <div class="border-t border-slate-100 dark:border-slate-800 pt-8">
                <TimeSliceManager
                    bind:slices={currentIncome.activeVersion.slices}
                    label="Revenue Variations"
                />
            </div>

            <!-- Scenario Linker (New Entities Only) -->
            {#if !currentIncome.id && scenarios.length > 0}
                <div class="space-y-4 pt-4 border-t border-slate-100 dark:border-slate-800">
                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 block">
                        Contextual Onboarding
                    </label>
                    <p class="text-[10px] text-slate-500 dark:text-slate-400 font-medium">
                        Auto-link this income to specific financial scenarios:
                    </p>
                    <div class="flex flex-wrap gap-2">
                        {#each scenarios as s}
                            <Button
                                variant={currentIncome.linkToScenarios?.includes(s.id) ? 'primary' : 'secondary'}
                                onclick={() => {
                                    if (currentIncome.linkToScenarios?.includes(s.id)) {
                                        currentIncome.linkToScenarios = currentIncome.linkToScenarios.filter((id) => id !== s.id);
                                    } else {
                                        currentIncome.linkToScenarios = [...(currentIncome.linkToScenarios || []), s.id];
                                    }
                                }}
                                class="px-3 py-1.5 text-[10px]"
                            >
                                <Layers class="w-3 h-3" />
                                {s.name}
                            </Button>
                        {/each}
                    </div>
                </div>
            {/if}

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
    {/if}
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
                    Hide this income source and all versions.
                </p>
            </div>
        </button>
    </div>

    <Button
        variant="secondary"
        onclick={() => {
            showDeleteConfirm = false;
            incomeToDelete = null;
        }}
        class="mt-8 w-full"
    >
        Cancel Action
    </Button>
</ConfirmModal>
