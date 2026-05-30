<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        LoanListSchema,
        TransactionPoolListSchema,
        VirtualAccountListSchema,
        LoanSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    import { formatGermanAmount, parseGermanAmount } from "$lib/utils/format";
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
        ClipboardPaste,
        CheckCircle2,
        AlertCircle,
        Check,
        Globe,
        Languages,
        Coins,
        HandCoins,
        Link,
    } from "@lucide/svelte";

    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";
    import SearchableMultiSelect from "$lib/components/SearchableMultiSelect.svelte";

    interface LoanVersion {
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
    }

    interface Loan {
        id?: string;
        name: string;
        poolId?: string | null;
        accountIds?: string[];
        activeVersion?: LoanVersion;
        import_selected?: boolean;
    }

    let loans = $state<Loan[]>([]);
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
    let showImportModal = $state(false);
    let currentLoan = $state<Loan>(createNewLoan());
    let amountInput = $state("");
    let interestInput = $state("");
    let balloonInput = $state("");
    let penaltyInput = $state("");
    let loanToDelete = $state<string | null>(null);

    // Import State
    let rawImportData = $state("");
    let previewLoans = $state<Loan[]>([]);
    let importLocale = $state<"DE" | "US">("US");

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

            loans = lR[0].loans;
            pools = pR[0].pools;
            virtualAccounts = vaR[0].virtualAccounts;
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
            currentLoan.activeVersion.amountLent =
                parseGermanAmount(amountInput);
            currentLoan.activeVersion.interestRate =
                parseGermanAmount(interestInput);
            currentLoan.activeVersion.balloonLeftover =
                parseGermanAmount(balloonInput);
            currentLoan.activeVersion.earlyPayoffPenalty =
                parseGermanAmount(penaltyInput);

            const [, err] = await wsCall(
                "loans::save",
                LoanSchema,
                {
                    id: currentLoan.id || "",
                    name: currentLoan.name,
                    poolId: currentLoan.poolId || "",
                    accountIds: currentLoan.accountIds || [],
                    activeVersion: {
                        id: currentLoan.activeVersion.id || "",
                        loanId: currentLoan.activeVersion.loanId || "",
                        amountLent: currentLoan.activeVersion.amountLent || 0,
                        interestRate:
                            currentLoan.activeVersion.interestRate || 0,
                        runtimeMonths:
                            currentLoan.activeVersion.runtimeMonths || 0,
                        startDate: currentLoan.activeVersion.startDate || "",
                        remainderStartDate:
                            currentLoan.activeVersion.remainderStartDate || "",
                        priority: currentLoan.activeVersion.priority || 0,
                        nextLoanId: currentLoan.activeVersion.nextLoanId || "",
                        balloonLeftover:
                            currentLoan.activeVersion.balloonLeftover || 0,
                        isInterestOnly:
                            currentLoan.activeVersion.isInterestOnly || false,
                        earlyPayoffPenalty:
                            currentLoan.activeVersion.earlyPayoffPenalty || 0,
                        createdAt: currentLoan.activeVersion.createdAt || "",
                    },
                    linkToScenarios: currentLoan.linkToScenarios || [],
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
        }
        if (!currentLoan.accountIds) currentLoan.accountIds = [];

        amountInput = formatGermanAmount(currentLoan.activeVersion.amountLent);
        interestInput = formatGermanAmount(
            currentLoan.activeVersion.interestRate,
        );
        balloonInput = formatGermanAmount(
            currentLoan.activeVersion.balloonLeftover,
        );
        penaltyInput = formatGermanAmount(
            currentLoan.activeVersion.earlyPayoffPenalty,
        );

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

    async function executeImport() {
        const toImport = previewLoans.filter((l) => l.import_selected);
        if (toImport.length === 0) return;
        try {
            isSaving = true;
            const [, err] = await wsCall(
                "loans::save_bulk",
                LoanListSchema,
                {
                    loans: toImport.map((l) => ({
                        id: l.id || "",
                        name: l.name,
                        poolId: l.poolId || "",
                        accountIds: l.accountIds || [],
                        activeVersion: l.activeVersion
                            ? {
                                  id: l.activeVersion?.id || "",
                                  loanId: l.activeVersion?.loanId || "",
                                  amountLent: l.activeVersion?.amountLent || 0,
                                  interestRate:
                                      l.activeVersion?.interestRate || 0,
                                  runtimeMonths:
                                      l.activeVersion?.runtimeMonths || 0,
                                  startDate: l.activeVersion?.startDate || "",
                                  remainderStartDate:
                                      l.activeVersion?.remainderStartDate || "",
                                  priority: l.activeVersion?.priority || 0,
                                  nextLoanId: l.activeVersion?.nextLoanId || "",
                                  balloonLeftover:
                                      l.activeVersion?.balloonLeftover || 0,
                                  isInterestOnly:
                                      l.activeVersion?.isInterestOnly || false,
                                  earlyPayoffPenalty:
                                      l.activeVersion?.earlyPayoffPenalty || 0,
                                  createdAt: l.activeVersion?.createdAt || "",
                              }
                            : undefined,
                        linkToScenarios: l.linkToScenarios || [],
                    })),
                },
                [ErrorSchema],
            ).one();
            if (err) throw err;
            showImportModal = false;
            rawImportData = "";
            previewLoans = [];
            await fetchData();
        } catch (err: any) {
            alert(err.message);
        } finally {
            isSaving = false;
        }
    }

    function parseNumericAmount(val: string | number, locale: "DE" | "US"): number {
        if (typeof val === "number") return val; if (!val) return 0;
        if (locale === "DE") return parseGermanAmount(val);
        let clean = val.toString().trim().replace(/,/g, "");
        return parseFloat(clean) || 0;
    }

    function parseDateString(val: string): string {
        if (!val || val.trim() === "") {
            const now = new Date();
            return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-01T00:00:00Z`;
        }
        const parts = val.split("/");
        if (parts.length === 2) {
            const month = parts[0].padStart(2, "0");
            const year = parts[1];
            return `${year}-${month}-01T00:00:00Z`;
        }
        return new Date().toISOString();
    }

    $effect(() => {
        if (rawImportData.trim() === "") {
            previewLoans = [];
            return;
        }

        const lines = rawImportData.trim().split("\n");
        const detected: Loan[] = [];

        lines.forEach((line) => {
            const parts = line.split("\t");
            if (parts.length < 3) return;

            const name = parts[0].trim();
            const principal = parseNumericAmount(parts[1], importLocale);
            const interest = parseNumericAmount(parts[2], importLocale);
            const runtime = parseInt(parts[3]) || 12;
            const startDate = parseDateString(parts[4]);
            const balloon = parts[5]
                ? parseNumericAmount(parts[5], importLocale)
                : 0;

            detected.push({
                name,
                poolId: null,
                accountIds: [],
                import_selected: true,
                activeVersion: {
                    amountLent: principal,
                    interestRate: interest,
                    runtimeMonths: runtime,
                    startDate: startDate,
                    remainderStartDate: null,
                    priority: 0,
                    nextLoanId: null,
                    balloonLeftover: balloon,
                    isInterestOnly: false,
                    earlyPayoffPenalty: 1,
                },
            });
        });

        previewLoans = detected;
    });

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

<div class="space-y-8">
    <div
        class="flex flex-col md:flex-row md:items-center justify-between gap-6"
    >
        <div>
            <h2
                class="text-3xl font-black tracking-tight text-slate-900 text-transparent bg-clip-text bg-gradient-to-br from-slate-900 to-slate-500"
            >
                Loan Management
            </h2>
            <p class="text-slate-500 font-medium text-sm">
                Deterministic amortization models and liability chains.
            </p>
        </div>
        <div class="flex gap-4">
            <button
                onclick={() => (showImportModal = true)}
                class="btn-secondary"
                ><ClipboardPaste class="w-4 h-4" /> Bulk Import</button
            >
            <button
                onclick={() => {
                    currentLoan = createNewLoan();
                    amountInput = "";
                    interestInput = "";
                    balloonInput = "";
                    showAddModal = true;
                }}
                class="btn-primary bg-slate-900 shadow-slate-200"
                ><Plus class="w-5 h-5" /> Add Loan</button
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
            <Loader2 class="w-10 h-10 text-slate-900 animate-spin" />
            <p
                class="text-slate-400 font-black uppercase tracking-[0.2em] text-[10px]"
            >
                Calculating Amortization Node...
            </p>
        </div>
    {:else if (loans || []).length === 0}
        <div class="glass-card p-20 text-center space-y-6">
            <div
                class="inline-flex items-center justify-center p-6 bg-slate-50 rounded-3xl border border-slate-100 shadow-inner"
            >
                <HandCoins class="w-12 h-12 text-slate-300" />
            </div>
            <div class="space-y-2">
                <h3 class="text-xl font-black text-slate-900">
                    No Active Liabilities
                </h3>
                <p class="text-slate-500 max-w-xs mx-auto font-medium text-sm">
                    Add your loans to start tracking automated debt repayment.
                </p>
            </div>
            <button
                onclick={() => (showAddModal = true)}
                class="btn-secondary mx-auto">Initialize First Loan</button
            >
        </div>
    {:else}
        <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
            {#each loans || [] as loan (loan.id)}
                <div
                    transition:fade
                    class="glass-card p-8 group hover:border-slate-400/50 transition-all duration-300 relative overflow-hidden"
                >
                    <div
                        class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-slate-500/0 via-slate-500/20 to-slate-500/0 opacity-0 group-hover:opacity-100 transition-opacity"
                    ></div>
                    <div class="flex justify-between items-start mb-6">
                        <div class="space-y-1">
                            <h3
                                class="text-xl font-black tracking-tight text-slate-900"
                            >
                                {loan.name}
                            </h3>
                            <div class="flex items-center gap-2">
                                <span
                                    class="px-2 py-0.5 bg-slate-100 text-slate-600 rounded-md text-[9px] font-black uppercase tracking-[0.2em]"
                                >
                                    {loan.activeVersion?.balloonLeftover > 0
                                        ? "Balloon"
                                        : loan.activeVersion?.isInterestOnly
                                          ? "Interest-Only"
                                          : "Standard"}
                                </span>
                            </div>
                        </div>
                        <div class="flex gap-2">
                            <button
                                onclick={() => editLoan(loan)}
                                class="p-2.5 text-slate-400 hover:text-slate-900 hover:bg-slate-50 rounded-xl transition-all"
                                ><Pencil class="w-4 h-4" /></button
                            >
                            <button
                                onclick={() => {
                                    loanToDelete = loan.id!;
                                    showDeleteConfirm = true;
                                }}
                                class="p-2.5 text-slate-400 hover:text-red-600 hover:bg-red-50 rounded-xl transition-all"
                                ><Trash2 class="w-4 h-4" /></button
                            >
                        </div>
                    </div>
                    <div class="space-y-6">
                        <div class="grid grid-cols-2 gap-4">
                            <div>
                                <p
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 mb-1"
                                >
                                    Principal
                                </p>
                                <p class="text-2xl font-black text-slate-900">
                                    {formatGermanAmount(
                                        loan.activeVersion?.amountLent,
                                    )} €
                                </p>
                            </div>
                            <div>
                                <p
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 mb-1"
                                >
                                    Interest
                                </p>
                                <p class="text-2xl font-black text-slate-900">
                                    {formatGermanAmount(
                                        loan.activeVersion?.interestRate,
                                    )}%
                                </p>
                            </div>
                        </div>
                        {#if loan.activeVersion?.nextLoanId}
                            <div
                                class="flex items-center gap-2 px-3 py-2 bg-emerald-50 border border-emerald-100 rounded-xl"
                            >
                                <Link class="w-3 h-3 text-emerald-600" />
                                <span
                                    class="text-[9px] font-black uppercase text-emerald-600 tracking-[0.2em]"
                                    >Next: {loans.find(
                                        (l) =>
                                            l.id ===
                                            loan.activeVersion?.nextLoanId,
                                    )?.name || "Linked"}</span
                                >
                            </div>
                        {/if}
                        <div
                            class="flex items-center gap-6 pt-6 border-t border-slate-100"
                        >
                            <div class="space-y-1 flex-1">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400"
                                >
                                    Started
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {formatDate(loan.activeVersion?.startDate)}
                                </p>
                            </div>
                            <div class="space-y-1 flex-1 text-right">
                                <p
                                    class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400"
                                >
                                    Runtime
                                </p>
                                <p class="text-xs font-bold text-slate-700">
                                    {loan.activeVersion?.runtimeMonths} Months
                                </p>
                            </div>
                        </div>
                    </div>
                </div>
            {/each}
        </div>
    {/if}
</div>

<!-- Add Modal -->
{#if showAddModal}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-2xl bg-white rounded-[30px] shadow-2xl relative max-h-[90vh] overflow-y-auto"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <button
                onclick={() => (showAddModal = false)}
                class="absolute top-6 right-6 text-slate-400 hover:text-slate-900 transition-colors"
                ><Plus class="w-6 h-6 rotate-45" /></button
            >
            <div class="p-10 space-y-10">
                <div>
                    <h3
                        class="text-2xl font-black text-slate-900 tracking-tight"
                    >
                        {currentLoan.id ? "Refine" : "New"} Liability Node
                    </h3>
                    <p class="text-slate-500 font-medium text-sm">
                        Define deterministic amortization parameters.
                    </p>
                </div>
                <form
                    onsubmit={(e) => {
                        e.preventDefault();
                        saveLoan();
                    }}
                    class="space-y-8"
                >
                    <div class="space-y-6">
                        <div class="grid grid-cols-2 gap-6">
                            <div class="space-y-2">
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                    >Loan Identity</label
                                >
                                <input
                                    bind:value={currentLoan.name}
                                    placeholder="e.g. SWK Loan"
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold"
                                    required
                                />
                            </div>
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
                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Principal (€)</label
                            ><input
                                type="text"
                                bind:value={amountInput}
                                placeholder="50.000,00"
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                required
                            />
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Interest %</label
                            ><input
                                type="text"
                                bind:value={interestInput}
                                placeholder="5,83"
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                required
                            />
                        </div>
                    </div>
                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Runtime (Months)</label
                            ><input
                                type="number"
                                bind:value={
                                    currentLoan.activeVersion.runtimeMonths
                                }
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                required
                            />
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Start Month</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentLoan.activeVersion.startDate,
                                )}
                                oninput={(e: any) =>
                                    (currentLoan.activeVersion.startDate =
                                        fromInputMonth(e.target.value))}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                                required
                            />
                        </div>
                    </div>
                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Balloon Leftover (€)</label
                            ><input
                                type="text"
                                bind:value={balloonInput}
                                placeholder="0,00"
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                            />
                        </div>
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Remainder Start</label
                            >
                            <input
                                type="month"
                                value={toInputMonth(
                                    currentLoan.activeVersion
                                        .remainderStartDate,
                                )}
                                oninput={(e: any) =>
                                    (currentLoan.activeVersion.remainderStartDate =
                                        e.target.value
                                            ? fromInputMonth(e.target.value)
                                            : null)}
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                            />
                        </div>
                    </div>
                    <div class="grid grid-cols-2 gap-6">
                        <div class="space-y-2">
                            <label
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                >Early Payoff Penalty %</label
                            ><input
                                type="text"
                                bind:value={penaltyInput}
                                placeholder="1,00"
                                class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all"
                            />
                        </div>
                    </div>
                    <div class="flex items-center gap-3 ml-1">
                        <input
                            type="checkbox"
                            bind:checked={
                                currentLoan.activeVersion.isInterestOnly
                            }
                            class="w-5 h-5 rounded border-slate-300 text-indigo-600 focus:ring-indigo-500"
                        />
                        <span
                            class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400"
                            >Interest Only</span
                        >
                    </div>

                    <!-- Loan Replacement Section -->
                    <div
                        class="space-y-4 p-6 bg-white rounded-2xl border border-slate-100 shadow-sm"
                    >
                        <div class="flex items-center justify-between">
                            <div class="space-y-0.5">
                                <label class="text-sm font-black text-slate-900"
                                    >Enable Loan Replacement</label
                                >
                                <p
                                    class="text-[10px] font-medium text-slate-500"
                                >
                                    Link to another loan configuration for
                                    rollover.
                                </p>
                            </div>
                            <button
                                type="button"
                                onclick={() => {
                                    if (currentLoan.activeVersion.nextLoanId) {
                                        currentLoan.activeVersion.nextLoanId =
                                            null;
                                    } else {
                                        const firstOther = loans.find(
                                            (l) => l.id !== currentLoan.id,
                                        );
                                        if (firstOther) {
                                            currentLoan.activeVersion.nextLoanId =
                                                firstOther.id!;

                                            // Trigger date sync
                                            const startDate = new Date(
                                                currentLoan.activeVersion
                                                    .startDate,
                                            );
                                            const endDate = new Date(
                                                startDate.setMonth(
                                                    startDate.getMonth() +
                                                        currentLoan
                                                            .activeVersion
                                                            .runtimeMonths,
                                                ),
                                            );
                                            const endDateStr =
                                                endDate
                                                    .toISOString()
                                                    .substring(0, 7) +
                                                "-01T00:00:00Z";
                                            firstOther.activeVersion.startDate =
                                                endDateStr;
                                        }
                                    }
                                }}
                                class="w-12 h-6 rounded-full transition-all relative {currentLoan
                                    .activeVersion.nextLoanId
                                    ? 'bg-indigo-600 shadow-lg shadow-indigo-100'
                                    : 'bg-slate-200'}"
                            >
                                <div
                                    class="absolute top-1 left-1 w-4 h-4 bg-white rounded-full transition-all {currentLoan
                                        .activeVersion.nextLoanId
                                        ? 'translate-x-6'
                                        : ''}"
                                ></div>
                            </button>
                        </div>

                        {#if currentLoan.activeVersion.nextLoanId}
                            <div class="space-y-2" transition:slide>
                                <label
                                    class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400 ml-1 mb-1"
                                    >Next Loan in Chain</label
                                >
                                <select
                                    bind:value={
                                        currentLoan.activeVersion.nextLoanId
                                    }
                                    onchange={() => {
                                        const nextLoan = loans.find(
                                            (l) =>
                                                l.id ===
                                                currentLoan.activeVersion
                                                    .nextLoanId,
                                        );
                                        if (nextLoan) {
                                            const startDate = new Date(
                                                currentLoan.activeVersion
                                                    .startDate,
                                            );
                                            const endDate = new Date(
                                                startDate.setMonth(
                                                    startDate.getMonth() +
                                                        currentLoan
                                                            .activeVersion
                                                            .runtimeMonths,
                                                ),
                                            );
                                            const endDateStr =
                                                endDate
                                                    .toISOString()
                                                    .substring(0, 7) +
                                                "-01T00:00:00Z";

                                            // Update in local state for immediate feedback
                                            nextLoan.activeVersion.startDate =
                                                endDateStr;
                                        }
                                    }}
                                    class="block w-full px-4 py-3 bg-white border border-slate-200 rounded-xl focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 outline-none transition-all font-bold appearance-none cursor-pointer"
                                >
                                    {#each loans.filter((l) => l.id !== currentLoan.id) as loan}
                                        <option value={loan.id}
                                            >{loan.name}</option
                                        >
                                    {/each}
                                </select>
                            </div>
                        {/if}
                    </div>

                    <div class="pt-6">
                        <button
                            disabled={isSaving}
                            class="btn-primary w-full py-4 text-lg shadow-2xl bg-indigo-600 hover:bg-indigo-700 text-white shadow-indigo-100"
                            >Commit Liability Node</button
                        >
                    </div>
                </form>
            </div>
        </div>
    </div>
{/if}

<!-- Import Modal (Standard) -->
{#if showImportModal}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-w-5xl bg-white rounded-[30px] shadow-2xl relative max-h-[90vh] flex flex-col overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <button
                onclick={() => (showImportModal = false)}
                class="absolute top-6 right-6 text-slate-400 hover:text-slate-900 transition-colors"
                ><Plus class="w-6 h-6 rotate-45" /></button
            >
            <div class="p-10 flex flex-col overflow-hidden h-full">
                <div class="mb-8">
                    <h3
                        class="text-3xl font-black text-slate-900 tracking-tight"
                    >
                        Loan Verification Engine
                    </h3>
                    <p class="text-slate-500 font-medium">
                        Review and verify external liability nodes.
                    </p>
                </div>
                <div
                    class="grid grid-cols-1 lg:grid-cols-12 gap-10 flex-1 overflow-hidden"
                >
                    <div class="lg:col-span-4 flex flex-col space-y-6">
                        <textarea
                            bind:value={rawImportData}
                            placeholder="Paste columns..."
                            class="flex-1 w-full p-4 bg-white border border-slate-200 rounded-2xl outline-none font-mono text-[10px] resize-none shadow-sm focus:ring-4 focus:ring-indigo-500/10 focus:border-indigo-500 transition-all"
                        ></textarea>
                        <div
                            class="p-5 bg-white rounded-2xl border border-slate-100 shadow-sm space-y-4"
                        >
                            <p
                                class="text-[10px] font-black uppercase tracking-[0.2em] text-slate-400"
                            >
                                Numeric Parsing Strategy
                            </p>
                            <div
                                class="flex p-1 bg-white border border-slate-200 rounded-xl"
                            >
                                <button
                                    onclick={() => (importLocale = "US")}
                                    class="flex-1 py-2 px-3 rounded-lg text-[11px] font-black transition-all {importLocale ===
                                    'US'
                                        ? 'bg-indigo-600 text-white shadow-lg'
                                        : 'text-slate-400'}"
                                    >International</button
                                ><button
                                    onclick={() => (importLocale = "DE")}
                                    class="flex-1 py-2 px-3 rounded-lg text-[11px] font-black transition-all {importLocale ===
                                    'DE'
                                        ? 'bg-indigo-600 text-white shadow-lg'
                                        : 'text-slate-400'}">German</button
                                >
                            </div>
                        </div>
                    </div>
                    <div class="lg:col-span-8 flex flex-col overflow-hidden">
                        <div
                            class="flex-1 overflow-y-auto border border-slate-100 rounded-2xl bg-white shadow-sm"
                        >
                            <table
                                class="w-full text-left border-collapse text-xs"
                            >
                                <thead class="sticky top-0 bg-white shadow-sm"
                                    ><tr
                                        ><th
                                            class="p-4 text-[10px] font-black uppercase text-slate-400"
                                        ></th><th
                                            class="p-4 text-[10px] font-black uppercase text-slate-400"
                                            >Loan</th
                                        ><th
                                            class="p-4 text-[10px] font-black uppercase text-slate-400"
                                            >Parameters</th
                                        ></tr
                                    ></thead
                                >
                                <tbody
                                    >{#each previewLoans || [] as l}<tr
                                            class="group hover:bg-slate-50 transition-colors border-b border-slate-50 last:border-0"
                                            ><td class="p-4 text-center"
                                                ><button
                                                    onclick={() =>
                                                        (l.import_selected =
                                                            !l.import_selected)}
                                                    class="w-5 h-5 rounded border-2 transition-all {l.import_selected
                                                        ? 'bg-indigo-600 border-indigo-600 text-white'
                                                        : 'border-slate-200 bg-white'}"
                                                    ><Check
                                                        class="w-4 h-4"
                                                    /></button
                                                ></td
                                            ><td
                                                class="p-4 font-black text-slate-900"
                                                >{l.name}</td
                                            ><td class="p-4"
                                                ><p
                                                    class="font-black text-slate-900"
                                                >
                                                    € {formatGermanAmount(
                                                        l.activeVersion
                                                            .amountLent,
                                                    )} @ {l.activeVersion
                                                        .interestRate}%
                                                </p>
                                                <p
                                                    class="text-[9px] text-slate-400 uppercase font-bold tracking-wider"
                                                >
                                                    {l.activeVersion
                                                        .runtimeMonths}m
                                                </p></td
                                            ></tr
                                        >{/each}</tbody
                                >
                            </table>
                        </div>
                    </div>
                </div>
                <div
                    class="mt-10 pt-8 border-t border-slate-100 flex gap-4 justify-end"
                >
                    <button
                        onclick={() => (showImportModal = false)}
                        class="btn-secondary px-8">Abort</button
                    ><button
                        onclick={executeImport}
                        disabled={!(previewLoans || []).some(
                            (l) => l.import_selected,
                        ) || isSaving}
                        class="btn-primary px-12 py-4 text-lg bg-indigo-600 text-white shadow-2xl shadow-indigo-100"
                        >Execute Batch Import</button
                    >
                </div>
            </div>
        </div>
    </div>
{/if}

<!-- Delete Modal (Standard) -->
{#if showDeleteConfirm}
    <div
        transition:fade={{ duration: 200 }}
        class="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-slate-900/40 backdrop-blur-sm"
    >
        <div
            transition:slide
            class="w-full max-md bg-white rounded-[30px] shadow-2xl p-10 relative overflow-hidden"
        >
            <div
                class="absolute top-0 left-0 right-0 h-1 bg-gradient-to-r from-indigo-500 via-purple-500 to-pink-500"
            ></div>

            <div class="text-center space-y-2 mb-8">
                <h3 class="text-2xl font-black text-slate-900">
                    Liability Lifecycle
                </h3>
                <p class="text-slate-500 font-medium text-sm">
                    Deterministic removal action.
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
                            Revert Version
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Delete only the latest version record.
                        </p>
                    </div></button
                ><button
                    onclick={() => confirmDelete("full")}
                    class="flex items-center gap-4 p-5 rounded-2xl border-2 border-slate-50 hover:border-rose-100 hover:bg-rose-50 transition-all text-left group"
                >
                    <div
                        class="p-3 bg-rose-100 rounded-xl group-hover:scale-110 transition-transform"
                    >
                        <Archive class="w-6 h-6 text-rose-600" />
                    </div>
                    <div>
                        <p class="font-black text-rose-600 leading-tight">
                            Node Archive
                        </p>
                        <p class="text-xs text-slate-500 font-medium">
                            Hide this liability and all versions.
                        </p>
                    </div></button
                >
            </div>
            <button
                onclick={() => {
                    showDeleteConfirm = false;
                    loanToDelete = null;
                }}
                class="w-full mt-8 py-3 text-slate-400 font-black uppercase tracking-[0.2em] text-[10px] hover:text-slate-900 transition-colors"
                >Cancel Action</button
            >
        </div>
    </div>
{/if}
