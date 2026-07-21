<script lang="ts">
    import { wsCall, decode } from "$lib/utils/ws_fetch";
    import {
        ModificationListSchema,
        AssetListSchema,
        LoanListSchema,
        ModificationSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { toInputMonth, fromInputMonth } from "$lib/utils/date";


    import { onMount } from "svelte";
    import {
        Plus,
        Trash2,
        Undo2,
        Archive,
        Pencil,
        AlertCircle,
        Settings2,
    } from "@lucide/svelte";
    import { fade } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";
    import { formatGermanAmount } from "$lib/utils/format";
    import Button from "$lib/components/ui/Button.svelte";
    import Input from "$lib/components/ui/Input.svelte";
    import CurrencyInput from "$lib/components/ui/CurrencyInput.svelte";
    import Badge from "$lib/components/ui/Badge.svelte";
    import Modal from "$lib/components/ui/Modal.svelte";
    import ConfirmModal from "$lib/components/ui/ConfirmModal.svelte";
    import Table from "$lib/components/ui/Table.svelte";

    interface ModificationVersion {
        id?: string;
        modificationId?: string;
        amount: number;
        withdrawalPercentage: number;
        startDate: string;
        endDate: string | null;
        intervalMonths: number;
        createdAt?: string;
    }

    interface Modification {
        id?: string;
        targetId: string;
        targetIds: string[];
        targetType: "ASSET" | "LOAN";
        description: string;
        activeVersion?: ModificationVersion;
    }



    function formatDate(dateStr: string | null | undefined): string {
        if (!dateStr) return "Ongoing";
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            year: "numeric",
            month: "2-digit",
        });
    }

    let mods = $state<Modification[]>([]);
    let sortedMods = $derived(
        [...mods].sort((a, b) => {
            const dateA = a.activeVersion?.startDate || "";
            const dateB = b.activeVersion?.startDate || "";
            if (dateA !== dateB) {
                return dateA.localeCompare(dateB);
            }
            return (a.description || "").localeCompare(b.description || "");
        })
    );
    let isLoading = $state(true);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

    // Context for mapping names to IDs during import
    let allAssets = $state<any[]>([]);
    let allLoans = $state<any[]>([]);

    const typeOptions = [
        { id: "ASSET", label: "Asset" },
        { id: "LOAN", label: "Loan" },
    ];

    const loanOptions = $derived([
        { id: "", label: "Select Target..." },
        ...(allLoans || []).map((l) => ({ id: l.id, label: l.name })),
    ]);

    const assetMultiOptions = $derived(
        (allAssets || []).map((a) => ({ id: a.id, label: a.name }))
    );

    // Modal State
    let showAddModal = $state(false);
    let showDeleteConfirm = $state(false);
    let currentMod = $state<Modification & { activeVersion: ModificationVersion }>(createNewMod() as any);
    let modToDelete = $state<string | null>(null);

    function createNewMod(): Modification & { activeVersion: ModificationVersion } {
        const now = new Date();
        const monthStr = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;

        return {
            targetId: "",
            targetIds: [],
            targetType: "ASSET",
            description: "",
            activeVersion: {
                amount: 0,
                withdrawalPercentage: 0,
                startDate: monthStr,
                endDate: null,
                intervalMonths: 0,
            },
        } as any;
    }

    async function fetchData() {
        isLoading = true;
        error = null;
        try {
            const [mR, aR, lR] = await Promise.all([
                wsCall("modifications::list", null, null, [
                    ModificationListSchema,
                ]).one(),
                wsCall("assets::list", null, null, [AssetListSchema]).one(),
                wsCall("loans::list", null, null, [LoanListSchema]).one(),
            ]);

            if (mR[1]) throw mR[1];
            if (aR[1]) throw aR[1];
            if (lR[1]) throw lR[1];

            mods = mR[0]?.modifications ?? [];
            allAssets = aR[0]?.assets ?? [];
            allLoans = lR[0]?.loans ?? [];
        } catch (err: any) {
            error = err.message;
        } finally {
            isLoading = false;
        }
    }

    async function saveMod() {
        if (!currentMod.description) return;
        if (currentMod.targetType === "LOAN" && !currentMod.targetId) return;
        if (
            currentMod.targetType === "ASSET" &&
            currentMod.targetIds.length === 0
        )
            return;

        isSaving = true;
        try {
            const [, err] = await wsCall(
                "modifications::save",
                ModificationSchema,
                {
                    id: currentMod.id || "",
                    targetId: currentMod.targetId || "",
                    targetIds: currentMod.targetIds || [],
                    targetType: currentMod.targetType || "ASSET",
                    description: currentMod.description,
                    activeVersion: {
                        id: currentMod.activeVersion.id || "",
                        modificationId:
                            currentMod.activeVersion.modificationId || "",
                        amount: currentMod.activeVersion.amount || 0,
                        withdrawalPercentage:
                            currentMod.activeVersion.withdrawalPercentage || 0,
                        startDate: currentMod.activeVersion.startDate || "",
                        endDate: currentMod.activeVersion.endDate || "",
                        intervalMonths:
                            parseInt(
                                currentMod.activeVersion.intervalMonths as any,
                            ) || 0,
                        createdAt: currentMod.activeVersion.createdAt || "",
                    },
                },
                [ModificationSchema, ErrorSchema],
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

    function editMod(mod: Modification) {
        currentMod = decode(mod);
        if (!currentMod.activeVersion) {
            currentMod.activeVersion = {
                amount: 0,
                withdrawalPercentage: 0,
                startDate: new Date().toISOString(),
                endDate: null,
                intervalMonths: 0,
            };
        } else {
            currentMod.activeVersion.amount = currentMod.activeVersion.amount ?? 0;
        }
        showAddModal = true;
    }

    function triggerDelete(id: string) {
        modToDelete = id;
        showDeleteConfirm = true;
    }

    async function deleteMod(mode?: string) {
        if (!modToDelete) return;
        try {
            const [, err] = await wsCall(
                "modifications::delete",
                GenericIDSchema,
                { id: modToDelete },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showDeleteConfirm = false;
            modToDelete = null;
        } catch (err: any) {
            alert(err.message);
        }
    }

    function getTargetName(mod: Modification): string {
        if (mod.targetType === "LOAN") {
            const loan = allLoans.find((l) => l.id === mod.targetId);
            return loan ? loan.name : "Unknown Liability";
        } else {
            if (!mod.targetIds || mod.targetIds.length === 0) return "Global";
            const names = mod.targetIds.map((id) => {
                const asset = allAssets.find((a) => a.id === id);
                return asset ? asset.name : "Unknown Asset";
            });
            return names.join(", ");
        }
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
                Adjustments
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Strategic rules, extra loan repayments, and targeted portfolio actions.
            </p>
        </div>
        <div>
            <Button
                onclick={() => {
                    currentMod = createNewMod();
                    showAddModal = true;
                }}
            >
                <Plus class="w-5 h-5" />
                Add Adjustment
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
                Syncing Strategic Rules...
            </p>
        </div>
    {:else if mods.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner">
                <Settings2 class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Strategic Rules Active
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Create adjustments for extra principal payments or scheduled asset additions.
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
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Description</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Target Type</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Target Asset / Loan</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400">Timeline</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Value</th>
                <th class="px-6 py-4 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 text-right">Actions</th>
            {/snippet}
            {#snippet body()}
                {#each sortedMods as mod (mod.id)}
                    <tr class="border-b border-slate-100 hover:bg-slate-50/30 transition-colors last:border-b-0">
                        <td class="px-6 py-4 font-bold text-slate-800">
                            {mod.description}
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            <Badge variant={mod.targetType === 'LOAN' ? 'error' : 'success'}>
                                {mod.targetType === 'LOAN' ? 'Extra Repayment' : 'Asset Addition'}
                            </Badge>
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700 max-w-[200px] truncate">
                            {getTargetName(mod)}
                        </td>
                        <td class="px-6 py-4 text-xs font-bold text-slate-700">
                            {formatDate(mod.activeVersion?.startDate)} → {formatDate(mod.activeVersion?.endDate)}
                        </td>
                        <td class="px-6 py-4">
                            <div class="flex items-center justify-between w-28 ml-auto tabular-nums font-black text-slate-900 dark:text-slate-100">
                                {#if mod.activeVersion && mod.activeVersion.withdrawalPercentage > 0}
                                    <span class="text-xs text-slate-400 italic">Dynamic</span>
                                    <span>{formatGermanAmount(mod.activeVersion.withdrawalPercentage)}%</span>
                                {:else}
                                    <span>€</span>
                                    <span>{formatGermanAmount(mod.activeVersion?.amount || 0)}</span>
                                {/if}
                            </div>
                        </td>
                        <td class="px-6 py-4 text-right">
                            <div class="inline-flex gap-2">
                                <Button
                                    variant="ghost"
                                    onclick={() => editMod(mod)}
                                    title="Edit (Create New Version)"
                                    class="hover:text-indigo-600 hover:bg-indigo-50 hover:border-indigo-100"
                                >
                                    <Pencil class="w-4 h-4" />
                                </Button>
                                <Button
                                    variant="ghost"
                                    onclick={() => triggerDelete(mod.id!)}
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
    title="{currentMod.id ? 'Refine' : 'New'} Strategic Rule"
    subtitle="Configure targeted additions, payments, or liquidation triggers."
>
    {#if currentMod.activeVersion}
        <form
            onsubmit={(e) => {
                e.preventDefault();
                saveMod();
            }}
            class="space-y-8"
        >
            <Input
                label="Rule Description"
                bind:value={currentMod.description}
                placeholder="e.g. Annual Vacation Fund, Principal Repayment..."
                required
            />

            <div class="grid grid-cols-2 gap-6">
                <div class="space-y-2">
                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                        Target Type
                    </label>
                    <select
                        bind:value={currentMod.targetType}
                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                    >
                        {#each typeOptions as opt}
                            <option value={opt.id}>{opt.label}</option>
                        {/each}
                    </select>
                </div>

                <div class="space-y-2">
                    {#if currentMod.targetType === "LOAN"}
                        <SearchableDropdown
                            label="Target Loan"
                            options={loanOptions}
                            bind:value={currentMod.targetId}
                            placeholder="Select Target Loan..."
                        />
                    {:else}
                        <SearchableMultiSelect
                            label="Target Asset(s)"
                            options={assetMultiOptions}
                            bind:values={currentMod.targetIds}
                            placeholder="Select Assets..."
                        />
                    {/if}
                </div>
            </div>

            <div class="grid grid-cols-2 gap-6">
                <CurrencyInput
                    label="Rate Amount (€)"
                    bind:value={currentMod.activeVersion.amount}
                    required={currentMod.activeVersion.withdrawalPercentage === 0}
                />
                <div class="space-y-2">
                    <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                        Frequency
                    </label>
                    <select
                        bind:value={currentMod.activeVersion.intervalMonths}
                        class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer dark:bg-slate-800 dark:border-slate-700"
                    >
                        <option value={0}>One-Time (Due in Start Month)</option>
                        <option value={1}>Monthly</option>
                        <option value={12}>Yearly</option>
                    </select>
                </div>
            </div>

            {#if currentMod.targetType === "ASSET" && currentMod.activeVersion.intervalMonths > 0}
                <div class="p-6 bg-slate-50 dark:bg-slate-800/40 rounded-2xl border border-slate-100 dark:border-slate-800 shadow-sm space-y-4">
                    <div class="space-y-2">
                        <label class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 block">
                            Dynamic Withdrawal Percentage (%)
                        </label>
                        <div class="relative">
                            <Input
                                type="number"
                                step="0.01"
                                bind:value={currentMod.activeVersion.withdrawalPercentage}
                                placeholder="e.g. 4.00"
                            />
                        </div>
                        <p class="text-[9px] font-medium text-slate-500 dark:text-slate-400 ml-1">
                            Use this for dynamic payouts (like safe withdrawal rates). Leave at 0% for fixed monetary amounts.
                        </p>
                    </div>
                </div>
            {/if}

            <div class="grid grid-cols-2 gap-6">
                <Input
                    type="month"
                    label="Start Month"
                    value={toInputMonth(currentMod.activeVersion.startDate)}
                    oninput={(e: any) => (currentMod.activeVersion.startDate = fromInputMonth(e.target.value))}
                    required
                />
                {#if currentMod.activeVersion.intervalMonths > 0}
                    <Input
                        type="month"
                        label="End Month (Optional)"
                        value={toInputMonth(currentMod.activeVersion.endDate)}
                        oninput={(e: any) => (currentMod.activeVersion.endDate = e.target.value ? fromInputMonth(e.target.value) : null)}
                    />
                {/if}
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
    {/if}
</Modal>

<!-- Deletion Confirmation Modal -->
<ConfirmModal
    bind:open={showDeleteConfirm}
    title="Archive Modification?"
    description="This will stop applying this adjustment to future projections. History is preserved."
>
    <div class="grid grid-cols-2 gap-4">
        <Button variant="secondary" onclick={() => (showDeleteConfirm = false)}>
            Keep
        </Button>
        <Button variant="danger" onclick={() => deleteMod("full")}>
            Archive
        </Button>
    </div>
    <button
        onclick={() => deleteMod("revert")}
        class="w-full mt-6 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 hover:text-indigo-600 transition-colors"
    >
        Revert Latest Version Only
    </button>
</ConfirmModal>
