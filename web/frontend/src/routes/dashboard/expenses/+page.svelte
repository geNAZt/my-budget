<script lang="ts">
	import ExpenseManager from '../ExpenseManager.svelte';
</script>

<div class="space-y-6">
	<a href="/dashboard" class="text-xs font-black uppercase tracking-[0.2em] text-slate-400 hover:text-indigo-600 transition-colors flex items-center gap-2 mb-2">
		← Back to Command Center
	</a>
	<ExpenseManager />
</div>
