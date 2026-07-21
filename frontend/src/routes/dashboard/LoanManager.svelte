<script lang="ts">
    import { wsCall, decode } from "$lib/utils/ws_fetch";
    import {
        LoanListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        LoanSchema,
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
        AlertCircle,
        HandCoins,
    } from "@lucide/svelte";

    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";
    import Button from "$lib/components/ui/Button.svelte";
    import Input from "$lib/components/ui/Input.svelte";
    import CurrencyInput from "$lib/components/ui/CurrencyInput.svelte";
    import Badge from "$lib/components/ui/Badge.svelte";
    import Modal from "$lib/components/ui/Modal.svelte";
    import ConfirmModal from "$lib/components/ui/ConfirmModal.svelte";
    import Table from "$lib/components/ui/Table.svelte";

    interface LoanVersion {
        id?: string;
        loanId?: string;
        amountLent: number;
        interestRate: number;
        runtimeMonths: number;
        startDate: string;
        remainderStartDate: string | null;
        priority: number;
        nextLoanId: string | null;
        balloonLeftover: number;
        isInterestOnly: boolean;
        earlyPayoffPenalty: number;
        createdAt?: string;
    }

    interface Loan {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        activeVersion?: LoanVersion;
        linkToScenarios?: string[];
    }

    let loans = $state<Loan[]>([]);
    let sortedLoans = $derived(
        [...loans].sort((a, b) => {
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
    let currentLoan = $state<Loan>(createNewLoan());
    let loanToDelete = $state<string | null>(null);

    function createNewLoan(): Loan {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            name: "",
            poolId: null,
            accountIds: [],
            activeVersion: {
                amountLent: 0,
                interestRate: 0,
                runtimeMonths: 12,
                startDate: monthStr,
                remainderStartDate: null,
                priority: 0,
                nextLoanId: null,
                balloonLeftover: 0,
                isInterestOnly: false,
                earlyPayoffPenalty: 1,
            },
        };
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [lR, pR, vaR] = await Promise.all([
                wsCall("loans::list", null, null, [LoanListSchema]).one(),
                wsCall("pools::list", null, null, [
                    TransactionPoolListSchema,
                ]).one(),
                wsCall("virtualaccounts::list", null, null, [
                    VirtualAccountListSchema,
                ]).one(),
            ]);

            if (lR[1]) throw lR[1];
            if (pR[1]) throw pR[1];
            if (vaR[1]) throw vaR[1];

            loans = lR[0]?.loans ?? [];
            pools = pR[0]?.pools ?? [];
            virtualAccounts = vaR[0]?.virtualAccounts ?? [];
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    async function saveLoan() {
        if (!currentLoan.name) return;
        isSaving = true;
        try {
            const [, err] = await wsCall(
                "loans::save",
                LoanSchema,
                {
                    id: currentLoan.id || "",
                    name: currentLoan.name,
                    poolId: currentLoan.poolId || "",
                    accountIds: currentLoan.accountIds || [],
                    activeVersion: currentLoan.activeVersion
                        ? {
                              id: currentLoan.activeVersion.id || "",
                              loanId: currentLoan.activeVersion.loanId || "",
                              amountLent:
                                  currentLoan.activeVersion.amountLent || 0,
                              interestRate:
                                  currentLoan.activeVersion.interestRate || 0,
                              runtimeMonths:
                                  currentLoan.activeVersion.runtimeMonths || 0,
                              startDate:
                                  currentLoan.activeVersion.startDate || "",
                              remainderStartDate:
                                  currentLoan.activeVersion
                                      .remainderStartDate || "",
                              priority: currentLoan.activeVersion.priority || 0,
                              nextLoanId:
                                  currentLoan.activeVersion.nextLoanId || "",
                              balloonLeftover:
                                  currentLoan.activeVersion.balloonLeftover || 0,
                              isInterestOnly:
                                  currentLoan.activeVersion.isInterestOnly || false,
                              earlyPayoffPenalty:
                                  currentLoan.activeVersion.earlyPayoffPenalty || 0,
                          }
                        : undefined,
                },
                [LoanSchema, ErrorSchema],
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

    function editLoan(loan: Loan) {
        currentLoan = decode(loan);
        if (!currentLoan.activeVersion) {
            currentLoan.activeVersion = {
                amountLent: 0,
                interestRate: 0,
                runtimeMonths: 12,
                startDate: new Date().toISOString(),
                remainderStartDate: null,
                priority: 0,
                nextLoanId: null,
                balloonLeftover: 0,
                isInterestOnly: false,
                earlyPayoffPenalty: 1,
            };
        } else {
            currentLoan.activeVersion.amountLent = currentLoan.activeVersion.amountLent ?? 0;
            currentLoan.activeVersion.balloonLeftover = currentLoan.activeVersion.balloonLeftover ?? 0;
        }
        showAddModal = true;
    }

    function triggerDelete(id: string) {
        loanToDelete = id;
        showDeleteConfirm = true;
    }

    async function confirmDelete(mode: "revert" | "full") {
        if (!loanToDelete) return;
        try {
            const [, err] = await wsCall(
                "loans::delete",
                GenericIDSchema,
                { id: loanToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            loanToDelete = null;
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
                Liabilities
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Amortization schedules and debt structures.
            </p>
        </div>
        <div>
            <Button
                onclick={() => {
                    currentLoan = createNewLoan();
                    showAddModal = true;
                }}
            >
                <Plus class="w-5 h-5" />
                Add Loan
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
            <div class="w-10 h-10 border-4 border-t-indigo-600 border-indigo-100 rounded-full animate-spin"></div>
            <p class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]">
                Syncing Debts...
            </p>
        </div>
    {:else if loans.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner">
                <HandCoins class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Liabilities Logged
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Keep track of loans, mortgage schedules, or amortization flows.
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
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Started</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Runtime</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Interest</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Principal</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
            {/snippet}
            {#snippet body()}
                {#each sortedLoans as loan (loan.id)}
                    <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                        <td class="px-6 py-4">
                            <div class="font-bold text-slate-800 dark:text-slate-100">{loan.name}</div>
                            {#if loan.activeVersion?.nextLoanId}
                                <div class="text-[9px] font-black text-emerald-600 dark:text-emerald-400 uppercase tracking-wider mt-0.5">Linked</div>
                            {/if}
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            <Badge variant="slate">
                                {loan.activeVersion && loan.activeVersion.balloonLeftover > 0
                                    ? "Balloon"
                                    : loan.activeVersion?.isInterestOnly
                                      ? "Interest-Only"
                                      : "Standard"}
                            </Badge>
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatDate(loan.activeVersion?.startDate || null)}</td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{loan.activeVersion?.runtimeMonths || 0} Months</td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">{formatGermanAmount(loan.activeVersion?.interestRate || 0)}%</td>
                        <td class="px-6 py-4">
                            <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900 dark:text-slate-100">
                                <span>€</span>
                                <span>{formatGermanAmount(loan.activeVersion?.amountLent || 0)}</span>
                            </div>
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="inline-flex gap-2">
                                <Button
                                    variant="ghost"
                                    onclick={() => editLoan(loan)}
                                    title="Edit (Create New Version)"
                                    class="hover:text-slate-900 hover:bg-slate-50 hover:border-slate-200"
                                >
                                    <Pencil class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => triggerDelete(loan.id!)}
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

<!-- Add Modal -->
<Modal
    bind:open={showAddModal}
    title="{currentLoan.id ? 'Edit' : 'New'} Liability"
    subtitle="Define amortization schedule and interest rate parameters."
    maxWidth="max-w-2xl"
>
    <form
        onsubmit={(e) => {
            e.preventDefault();
            saveLoan();
        }}
        class="space-y-8"
    >
        <div class="space-y-6">
            <div class="grid grid-cols-2 gap-6">
                <Input
                    label="Loan Identity"
                    bind:value={currentLoan.name}
                    placeholder="e.g. SWK Loan"
                    required
                />
                <SearchableMultiSelect
                    label="Planned Account Link"
                    options={virtualAccountMultiOptions}
                    bind:values={currentLoan.accountIds}
                    placeholder="Select accounts..."
                />
            </div>
            <div class="grid grid-cols-2 gap-6">
                <SearchableDropdown
                    label="Realtime Pool Link"
                    options={poolOptions}
                    bind:value={currentLoan.poolId}
                    placeholder="None / Uncategorized"
                />
            </div>
        </div>

        {#if currentLoan.activeVersion}
            <div class="grid grid-cols-2 gap-6">
                <CurrencyInput
                    label="Principal (€)"
                    bind:value={currentLoan.activeVersion.amountLent}
                    required
                />
                <Input
                    type="number"
                    step="0.01"
                    label="Interest %"
                    bind:value={currentLoan.activeVersion.interestRate}
                    placeholder="5,83"
                    required
                />
            </div>

            <div class="grid grid-cols-2 gap-6">
                <Input
                    type="number"
                    label="Runtime (Months)"
                    bind:value={currentLoan.activeVersion.runtimeMonths}
                    required
                />
                <Input
                    type="month"
                    label="Start Month"
                    value={toInputMonth(currentLoan.activeVersion.startDate)}
                    oninput={(e: any) => (currentLoan.activeVersion!.startDate = fromInputMonth(e.target.value))}
                    required
                />
            </div>

            <div class="grid grid-cols-2 gap-6">
                <CurrencyInput
                    label="Balloon Leftover (€)"
                    bind:value={currentLoan.activeVersion.balloonLeftover}
                    required={false}
                />
                <Input
                    type="month"
                    label="Remainder Start"
                    value={toInputMonth(currentLoan.activeVersion.remainderStartDate)}
                    oninput={(e: any) => (currentLoan.activeVersion!.remainderStartDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                />
            </div>

            <div class="grid grid-cols-2 gap-6">
                <Input
                    type="number"
                    step="0.01"
                    label="Early Payoff Penalty %"
                    bind:value={currentLoan.activeVersion.earlyPayoffPenalty}
                    placeholder="1,00"
                />
            </div>

            <div class="flex items-center gap-3 ml-1">
                <input
                    type="checkbox"
                    bind:checked={currentLoan.activeVersion.isInterestOnly}
                    class="w-5 h-5 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                />
                <span class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">
                    Interest Only
                </span>
            </div>

            <!-- Loan Replacement Section -->
            <div class="space-y-4 p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm">
                <div class="flex items-center justify-between">
                    <div class="space-y-0.5">
                        <label class="text-sm font-black text-slate-900 dark:text-slate-100">
                            Enable Loan Replacement
                        </label>
                        <p class="text-[10px] font-medium text-slate-500 dark:text-slate-400">
                            Link to another loan configuration for rollover.
                        </p>
                    </div>
                    <button
                        type="button"
                        onclick={() => {
                            if (currentLoan.activeVersion!.nextLoanId) {
                                currentLoan.activeVersion!.nextLoanId = null;
                            } else {
                                const firstOther = loans.find((l) => l.id !== currentLoan.id);
                                if (firstOther) {
                                    currentLoan.activeVersion!.nextLoanId = firstOther.id!;

                                    // Trigger date sync
                                    const startDate = new Date(currentLoan.activeVersion!.startDate);
                                    const endDate = new Date(
                                        startDate.setMonth(
                                            startDate.getMonth() + currentLoan.activeVersion!.runtimeMonths,
                                        ),
                                    );
                                    const endDateStr = endDate.toISOString().substring(0, 7) + "-01T00:00:00Z";
                                    if (firstOther.activeVersion) {
                                        firstOther.activeVersion.startDate = endDateStr;
                                    }
                                }
                            }
                        }}
                        class="w-12 h-6 rounded-full transition-all relative {currentLoan.activeVersion.nextLoanId ? 'bg-indigo-600 shadow-lg shadow-indigo-100 dark:shadow-none' : 'bg-slate-200 dark:bg-slate-700'}"
                    >
                        <div class="absolute top-1 left-1 w-4 h-4 bg-white rounded-full transition-all {currentLoan.activeVersion.nextLoanId ? 'translate-x-6' : ''}"></div>
                    </button>
                </div>

                {#if currentLoan.activeVersion.nextLoanId}
                    <div class="space-y-2" transition:slide>
                        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                            Next Loan in Chain
                        </label>
                        <select
                            bind:value={currentLoan.activeVersion.nextLoanId}
                            onchange={() => {
                                const nextLoan = loans.find((l) => l.id === currentLoan.activeVersion!.nextLoanId);
                                if (nextLoan) {
                                    const startDate = new Date(currentLoan.activeVersion!.startDate);
                                    const endDate = new Date(
                                        startDate.setMonth(
                                            startDate.getMonth() + currentLoan.activeVersion!.runtimeMonths,
                                        ),
                                    );
                                    const endDateStr = endDate.toISOString().substring(0, 7) + "-01T00:00:00Z";
                                    if (nextLoan.activeVersion) {
                                        nextLoan.activeVersion.startDate = endDateStr;
                                    }
                                }
                            }}
                            class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                        >
                            {#each loans.filter((l) => l.id !== currentLoan.id) as loan}
                                <option value={loan.id}>{loan.name}</option>
                            {/each}
                        </select>
                    </div>
                {/if}
            </div>
        {/if}

        <div class="pt-6">
            <Button
                type="submit"
                variant="primary"
                loading={isSaving}
                loadingLabel="Saving..."
                class="w-full py-4 text-lg"
            >
                Save Liability Settings
            </Button>
        </div>
    </form>
</Modal>

<!-- Delete Modal -->
<ConfirmModal
    bind:open={showDeleteConfirm}
    title="Liability Lifecycle Options"
    description="How would you like to handle this delete action?"
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
                    Revert Version
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
                <p class="font-black text-rose-600 dark:text-rose-400 leading-tight">
                    Archive Liability
                </p>
                <p class="text-xs text-slate-500 dark:text-slate-400 font-medium">
                    Hide this liability and all versions.
                </p>
            </div>
        </button>
    </div>

    <Button
        variant="secondary"
        onclick={() => {
            showDeleteConfirm = false;
            loanToDelete = null;
        }}
        class="mt-8 w-full"
    >
        Cancel Action
    </Button>
</ConfirmModal>
