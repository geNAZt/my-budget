<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        ModificationListSchema,
        AssetListSchema,
        LoanListSchema,
        ModificationSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    const decode = (obj: any) => JSON.parse(JSON.stringify(obj));

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
        Settings2,
        Layers,
        Hammer,
        Target,
        Percent,
        Activity,
        X,
        AlertCircle,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";

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

    function toInputMonth(isoStr: string | null | undefined): string {
        if (!isoStr) return "";
        return isoStr.substring(0, 7); // "YYYY-MM"
    }

    function fromInputMonth(val: string): string {
        if (!val) return "";
        return val + "-01T00:00:00Z";
    }

    function formatDate(dateStr: string | null | undefined): string {
        if (!dateStr) return "Ongoing";
        const d = new Date(dateStr);
        return d.toLocaleDateString("de-DE", {
            year: "numeric",
            month: "2-digit",
        });
    }

    function parseNumeric(val: string | number, locale: "DE" | "US"): number {
        if (typeof val === "number") return val;
        if (!val) return 0;
        if (locale === "DE") return parseGermanAmount(val);
        let clean = val.toString().trim().replace(/,/g, "");
        return parseFloat(clean) || 0;
    }

    let mods = $state<Modification[]>([]);
    let isLoading = $state(true);
    let isSaving = $state(false);
    let error = $state<string | null>(null);

    // Context for mapping names to IDs during import
    let allAssets = $state<any[]>([]);
    let allLoans = $state<any[]>([]);

    const typeOptions = [
        { id: "ASSET", label: "Asset Node" },
        { id: "LOAN", label: "Liability Node" },
    ];

    const intervalOptions = [
        { id: "0", label: "One-Time" },
        { id: "1", label: "Monthly" },
        { id: "12", label: "Yearly" },
    ];

    const loanOptions = $derived([
        { id: "", label: "Select Target..." },
        ...(allLoans || []).map((l) => ({ id: l.id, label: l.name })),
    ]);

    // Modal State
    let showAddModal = $state(false);
    let showDeleteConfirm = $state(false);
    let currentMod = $state<Modification & { activeVersion: ModificationVersion }>(createNewMod() as any);
    let amountInput = $state("");
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
                intervalMonths: "0" as any,
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

        currentMod.activeVersion.amount = parseNumeric(amountInput, "DE");

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
                            Number(currentMod.activeVersion.intervalMonths) || 0,
                        createdAt: currentMod.activeVersion.createdAt || "",
                    },
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            await fetchData();
            showAddModal = false;
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
                intervalMonths: "0" as any,
            };
        } else {
            currentMod.activeVersion.intervalMonths = String(currentMod.activeVersion.intervalMonths) as any;
        }
        if (!currentMod.targetIds) currentMod.targetIds = [];
        amountInput = formatGermanAmount(currentMod.activeVersion.amount);
        showAddModal = true;
    }

    function toggleAsset(id: string) {
        if (currentMod.targetIds.includes(id)) {
            currentMod.targetIds = currentMod.targetIds.filter(
                (tid) => tid !== id,
            );
        } else {
            currentMod.targetIds = [...currentMod.targetIds, id];
        }
    }

    async function deleteMod(mode: "revert" | "full") {
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

    function getTargetNames(m: Modification) {
        if (m.targetType === "LOAN") {
            return (
                allLoans.find((l) => l.id === m.targetId)?.name ||
                "Unknown Loan"
            );
        }
        if (m.targetIds && m.targetIds.length > 0) {
            if (m.targetIds.length === 1) {
                return (
                    allAssets.find((a) => a.id === m.targetIds[0])?.name ||
                    "Unknown Asset"
                );
            }
            return `${m.targetIds.length} Assets`;
        }
        return (
            allAssets.find((a) => a.id === m.targetId)?.name || "Unknown Asset"
        );
    }

    onMount(fetchData);
</script>

<div class="space-y-8">
    <div
        class="flex flex-col md:flex-row md:items-center justify-between gap-6"
    >
        <div>
            <h2
                class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500"
            >
                Balance Modifications
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Targeted adjustments for deterministic asset and loan growth.
            </p>
        </div>
        <div class="flex gap-4">
            <button
                onclick={() => {
                    currentMod = createNewMod();
                    amountInput = "";
                    showAddModal = true;
                }}
                class="btn-primary"
                ><Plus class="w-5 h-5" /> Add Modification</button
            >
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
            <Loader2 class="w-10 h-10 text-indigo-600 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]"
            >
                Syncing Adjustments...
            </p>
        </div>
    {:else if mods.length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div
                class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner"
            >
                <Hammer class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Modifications Active
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Add adjustments for specific loans or assets to model
                    real-world transactions.
                </p>
            </div>
            <button
                onclick={() => (showAddModal = true)}
                class="btn-secondary mx-auto">Initialize First Mod</button
            >
        </div>
    {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {#each mods as m (m.id)}
                <div
                    transition:fade
                    class="glass-card p-8 group hover:border-indigo-200/50 transition-all duration-300 relative overflow-hidden"
                >
                    <div
                        class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500/0 via-indigo-500/20 to-indigo-500/0 opacity-0 group-hover:opacity-100 transition-opacity"
                    ></div>
                    <div class="flex justify-between items-start mb-6">
                        <div class="space-y-1">
                            <h3
                                class="text-xl font-black tracking-tight text-slate-900"
                            >
                                {m.description}
                            </h3>
                            <div class="flex items-center gap-2">
                                <span
                                    class="px-2 py-0.5 bg-slate-100 text-slate-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]"
                                >
                                    {getTargetNames(m)}
                                </span>
                            </div>
                        </div>
                        <div class="flex gap-2">
                            <button
                                onclick={() => editMod(m)}
                                class="p-2.5 text-slate-400 hover:text-indigo-600 hover:bg-indigo-50 rounded-xl transition-all border border-transparent hover:border-indigo-100"
                                title="Refine (New Version)"
                            >
                                <Pencil class="w-4 h-4" />
                            </button>
                            <button
                                onclick={() => {
                                    modToDelete = m.id!;
                                    showDeleteConfirm = true;
                                }}
                                class="p-2.5 text-slate-400 hover:text-rose-600 hover:bg-rose-50 rounded-xl transition-all border border-transparent hover:border-red-100"
                                title="Archive Modification"
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
                                Adjustment
                            </p>
                            <p
                                class="text-3xl font-black {(m.activeVersion?.amount ?? 0) >= 0
                                    ? 'text-emerald-600'
                                    : 'text-rose-600'}"
                            >
                                {(m.activeVersion?.amount ?? 0) >= 0
                                    ? "+"
                                    : ""}{formatGermanAmount(
                                    m.activeVersion?.amount ?? 0,
                                )} €
                            </p>
                        </div>
                        <div
                            class="flex items-center gap-6 pt-6 border-t border-slate-100"
                        >
                            <div class="space-y-1 flex-1">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400"
                                >
                                    Node Interval
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {m.activeVersion?.intervalMonths === 0
                                        ? "One-Time"
                                        : m.activeVersion?.intervalMonths + "m"}
                                </p>
                            </div>
                            <div class="space-y-1 flex-1 text-right">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400"
                                >
                                    Date
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {formatDate(m.activeVersion?.startDate)}
                                </p>
                            </div>
                        </div>
                    </div>
                </div>
            {/each}
        </div>
    {/if}
</div>

<!-- Delete Confirmation Modal -->
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

            <div class="text-center mb-8">
                <div
                    class="inline-flex items-center justify-center p-4 bg-rose-50 text-rose-600 rounded-2xl mb-6"
                >
                    <Trash2 class="w-8 h-8" />
                </div>
                <h3 class="text-2xl font-black text-slate-900 mb-2">
                    Archive Modification?
                </h3>
                <p class="text-slate-500 font-medium text-sm">
                    This will stop applying this adjustment to future
                    projections. History is preserved.
                </p>
            </div>

            <div class="grid grid-cols-2 gap-4">
                <button
                    onclick={() => (showDeleteConfirm = false)}
                    class="btn-secondary">Keep</button
                >
                <button
                    onclick={() => deleteMod("full")}
                    class="btn-primary bg-rose-600 hover:bg-rose-700 border-rose-600 shadow-rose-100"
                    >Archive</button
                >
            </div>
            <button
                onclick={() => deleteMod("revert")}
                class="w-full mt-6 text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 hover:text-indigo-600 transition-colors"
                >Revert Latest Version Only</button
            >
        </div>
    </div>
{/if}

<!-- Add/Edit Modal -->
{#if showAddModal}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-lg bg-white rounded-[30px] shadow-2xl relative max-h-[90vh] flex flex-col overflow-hidden"
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

            <div class="p-10 space-y-10 overflow-y-auto">
                <div>
                    <h3
                        class="text-2xl font-black text-slate-900 tracking-tight"
                    >
                        {currentMod.id ? "Refine" : "New"} Modification
                    </h3>
                    <p class="text-slate-500 font-medium text-sm">
                        {currentMod.id
                            ? "Changes will be saved as a new immutable version."
                            : "Target specific nodes for balance adjustments."}
                    </p>
                </div>

                <form
                    onsubmit={(e) => {
                        e.preventDefault();
                        saveMod();
                    }}
                    class="space-y-8"
                >
                    <div class="grid grid-cols-2 gap-6">
                        <SearchableDropdown
                            label="Target Type"
                            options={typeOptions}
                            bind:value={currentMod.targetType}
                        />

                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Target Node</label
                            >
                            {#if currentMod.targetType === "LOAN"}
                                <SearchableDropdown
                                    options={loanOptions}
                                    bind:value={currentMod.targetId}
                                    placeholder="Select Liability..."
                                />
                            {:else}
                                <div
                                    class="space-y-2 max-h-40 overflow-y-auto p-3 bg-white border border-slate-200 rounded-xl shadow-inner custom-scrollbar"
                                >
                                    {#each allAssets as a}
                                        <label
                                            class="flex items-center gap-3 cursor-pointer p-1.5 hover:bg-slate-50 rounded-lg transition-colors"
                                        >
                                            <input
                                                type="checkbox"
                                                checked={currentMod.targetIds.includes(
                                                    a.id,
                                                )}
                                                onchange={() =>
                                                    toggleAsset(a.id)}
                                                class="w-4 h-4 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                                            />
                                            <span
                                                class="text-[11px] font-bold text-slate-700"
                                                >{a.name}</span
                                            >
                                        </label>
                                    {/each}
                                </div>
                            {/if}
                        </div>
                    </div>

                    <div class="space-y-2">
                        <label
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                            >Description</label
                        >
                        <input
                            bind:value={currentMod.description}
                            placeholder="e.g. Sondertilgung"
                            class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                            required
                        />
                    </div>

                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                            >
                                {currentMod.activeVersion.withdrawalPercentage >
                                0
                                    ? "Min. Threshold (€)"
                                    : "Adjustment (€)"}
                            </label>
                            <input
                                type="text"
                                bind:value={amountInput}
                                placeholder="+5.000,00"
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                required
                            />
                        </div>

                        <SearchableDropdown
                            label="Interval"
                            options={intervalOptions}
                            bind:value={
                                currentMod.activeVersion.intervalMonths as any
                            }
                        />
                    </div>

                    {#if currentMod.targetType === "ASSET"}
                        <div
                            class="space-y-4 p-6 bg-white rounded-2xl border border-slate-100 shadow-sm"
                            transition:slide
                        >
                            <div class="space-y-2">
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1 flex items-center gap-2"
                                >
                                    <Percent class="w-3 h-3" /> Dynamic Withdrawal
                                    (%)
                                </label>
                                <input
                                    type="number"
                                    step="0.1"
                                    bind:value={
                                        currentMod.activeVersion
                                            .withdrawalPercentage
                                    }
                                    placeholder="3,5"
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                />
                                <p
                                    class="text-[9px] font-medium text-slate-500 ml-1"
                                >
                                    If set, it will withdraw this % annually
                                    (split monthly) if the balance is >=
                                    Threshold.
                                </p>
                            </div>
                        </div>
                    {/if}

                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Start Month</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentMod.activeVersion.startDate,
                                )}
                                oninput={(e: any) =>
                                    (currentMod.activeVersion.startDate =
                                        fromInputMonth(e.target.value))}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                required
                            />
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >End Month (Optional)</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentMod.activeVersion.endDate,
                                )}
                                oninput={(e: any) =>
                                    (currentMod.activeVersion.endDate = e.target
                                        .value
                                        ? fromInputMonth(e.target.value)
                                        : null)}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                            />
                        </div>
                    </div>

                    <div class="pt-6">
                        <button
                            disabled={isSaving}
                            class="btn-primary w-full py-4 text-lg shadow-2xl shadow-indigo-100 bg-indigo-600 hover:bg-indigo-700"
                        >
                            {#if isSaving}
                                <Loader2 class="w-6 h-6 animate-spin" />
                                <span>Versioning Adjustments...</span>
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

<style>
    @reference "../../app.css";
    .custom-scrollbar::-webkit-scrollbar {
        width: 4px;
    }
    .custom-scrollbar::-webkit-scrollbar-track {
        background: #f1f5f9;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb {
        background: #e2e8f0;
        border-radius: 10px;
    }
    .custom-scrollbar::-webkit-scrollbar-thumb:hover {
        background: #cbd5e1;
    }

    .btn-primary {
        @apply px-6 py-3 bg-slate-900 text-white rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] hover:bg-indigo-600 transition-all active:scale-95 disabled:opacity-50 flex items-center justify-center gap-2;
    }

    .btn-secondary {
        @apply px-6 py-3 bg-slate-50 text-slate-500 rounded-2xl font-black text-[10px] uppercase tracking-[0.2em] hover:bg-slate-100 transition-all active:scale-95 border border-slate-100 flex items-center justify-center gap-2;
    }

    .glass-card {
        @apply bg-white border border-slate-100 rounded-[32px] shadow-sm;
    }
</style>
