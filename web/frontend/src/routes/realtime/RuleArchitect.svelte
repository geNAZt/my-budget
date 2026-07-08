<script lang="ts">
    import { wsCall } from "$lib/utils/ws_fetch";
    import {
        TransactionPoolSchema,
        TransactionRuleSchema,
        GenericIDSchema,
        ErrorSchema,
    } from "$lib/gen/api_pb.js";
    const decode = (obj: any) => JSON.parse(JSON.stringify(obj));

    import {
        Plus,
        Trash2,
        X,
        Filter,
        Zap,
        LayoutGrid,
        ArrowRight,
        Search,
        Clock,
        Check,
        Pencil,
        Percent,
    } from "@lucide/svelte";
    import { fade, slide } from "svelte/transition";
    import SearchableDropdown from "$lib/components/SearchableDropdown.svelte";

    let {
        pools = $bindable([]),
        rules = $bindable([]),
        transactions = [],
        mappedAccounts = {},
        onChange,
    } = $props();

    function buildPoolTree(flatPools: any[]): any[] {
        const treeMap: Record<string, any> = {};
        const roots: any[] = [];

        flatPools.forEach((p) => {
            treeMap[p.id] = { ...p, children: [] };
        });

        flatPools.forEach((p) => {
            if (p.parentId && treeMap[p.parentId]) {
                treeMap[p.parentId].children.push(treeMap[p.id]);
            } else {
                roots.push(treeMap[p.id]);
            }
        });

        return roots;
    }

    function flattenPoolTree(nodes: any[], depth = 0): any[] {
        const result: any[] = [];
        nodes.forEach((n) => {
            result.push({ ...n, depth });
            if (n.children && n.children.length > 0) {
                result.push(...flattenPoolTree(n.children, depth + 1));
            }
        });
        return result;
    }

    const poolTree = $derived(buildPoolTree(pools || []));

    const poolOptions = $derived([
        { id: "", label: "Select Pool..." },
        ...flattenPoolTree(poolTree).map((p) => ({
            id: p.id,
            label: "\u00A0\u00A0".repeat(p.depth) + p.name,
        })),
    ]);

    let newPoolName = $state("");
    let newPoolColor = $state("#6366f1");
    let newPoolParentID = $state<string | null>(null);
    let isSaving = $state(false);

    let editingPool = $state<{
        id: string;
        name: string;
        parentId: string | null;
        color: string;
    } | null>(null);

    function startEditingPool(pool: any) {
        editingPool = {
            id: pool.id,
            name: pool.name,
            parentId: pool.parentId,
            color: pool.color,
        };
    }

    async function savePoolName() {
        if (!editingPool || !editingPool.name.trim()) {
            editingPool = null;
            return;
        }

        const newName = editingPool.name.trim();
        const id = editingPool.id;

        try {
            const pool = (pools || []).find((p) => p.id === id);
            if (
                pool &&
                (pool.name !== newName ||
                    pool.parentId !== editingPool.parentId)
            ) {
                const updatedPool = {
                    ...pool,
                    name: newName,
                    parentId: editingPool.parentId,
                };
                try {
                    const [, err] = await wsCall(
                        "pools::save",
                        TransactionPoolSchema,
                        {
                            id: pool.id,
                            parentId: editingPool.parentId || "",
                            name: newName,
                            color: pool.color,
                            isHidden: pool.isHidden,
                        },
                        [TransactionPoolSchema],
                    ).one();
                    if (err) throw err;
                    pool.name = newName;
                    pool.parentId = editingPool.parentId;
                    pools = [...(pools || [])];
                    if (onChange) onChange();
                } catch (e) {}
            }
        } catch (e) {
            console.error(e);
        }

        editingPool = null;
    }

    // Advanced Rule Creator State
    let newRulePoolID = $state("");
    let rulePriority = $state(0);
    let editingRuleID = $state<string | null>(null);
    let activeBuilderRule = $state<any>({
        operator: "NONE", // 'NONE', 'AND', 'OR'
        field: "RECEIVER", // 'RECEIVER', 'DESCRIPTION', 'TAGS', 'ACCOUNT_TAGS', 'AMOUNT', 'DATA_CHAIN'
        regex: "",
        amountOperator: ">",
        amountValue: 0,
        negate: false,
        children: [],
    });

    function getTxDescription(tx: any) {
        if (tx.description) return tx.description;
        if (!tx.data) return "External Transaction";

        // GoCardless / Generic camelCase
        if (tx.data.remittanceInformationUnstructured)
            return tx.data.remittanceInformationUnstructured;

        // EnableBanking / snake_case
        if (
            tx.data.remittance_information &&
            Array.isArray(tx.data.remittance_information) &&
            tx.data.remittance_information.length > 0
        ) {
            return tx.data.remittance_information.join(" ");
        }

        // Trading212 / Reference
        if (tx.data.reference)
            return `${tx.data.type || ""} ${tx.data.reference}`;

        return tx.data.type || "External Transaction";
    }

    function getTxPeer(tx: any) {
        if (tx.receiver) return tx.receiver;
        if (tx.peer) return tx.peer;
        if (!tx.data) return "External Peer";

        // GoCardless
        if (tx.data.debtorName || tx.data.creditorName)
            return tx.data.debtorName || tx.data.creditorName;

        // EnableBanking
        if (tx.data.creditor?.name || tx.data.debtor?.name)
            return tx.data.creditor?.name || tx.data.debtor?.name;

        // Trading212
        if (tx.data.reference) return tx.data.reference;

        return "External Peer";
    }

    function getTxPeerIban(tx: any) {
        if (tx.receiverIban) return tx.receiverIban;
        if (!tx.data) return "";
        return (
            tx.data.creditorAccount?.iban ||
            tx.data.creditor_account?.iban ||
            tx.data.debtorAccount?.iban ||
            tx.data.debtor_account?.iban ||
            tx.data.creditor?.account?.iban ||
            tx.data.debtor?.account?.iban ||
            ""
        );
    }

    function getTxAmount(tx: any) {
        if (tx.amount !== undefined) return tx.amount;
        if (!tx.data) return 0;
        let amt = 0;

        // GoCardless
        if (tx.data.transactionAmount)
            amt = parseFloat(tx.data.transactionAmount.amount || 0);
        // EnableBanking
        else if (tx.data.transaction_amount)
            amt = parseFloat(tx.data.transaction_amount.amount || 0);
        // Trading212 / Direct
        else if (tx.data.amount !== undefined) amt = tx.data.amount;

        // Normalize if Trading212 withdraw/withdrawal
        const desc = getTxDescription(tx);
        if (desc.includes("WITHDRAW")) {
            amt = -Math.abs(amt);
        }
        return amt;
    }

    // Client-side recursive evaluator for the live preview
    function evaluateRuleClient(rule: any, tx: any): boolean {
        let matched = false;
        const operator = rule.operator || "NONE";
        const field = rule.field || "NONE";

        if (operator === "AND" || operator === "OR") {
            if (!rule.children || rule.children.length === 0) matched = false;
            else if (operator === "AND") {
                matched = rule.children.every((child: any) =>
                    evaluateRuleClient(child, tx),
                );
            } else {
                matched = rule.children.some((child: any) =>
                    evaluateRuleClient(child, tx),
                );
            }
        } else {
            // Leaf rule evaluation
            let target = "";
            const desc = getTxDescription(tx) || "";
            const peer = getTxPeer(tx) || "";
            const tags = tx.tags || "";
            const accountTags = mappedAccounts?.[`${tx.integrationId}:${tx.accountId}`]?.tags || mappedAccounts?.[tx.accountId]?.tags || "";
            const accountName = mappedAccounts?.[`${tx.integrationId}:${tx.accountId}`]?.name || mappedAccounts?.[tx.accountId]?.name || "";
            const amount = getTxAmount(tx) || 0;

            switch (field) {
                case "RECEIVER":
                    target = peer;
                    break;
                case "DESCRIPTION":
                    target = desc;
                    break;
                case "TAGS":
                    target = tags;
                    break;
                case "ACCOUNT_TAGS":
                    target = accountTags;
                    break;
                case "ACCOUNT_NAME":
                    target = accountName;
                    break;
                case "DATA_CHAIN":
                    target = tx.integrationId || "";
                    break;
                case "TRANSACTION_ID":
                    target = tx.id || "";
                    break;
                case "AMOUNT":
                    const limit = Number(rule.amountValue || 0);
                    switch (rule.amountOperator) {
                        case ">":
                            matched = amount > limit;
                            break;
                        case "<":
                            matched = amount < limit;
                            break;
                        case "=":
                            matched = amount === limit;
                            break;
                        case ">=":
                            matched = amount >= limit;
                            break;
                        case "<=":
                            matched = amount <= limit;
                            break;
                        default:
                            matched = false;
                    }
                    break;
                default:
                    matched = false;
            }

            if (field !== "AMOUNT" && field !== "NONE") {
                try {
                    const re = new RegExp(rule.regex || "", "i");
                    matched = re.test(target);
                } catch (e) {
                    matched = false;
                }
            }
        }

        if (rule.negate) {
            return !matched;
        }
        return matched;
    }

    let focusedBuilderNode = $state<any>(null);
    const previewRuleToEvaluate = $derived(
        focusedBuilderNode || activeBuilderRule,
    );

    const previewFocusLabel = $derived(
        (() => {
            const rule = previewRuleToEvaluate;
            if (rule === activeBuilderRule) return "Entire Rule";
            if ((rule.operator || "NONE") !== "NONE")
                return `Selected Group (${rule.operator})`;
            return `Selected Condition (${rule.field || "NONE"})`;
        })(),
    );

    const previewMatches = $derived(
        (() => {
            const rule = previewRuleToEvaluate;
            const operator = rule.operator || "NONE";
            const field = rule.field || "NONE";
            const regex = rule.regex || "";
            if (operator === "NONE" && !regex.trim() && field !== "AMOUNT")
                return [];
            try {
                return (transactions || [])
                    .filter((tx) => {
                        return evaluateRuleClient(rule, tx);
                    })
                    .slice(0, 10);
            } catch (e) {
                return [];
            }
        })(),
    );

    async function savePool() {
        if (!newPoolName.trim()) return;
        isSaving = true;
        try {
            const [pool, err] = await wsCall(
                "pools::save",
                TransactionPoolSchema,
                {
                    id: "",
                    name: newPoolName.trim(),
                    color: newPoolColor,
                    parentId: newPoolParentID || "",
                    isHidden: false,
                },
                [TransactionPoolSchema],
            ).one();
            if (err) throw err;
            if (pool) {
                pools = [...(pools || []), pool];
                newPoolName = "";
                newPoolParentID = null;
                if (onChange) onChange();
            }
        } catch (e) {
            console.error(e);
        } finally {
            isSaving = false;
        }
    }

    async function deletePool(id: string) {
        if (
            !confirm(
                "Delete this pool? Rules using it will remain but may fail to categorize.",
            )
        )
            return;
        const [, err] = await wsCall(
            "pools::delete",
            GenericIDSchema,
            { id: id },
            [ErrorSchema],
        ).one();
        if (err) {
            alert(err.message);
            return;
        }
        pools = (pools || []).filter((p) => p.id !== id);
        if (onChange) onChange();
    }

    // Builder Tree Manipulation helpers
    function convertToGroup(node: any) {
        const oldField = node.field;
        const oldRegex = node.regex;
        const oldAmtOp = node.amountOperator;
        const oldAmtVal = node.amountValue;
        const oldNegate = node.negate;

        node.operator = "AND";
        node.children = [
            {
                operator: "NONE",
                field: oldField,
                regex: oldRegex,
                amountOperator: oldAmtOp,
                amountValue: oldAmtVal,
                negate: oldNegate || false,
                children: [],
            },
            {
                operator: "NONE",
                field: "RECEIVER",
                regex: "",
                amountOperator: ">",
                amountValue: 0,
                negate: false,
                children: [],
            },
        ];
        focusedBuilderNode = node; // Focus the newly formed group
    }

    function convertToLeaf(node: any) {
        node.operator = "NONE";
        node.field = "RECEIVER";
        node.regex = "";
        node.amountOperator = ">";
        node.amountValue = 0;
        node.negate = false;
        node.children = [];
        focusedBuilderNode = node; // Focus the leaf
    }

    function addCondition(node: any) {
        if (!node.children) node.children = [];
        const newCondition = {
            operator: "NONE",
            field: "RECEIVER",
            regex: "",
            amountOperator: ">",
            amountValue: 0,
            negate: false,
            children: [],
        };
        node.children.push(newCondition);
        focusedBuilderNode = newCondition; // Focus newly added condition
    }

    function addGroup(node: any) {
        if (!node.children) node.children = [];
        const newGroup = {
            operator: "AND",
            field: "RECEIVER",
            regex: "",
            amountOperator: ">",
            amountValue: 0,
            negate: false,
            children: [
                {
                    operator: "NONE",
                    field: "RECEIVER",
                    regex: "",
                    amountOperator: ">",
                    amountValue: 0,
                    negate: false,
                    children: [],
                },
            ],
        };
        node.children.push(newGroup);
        focusedBuilderNode = newGroup; // Focus newly added group
    }

    function deleteChild(node: any, childIndex: number) {
        if (!node.children) return;
        const childToDelete = node.children[childIndex];
        node.children.splice(childIndex, 1);

        // If the focused node was deleted, reset focus to root rule
        if (focusedBuilderNode === childToDelete) {
            focusedBuilderNode = activeBuilderRule;
        }

        // If children are exhausted, convert back to a single leaf rule
        if (node.children.length === 0) {
            convertToLeaf(node);
        }
    }

    function editRule(rule: any) {
        // Deep clone the rule to avoid direct reactive edits to the list
        const cloned = decode(rule);
        activeBuilderRule = {
            operator: cloned.operator,
            field: cloned.field,
            regex: cloned.regex || "",
            amountOperator: cloned.amountOperator || ">",
            amountValue: cloned.amountValue || 0,
            negate: cloned.negate || false,
            children: cloned.children || [],
        };
        newRulePoolID = cloned.targetPoolId || "";
        rulePriority = cloned.priority || 0;
        editingRuleID = cloned.id;
        focusedBuilderNode = activeBuilderRule; // Reset focus to root

        // Smooth scroll to designer
        const el = document.getElementById("rule-designer-heading");
        if (el) el.scrollIntoView({ behavior: "smooth" });
    }

    function cancelEdit() {
        activeBuilderRule = {
            operator: "NONE",
            field: "RECEIVER",
            regex: "",
            amountOperator: ">",
            amountValue: 0,
            negate: false,
            children: [],
        };
        newRulePoolID = "";
        rulePriority = 0;
        editingRuleID = null;
        focusedBuilderNode = activeBuilderRule; // Reset focus to root
    }

    async function addRule() {
        if (!newRulePoolID) return;
        const payload = {
            id: editingRuleID || undefined,
            targetPoolId: newRulePoolID,
            priority: Number(rulePriority),
            operator: activeBuilderRule.operator,
            field: activeBuilderRule.field,
            regex: activeBuilderRule.regex.trim(),
            amountOperator: activeBuilderRule.amountOperator,
            amountValue:
                activeBuilderRule.field === "AMOUNT"
                    ? Number(activeBuilderRule.amountValue)
                    : 0,
            negate: !!activeBuilderRule.negate,
            children: activeBuilderRule.children,
        };

        const mapRuleToSchema = (r: any): any => {
            return {
                id: r.id || "",
                parentId: r.parentId || "",
                integrationId: r.integrationId || "",
                targetPoolId: r.targetPoolId || "",
                operator: r.operator || "NONE",
                field: r.field || "RECEIVER",
                regex: (r.regex || "").trim(),
                amountOperator: r.amountOperator || ">",
                amountValue: r.field === "AMOUNT" ? Number(r.amountValue || 0) : 0,
                priority: Number(r.priority !== undefined ? r.priority : (payload.priority || 0)),
                negate: !!r.negate,
                children: (r.children || []).map((c: any) => mapRuleToSchema(c)),
            };
        };

        try {
            const [rule, err] = await wsCall(
                "rules::save",
                TransactionRuleSchema,
                {
                    id: payload.id || "",
                    parentId: "",
                    integrationId: "",
                    targetPoolId: payload.targetPoolId || "",
                    operator: payload.operator,
                    field: payload.field,
                    regex: payload.regex,
                    amountOperator: payload.amountOperator,
                    amountValue: payload.amountValue,
                    priority: payload.priority,
                    negate: payload.negate,
                    children: (payload.children || []).map((c: any) => mapRuleToSchema(c)),
                },
                [TransactionRuleSchema],
            ).one();
            if (err) throw err;

            if (rule) {
                if (editingRuleID) {
                    rules = (rules || []).map((r) =>
                        r.id === editingRuleID ? rule : r,
                    );
                } else {
                    rules = [...(rules || []), rule];
                }
                cancelEdit();
                if (onChange) onChange();
            }
        } catch (e) {
            console.error(e);
        }
    }

    async function deleteRule(id: string) {
        if (!confirm("Are you sure you want to delete this rule?")) return;
        const [, err] = await wsCall(
            "rules::delete",
            GenericIDSchema,
            { id: id },
            [ErrorSchema],
        ).one();
        if (err) {
            alert(err.message);
            return;
        }
        rules = (rules || []).filter((r) => r.id !== id);
        // If the rule we are editing is deleted, cancel editing
        if (editingRuleID === id) {
            cancelEdit();
        }
        if (onChange) onChange();
    }
</script>

<!-- Inline Recursive Snippets -->
{#snippet renderBuilderNode(
    node: any,
    parentNode: any = null,
    childIndex: number | null = null,
)}
    {#if node.operator === "NONE"}
        <div
            class="flex flex-wrap items-center gap-3 p-4 bg-white/60 border rounded-2xl shadow-sm hover:shadow transition-all w-full cursor-pointer {focusedBuilderNode ===
            node
                ? 'ring-2 ring-purple-600/30 border-purple-500/50 bg-purple-50/10 shadow-purple-100/50 shadow-md'
                : 'border-slate-100'}"
            onclick={(e) => {
                e.stopPropagation();
                focusedBuilderNode = node;
            }}
            onfocusin={(e) => {
                e.stopPropagation();
                focusedBuilderNode = node;
            }}
        >
            <!-- Negate Checkbox -->
            <label class="flex items-center gap-1.5 cursor-pointer select-none">
                <input
                    type="checkbox"
                    bind:checked={node.negate}
                    class="rounded border-slate-300 text-red-600 focus:ring-red-500/20 w-3.5 h-3.5"
                />
                <span
                    class="text-[9px] font-black uppercase tracking-wider {node.negate
                        ? 'text-red-500 bg-red-50 border border-red-200/50 px-1.5 py-0.5 rounded'
                        : 'text-slate-400'}"
                >
                    NOT
                </span>
            </label>

            <select
                bind:value={node.field}
                class="px-3 py-2 bg-white border border-slate-200 rounded-xl font-bold text-xs outline-none focus:ring-4 focus:ring-purple-500/10 text-slate-800"
            >
                <option value="RECEIVER">Receiver</option>
                <option value="DESCRIPTION">Description</option>
                <option value="TAGS">Tags</option>
                <option value="ACCOUNT_TAGS">Account Tags</option>
                <option value="ACCOUNT_NAME">Account Name</option>
                <option value="DATA_CHAIN">Data Chain</option>
                <option value="TRANSACTION_ID">Transaction ID</option>
                <option value="AMOUNT">Amount (€)</option>
            </select>

            {#if node.field === "AMOUNT"}
                <select
                    bind:value={node.amountOperator}
                    class="px-3 py-2 bg-white border border-slate-200 rounded-xl font-bold text-xs outline-none focus:ring-4 focus:ring-purple-500/10 text-slate-800"
                >
                    <option value=">">&gt;</option>
                    <option value="<">&lt;</option>
                    <option value="=">=</option>
                    <option value=">=">&gt;=</option>
                    <option value="<=">&lt;=</option>
                </select>
                <input
                    type="number"
                    step="0.01"
                    bind:value={node.amountValue}
                    placeholder="Value..."
                    class="w-28 px-3 py-2 bg-white border border-slate-200 rounded-xl font-mono text-xs outline-none focus:ring-4 focus:ring-purple-500/10 text-slate-900"
                />
            {:else}
                <div class="flex-1 min-w-[150px] relative">
                    <span
                        class="absolute left-3 top-1/2 -translate-y-1/2 text-slate-400 font-mono text-xs"
                        >/</span
                    >
                    <input
                        type="text"
                        bind:value={node.regex}
                        placeholder="regex pattern (e.g. .*Amazon.*)"
                        class="w-full pl-6 pr-6 py-2 bg-white border border-slate-200 rounded-xl font-mono text-xs outline-none focus:ring-4 focus:ring-purple-500/10 text-slate-900"
                    />
                    <span
                        class="absolute right-3 top-1/2 -translate-y-1/2 text-slate-400 font-mono text-xs"
                        >/i</span
                    >
                </div>
            {/if}

            <div class="flex items-center gap-1.5 ml-auto">
                <button
                    type="button"
                    onclick={() => convertToGroup(node)}
                    class="px-2.5 py-1.5 bg-slate-100 hover:bg-slate-200 text-slate-600 font-bold rounded-lg text-[9px] uppercase tracking-wider transition-colors"
                >
                    Wrap in Group
                </button>
                {#if parentNode !== null && childIndex !== null}
                    <button
                        type="button"
                        onclick={() => deleteChild(parentNode, childIndex)}
                        class="p-2 hover:bg-red-50 text-slate-300 hover:text-red-600 rounded-xl transition-colors"
                        title="Remove condition"
                    >
                        <Trash2 class="w-4 h-4" />
                    </button>
                {/if}
            </div>
        </div>
    {:else if node.operator === "AND" || node.operator === "OR"}
        <div
            class="w-full border p-5 rounded-[24px] shadow-inner space-y-4 relative transition-all cursor-pointer {focusedBuilderNode ===
            node
                ? 'ring-2 ring-purple-600/30 border-purple-500/50 bg-purple-50/20 shadow-purple-100/20 shadow-md'
                : 'bg-slate-50/70 border-slate-200/60'}"
            onclick={(e) => {
                e.stopPropagation();
                focusedBuilderNode = node;
            }}
            onfocusin={(e) => {
                e.stopPropagation();
                focusedBuilderNode = node;
            }}
        >
            <div class="flex flex-wrap items-center justify-between gap-3">
                <div class="flex items-center gap-2">
                    <select
                        bind:value={node.operator}
                        class="px-3 py-1.5 bg-white border border-indigo-200 rounded-xl font-black text-xs outline-none focus:ring-4 focus:ring-indigo-500/10 text-indigo-700 uppercase"
                    >
                        <option value="AND">All of the following (AND)</option>
                        <option value="OR">Any of the following (OR)</option>
                    </select>
                    <!-- Negate Group Checkbox -->
                    <label
                        class="flex items-center gap-1.5 cursor-pointer select-none ml-2"
                    >
                        <input
                            type="checkbox"
                            bind:checked={node.negate}
                            class="rounded border-slate-300 text-red-600 focus:ring-red-500/20 w-3.5 h-3.5"
                        />
                        <span
                            class="text-[9px] font-black uppercase tracking-wider {node.negate
                                ? 'text-red-500 bg-red-50 border border-red-200/50 px-1.5 py-0.5 rounded'
                                : 'text-slate-400'}"
                        >
                            NOT
                        </span>
                    </label>
                </div>
                <div class="flex items-center gap-2">
                    <button
                        type="button"
                        onclick={() => addCondition(node)}
                        class="px-2.5 py-1.5 bg-white hover:bg-slate-100 text-slate-700 border border-slate-200 font-bold rounded-lg text-[9px] uppercase tracking-wider transition-all flex items-center gap-1 shadow-sm"
                    >
                        <Plus class="w-3 h-3 text-slate-500" /> Condition
                    </button>
                    <button
                        type="button"
                        onclick={() => addGroup(node)}
                        class="px-2.5 py-1.5 bg-white hover:bg-slate-100 text-slate-700 border border-slate-200 font-bold rounded-lg text-[9px] uppercase tracking-wider transition-all flex items-center gap-1 shadow-sm"
                    >
                        <Plus class="w-3 h-3 text-slate-500" /> Group
                    </button>
                    {#if parentNode !== null}
                        <button
                            type="button"
                            onclick={() => convertToLeaf(node)}
                            class="px-2.5 py-1.5 bg-amber-50 hover:bg-amber-100 text-amber-700 border border-amber-200 font-bold rounded-lg text-[9px] uppercase tracking-wider transition-all"
                        >
                            Flatten
                        </button>
                    {/if}
                    {#if parentNode !== null && childIndex !== null}
                        <button
                            type="button"
                            onclick={() => deleteChild(parentNode, childIndex)}
                            class="p-1.5 hover:bg-red-50 text-slate-300 hover:text-red-600 rounded-lg transition-colors"
                        >
                            <X class="w-3.5 h-3.5" />
                        </button>
                    {/if}
                </div>
            </div>

            <!-- Recursive render kids -->
            <div
                class="pl-4 border-l-2 border-dashed border-indigo-200/60 space-y-3"
            >
                {#each node.children || [] as child, idx (child)}
                    <div class="flex items-start" transition:slide>
                        {@render renderBuilderNode(child, node, idx)}
                    </div>
                {/each}
            </div>
        </div>
    {/if}
{/snippet}

{#snippet renderReadOnlyRuleNode(node: any)}
    <div class="pl-4 border-l border-slate-200/80 my-1.5 py-0.5 space-y-1">
        {#if node.operator === "AND" || node.operator === "OR"}
            <div class="flex items-center gap-1.5">
                {#if node.negate}
                    <span
                        class="px-1.5 py-0.5 bg-red-50 border border-red-200 text-[8px] font-black text-red-600 rounded uppercase tracking-[0.2em]"
                    >
                        NOT
                    </span>
                {/if}
                <span
                    class="px-1.5 py-0.5 bg-indigo-50 border border-indigo-100 text-[8px] font-black text-indigo-600 rounded"
                >
                    {node.operator}
                </span>
                <span
                    class="text-[8px] font-bold text-slate-400 uppercase tracking-[0.2em]"
                    >group:</span
                >
            </div>
            <div class="space-y-1">
                {#each node.children || [] as child}
                    {@render renderReadOnlyRuleNode(child)}
                {/each}
            </div>
        {:else}
            <div
                class="flex flex-wrap items-center gap-1.5 text-[10px] font-bold text-slate-600"
            >
                {#if node.negate}
                    <span
                        class="px-1.5 py-0.5 bg-red-50 border border-red-200 text-[8px] font-black text-red-600 rounded uppercase tracking-[0.2em]"
                    >
                        NOT
                    </span>
                {/if}
                <span
                    class="px-1 py-0.5 bg-slate-100 text-slate-500 rounded font-mono uppercase text-[7px] border border-slate-200/40"
                >
                    {node.field}
                </span>
                {#if node.field === "AMOUNT"}
                    <span class="text-slate-400 font-mono text-[9px]"
                        >{node.amountOperator}</span
                    >
                    <span class="text-slate-900 font-black font-mono"
                        >{(node.amountValue || 0).toLocaleString("de-DE")} €</span
                    >
                {:else}
                    <span class="text-slate-400">matches</span>
                    <span
                        class="text-purple-600 font-mono text-[9px] bg-purple-50 px-1 rounded truncate max-w-[200px] border border-purple-100/50"
                    >
                        /{node.regex}/
                    </span>
                {/if}
            </div>
        {/if}
    </div>
{/snippet}

{#snippet renderPoolNode(pool: any, depth = 0)}
    <div class="space-y-1">
        <div
            class="p-4 bg-slate-50 rounded-2xl border border-slate-100 flex items-center justify-between group/card"
            style="margin-left: {depth * 24}px"
        >
            <div class="flex items-center gap-3 flex-1 min-w-0">
                <div
                    class="w-3 h-3 rounded-full shrink-0"
                    style="background: {pool.color}"
                ></div>
                {#if editingPool && editingPool.id === pool.id}
                    <div class="flex items-center gap-2 flex-1">
                        <input
                            type="text"
                            bind:value={editingPool.name}
                            class="px-2 py-1 bg-white border border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 rounded-md text-xs font-bold text-slate-800 outline-none flex-1 shadow-sm"
                            onkeydown={(e) => {
                                if (e.key === "Enter") savePoolName();
                                if (e.key === "Escape") editingPool = null;
                            }}
                            autofocus
                        />
                        <div class="min-w-[256px] flex-shrink-0">
                            <SearchableDropdown
                                options={poolOptions.filter(
                                    (o) => o.id !== pool.id,
                                )}
                                bind:value={editingPool.parentId}
                                placeholder="Parent..."
                            />
                        </div>
                        <button
                            onclick={savePoolName}
                            class="p-1.5 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700"
                        >
                            <Check class="w-3.5 h-3.5" />
                        </button>
                    </div>
                {:else}
                    <div
                        class="flex items-center gap-2 group/title cursor-pointer truncate flex-1 min-w-0"
                        onclick={() => startEditingPool(pool)}
                    >
                        <span
                            class="font-black text-sm text-slate-900 hover:text-indigo-600 transition-colors truncate"
                            >{pool.name}</span
                        >
                        <span
                            class="opacity-0 group-hover/title:opacity-100 text-slate-400 hover:text-indigo-600 transition-all shrink-0"
                        >
                            <Pencil class="w-3 h-3" />
                        </span>
                    </div>
                {/if}
            </div>
            {#if !(editingPool && editingPool.id === pool.id)}
                <button
                    onclick={() => deletePool(pool.id)}
                    class="text-slate-300 hover:text-red-600 transition-colors opacity-0 group-hover/card:opacity-100 ml-2 shrink-0"
                >
                    <Trash2 class="w-3.5 h-3.5" />
                </button>
            {/if}
        </div>
        {#if pool.children && pool.children.length > 0}
            <div class="space-y-1">
                {#each pool.children as child}
                    {@render renderPoolNode(child, depth + 1)}
                {/each}
            </div>
        {/if}
    </div>
{/snippet}

<div class="space-y-8">
    <!-- Pool Architect -->
    <div class="glass-card p-8 space-y-8">
        <div class="flex items-center gap-4">
            <div class="p-3 bg-indigo-100 rounded-2xl">
                <LayoutGrid class="w-6 h-6 text-indigo-600" />
            </div>
            <h3 class="text-xl font-black tracking-tight">Transaction Pools</h3>
        </div>

        <div class="space-y-6">
            <div class="flex flex-wrap gap-2 items-end">
                <div class="flex-1 min-w-[200px] space-y-1">
                    <label
                        class="text-[10px] font-black uppercase text-slate-400 ml-1"
                        >Name</label
                    >
                    <input
                        type="text"
                        bind:value={newPoolName}
                        placeholder="New Pool Name..."
                        class="w-full px-4 py-3 bg-slate-50 border border-slate-200 rounded-xl font-bold text-xs outline-none focus:ring-4 focus:ring-indigo-500/10"
                    />
                </div>
                <div class="flex-1 min-w-[256px] flex-shrink-0">
                    <SearchableDropdown
                        label="Parent Pool"
                        options={poolOptions}
                        bind:value={newPoolParentID}
                    />
                </div>
                <div class="space-y-1">
                    <label
                        class="text-[10px] font-black uppercase text-slate-400 ml-1 text-center block"
                        >Color</label
                    >
                    <input
                        type="color"
                        bind:value={newPoolColor}
                        class="w-12 h-12 rounded-xl border-0 p-0 cursor-pointer overflow-hidden"
                    />
                </div>
                <button
                    onclick={savePool}
                    disabled={!newPoolName.trim() || isSaving}
                    class="p-3 bg-indigo-600 text-white rounded-xl hover:bg-indigo-700 transition-all disabled:opacity-50 h-12"
                >
                    <Plus class="w-5 h-5" />
                </button>
            </div>

            <div class="space-y-3">
                {#each poolTree || [] as pool}
                    {@render renderPoolNode(pool)}
                {/each}
            </div>
        </div>
    </div>

    <!-- Rule Architect -->
    <div class="glass-card p-8 space-y-8">
        <div class="flex items-center gap-4">
            <div class="p-3 bg-purple-100 rounded-2xl">
                <Zap class="w-6 h-6 text-purple-600" />
            </div>
            <h3 class="text-xl font-black tracking-tight">
                Categorization Rules
            </h3>
        </div>

        {#if pools && pools.length > 0}
            <div class="space-y-6">
                <!-- Advanced Rule Creator -->
                <div
                    class="space-y-6 p-6 bg-slate-50/50 rounded-[30px] border border-slate-100"
                >
                    <h4
                        id="rule-designer-heading"
                        class="text-xs font-black text-slate-400 uppercase ml-1 tracking-wider"
                    >
                        {editingRuleID ? "Edit Rule" : "Rule Designer"}
                    </h4>

                    <!-- Root node render -->
                    {@render renderBuilderNode(activeBuilderRule)}

                    <div
                        class="flex flex-wrap items-end gap-4 p-4 bg-white/70 border border-slate-100 rounded-2xl shadow-sm"
                    >
                        <div class="flex-1 min-w-[256px] flex-shrink-0">
                            <SearchableDropdown
                                label="Assign to Pool"
                                options={poolOptions}
                                bind:value={newRulePoolID}
                                placeholder="Select Pool..."
                            />
                        </div>

                        <div class="w-28 space-y-2">
                            <label
                                class="text-[8px] font-black text-slate-400 uppercase tracking-[0.2em] ml-1"
                                >Priority</label
                            >
                            <input
                                type="number"
                                bind:value={rulePriority}
                                class="w-full px-4 py-3 bg-white border border-slate-200 rounded-xl font-bold text-xs outline-none focus:ring-4 focus:ring-purple-500/10 text-slate-900"
                            />
                        </div>

                        {#if editingRuleID}
                            <button
                                type="button"
                                onclick={cancelEdit}
                                class="px-5 py-3.5 bg-slate-100 hover:bg-slate-200 text-slate-700 font-bold rounded-xl text-xs transition-all"
                            >
                                Cancel
                            </button>
                        {/if}
                        <button
                            onclick={addRule}
                            disabled={!newRulePoolID}
                            class="p-3 bg-purple-600 text-white rounded-xl hover:bg-purple-700 transition-all disabled:opacity-50 shadow-md shadow-purple-200 flex items-center justify-center gap-2 font-bold text-xs px-6 py-3.5"
                        >
                            <Check class="w-4 h-4" />
                            {editingRuleID ? "Update Rule" : "Save Rule"}
                        </button>
                    </div>
                </div>

                <!-- Preview matching transactions -->
                <div class="space-y-4">
                    <div class="flex items-center justify-between gap-2 ml-1">
                        <div class="flex items-center gap-2 text-slate-400">
                            <Search class="w-3.5 h-3.5 text-purple-500" />
                            <span
                                class="text-[9px] font-black uppercase tracking-[0.2em] text-slate-400"
                            >
                                Live Preview Matching - <span
                                    class="text-purple-600 font-bold"
                                    >{previewFocusLabel}</span
                                > (Max 10)
                            </span>
                        </div>
                        {#if focusedBuilderNode && focusedBuilderNode !== activeBuilderRule}
                            <button
                                type="button"
                                onclick={() =>
                                    (focusedBuilderNode = activeBuilderRule)}
                                class="px-2.5 py-1 bg-purple-50 hover:bg-purple-100 text-purple-600 border border-purple-200/50 rounded-lg text-[9px] font-black uppercase tracking-wider transition-all flex items-center gap-1 shadow-sm"
                            >
                                Reset Focus to Entire Rule
                            </button>
                        {/if}
                    </div>
                    {#if previewMatches.length > 0}
                        <div
                            class="grid grid-cols-1 md:grid-cols-2 gap-3"
                            transition:slide
                        >
                            {#each previewMatches as tx}
                                <div
                                    class="flex items-center justify-between p-4 bg-white border border-slate-100 rounded-2xl shadow-sm hover:shadow transition-all"
                                >
                                    <div
                                        class="flex items-center gap-3 min-w-0"
                                    >
                                        <div
                                            class="w-8 h-8 bg-slate-50 rounded-lg flex items-center justify-center flex-shrink-0"
                                        >
                                            <Clock
                                                class="w-4 h-4 text-slate-300"
                                            />
                                        </div>
                                        <div class="min-w-0">
                                            <p
                                                class="text-xs font-black text-slate-900 truncate uppercase"
                                            >
                                                {getTxDescription(tx)}
                                            </p>
                                            <div
                                                class="flex items-center gap-2"
                                            >
                                                <span
                                                    class="text-[8px] font-bold text-slate-400 uppercase"
                                                    >{new Date(tx.createdAt)
                                                        .toISOString()
                                                        .split("T")[0]}</span
                                                >
                                                <span class="text-slate-200"
                                                    >•</span
                                                >
                                                <span
                                                    class="text-[8px] font-bold text-slate-400 uppercase truncate max-w-[120px]"
                                                    >{getTxPeer(tx)}</span
                                                >
                                                {#if getTxPeerIban(tx)}
                                                    <span class="text-slate-200"
                                                        >•</span
                                                    >
                                                    <span
                                                        class="text-[8px] font-bold text-slate-300 uppercase truncate max-w-[120px]"
                                                        >{getTxPeerIban(
                                                            tx,
                                                        )}</span
                                                    >
                                                {/if}
                                            </div>
                                        </div>
                                    </div>
                                    <div class="text-right ml-4">
                                        <span
                                            class="text-xs font-black font-mono text-slate-900"
                                            >{getTxAmount(tx).toLocaleString(
                                                "de-DE",
                                            )} €</span
                                        >
                                    </div>
                                </div>
                            {/each}
                        </div>
                    {:else}
                        <div
                            class="p-8 text-center bg-slate-50/50 rounded-2xl border border-dashed border-slate-200"
                            transition:slide
                        >
                            <p
                                class="text-[10px] font-black uppercase text-slate-400 tracking-wider"
                            >
                                No matching transactions found for this scope
                            </p>
                        </div>
                    {/if}
                </div>

                <!-- Active rules list -->
                <div class="space-y-4">
                    <h4
                        class="text-xs font-black text-slate-400 uppercase ml-1 tracking-wider"
                    >
                        Active Categorization Rules
                    </h4>
                    {#if rules && rules.length > 0}
                        <div class="grid grid-cols-1 gap-4">
                            {#each rules || [] as rule}
                                {@const pool = (pools || []).find(
                                    (p) => p.id === rule.targetPoolId,
                                )}
                                <div
                                    class="p-6 bg-white border border-slate-100 rounded-3xl shadow-sm flex items-start justify-between group hover:border-purple-200 hover:shadow transition-all"
                                >
                                    <div class="space-y-3 flex-1 min-w-0">
                                        <div
                                            class="flex flex-wrap items-center gap-2"
                                        >
                                            <div
                                                class="flex items-center gap-1.5"
                                            >
                                                <div
                                                    class="w-2.5 h-2.5 rounded-full"
                                                    style="background: {pool?.color ||
                                                        '#94a3b8'}"
                                                ></div>
                                                <span
                                                    class="text-xs font-black text-slate-900 uppercase tracking-tight"
                                                    >{pool?.name ||
                                                        "Unknown Pool"}</span
                                                >
                                            </div>
                                            <ArrowRight
                                                class="w-3.5 h-3.5 text-slate-300"
                                            />
                                            <span
                                                class="px-2 py-0.5 bg-slate-50 border border-slate-200/60 rounded-full text-[8px] font-black text-slate-400 uppercase"
                                            >
                                                Priority: {rule.priority}
                                            </span>
                                        </div>

                                        <!-- Root rule node tree render -->
                                        <div
                                            class="pl-2 border-l-2 border-purple-500/20 py-0.5"
                                        >
                                            {@render renderReadOnlyRuleNode(
                                                rule,
                                            )}
                                        </div>
                                    </div>

                                    <div class="flex items-center gap-2">
                                        <button
                                            onclick={() => editRule(rule)}
                                            class="p-2 bg-slate-50 hover:bg-indigo-50 text-slate-400 hover:text-indigo-600 rounded-xl transition-all"
                                            title="Edit rule"
                                        >
                                            <Pencil class="w-4 h-4" />
                                        </button>
                                        <button
                                            onclick={() => deleteRule(rule.id)}
                                            class="p-2 bg-slate-50 hover:bg-red-50 text-slate-300 hover:text-red-600 rounded-xl transition-all"
                                            title="Delete rule"
                                        >
                                            <X class="w-4 h-4" />
                                        </button>
                                    </div>
                                </div>
                            {/each}
                        </div>
                    {:else}
                        <div
                            class="p-8 text-center bg-slate-50/50 rounded-3xl border border-dashed border-slate-200"
                        >
                            <p
                                class="text-[10px] font-black uppercase text-slate-400 tracking-wider"
                            >
                                No rules defined yet
                            </p>
                        </div>
                    {/if}
                </div>
            </div>
        {:else}
            <div
                class="p-8 text-center bg-slate-50 rounded-3xl border border-dashed border-slate-200"
            >
                <p class="text-[10px] font-black uppercase text-slate-400">
                    Create a Pool first to define rules
                </p>
            </div>
        {/if}
    </div>
</div>
